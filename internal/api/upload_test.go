package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/line-age/georef/internal/api"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

// uploadResponse mirrors the JSON body returned by POST /sheets.
type uploadResponse struct {
	ID          string `json:"id"`
	SheetURL    string `json:"sheet_url"`
	IIIFBaseURL string `json:"iiif_base_url"`
	ManifestURL string `json:"manifest_url"`
}

// errorResponse mirrors the JSON body returned on errors.
type errorResponse struct {
	Error string `json:"error"`
}

// makeTIFFFile returns bytes prefixed with the little-endian TIFF magic number,
// which is what http.DetectContentType requires to identify a file as image/tiff.
func makeTIFFFile(rest string) []byte {
	return append([]byte{0x49, 0x49, 0x2A, 0x00}, []byte(rest)...)
}

// makeJPEGFile returns bytes prefixed with the JPEG magic number.
func makeJPEGFile(rest string) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF}, []byte(rest)...)
}

func TestUploadSheet(t *testing.T) {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "georef_test",
				"POSTGRES_USER":     "georef",
				"POSTGRES_PASSWORD": "georef",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Cleanup(func() { ctr.Terminate(ctx) }) //nolint:errcheck

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	dsn := "postgres://georef:georef@" + host + ":" + port.Port() + "/georef_test"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := runMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sheets := sheet.NewRepository(pool)
	store := storage.NewLocalStore(t.TempDir())
	const oneGB = 1 << 30
	handler := api.New(sheets, store, "https://maps.example.com", oneGB)

	// ── Happy path ────────────────────────────────────────────────────────

	t.Run("tiff_upload_returns_202", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title:       "Test Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("jpeg_upload_returns_202", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title:       "JPEG Map",
			fileContent: makeJPEGFile("content"),
			mimeType:    "image/jpeg",
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("response_content_type_is_json", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title:       "JSON CT Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		contentType := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/json") {
			t.Errorf("want application/json, got %q", contentType)
		}
	})

	t.Run("response_contains_all_required_fields", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title:       "Fields Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		resp := decodeUploadResponse(t, rec)

		if resp.ID == "" {
			t.Error("id is empty")
		}
		if resp.SheetURL == "" {
			t.Error("sheet_url is empty")
		}
		if resp.IIIFBaseURL == "" {
			t.Error("iiif_base_url is empty")
		}
		if resp.ManifestURL == "" {
			t.Error("manifest_url is empty")
		}
	})

	t.Run("response_id_is_valid_ulid", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title:       "ULID Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		resp := decodeUploadResponse(t, rec)

		if _, err := ulid.Parse(resp.ID); err != nil {
			t.Errorf("id %q is not a valid ULID: %v", resp.ID, err)
		}
	})

	t.Run("each_upload_gets_unique_id", func(t *testing.T) {
		firstResp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Map A", fileContent: makeTIFFFile("a"), mimeType: "image/tiff",
		}))
		secondResp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Map B", fileContent: makeTIFFFile("b"), mimeType: "image/tiff",
		}))
		if firstResp.ID == secondResp.ID {
			t.Errorf("two uploads got the same id: %s", firstResp.ID)
		}
	})

	t.Run("urls_are_built_from_base_url_and_id", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			title: "URL Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		})
		resp := decodeUploadResponse(t, rec)

		wantSheetURL := "https://maps.example.com/sheets/" + resp.ID
		wantIIIFBase := "https://maps.example.com/iiif/" + resp.ID
		wantManifest := "https://maps.example.com/iiif/" + resp.ID + "/manifest"

		if resp.SheetURL != wantSheetURL {
			t.Errorf("sheet_url: got %q, want %q", resp.SheetURL, wantSheetURL)
		}
		if resp.IIIFBaseURL != wantIIIFBase {
			t.Errorf("iiif_base_url: got %q, want %q", resp.IIIFBaseURL, wantIIIFBase)
		}
		if resp.ManifestURL != wantManifest {
			t.Errorf("manifest_url: got %q, want %q", resp.ManifestURL, wantManifest)
		}
	})

	t.Run("sheet_is_created_with_processing_status", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Status Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if uploadedSheet.Status != model.StatusProcessing {
			t.Errorf("want status %q, got %q", model.StatusProcessing, uploadedSheet.Status)
		}
	})

	t.Run("checksum_matches_actual_file_content", func(t *testing.T) {
		content := makeTIFFFile("reproducible scan content")
		hashBytes := sha256.Sum256(content)
		expectedChecksum := hex.EncodeToString(hashBytes[:])

		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Checksum Map", fileContent: content, mimeType: "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if uploadedSheet.OriginalChecksum != expectedChecksum {
			t.Errorf("checksum: got %q, want %q", uploadedSheet.OriginalChecksum, expectedChecksum)
		}
	})

	t.Run("file_size_matches_actual_bytes", func(t *testing.T) {
		content := makeTIFFFile("size test")
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Size Map", fileContent: content, mimeType: "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if uploadedSheet.OriginalSize != int64(len(content)) {
			t.Errorf("original_size: got %d, want %d", uploadedSheet.OriginalSize, len(content))
		}
	})

	t.Run("original_scan_stored_at_canonical_path", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Path Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		storedPath, err := store.OriginalScanPath(ctx, resp.ID)
		if err != nil {
			t.Fatalf("OriginalScanPath: %v", err)
		}
		wantSuffix := "sheets/" + resp.ID + "/original"
		if !strings.HasSuffix(storedPath, wantSuffix) {
			t.Errorf("path %q does not end with %q", storedPath, wantSuffix)
		}
	})

	t.Run("original_scan_is_stored_in_storage", func(t *testing.T) {
		content := makeTIFFFile("stored scan content")
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Storage Map", fileContent: content, mimeType: "image/tiff",
		}))

		storedFile, err := store.OpenOriginalScan(ctx, resp.ID)
		if err != nil {
			t.Fatalf("OpenOriginalScan: %v", err)
		}
		defer storedFile.Close()

		storedBytes, err := io.ReadAll(storedFile)
		if err != nil {
			t.Fatalf("read stored file: %v", err)
		}
		if !bytes.Equal(storedBytes, content) {
			t.Errorf("stored content mismatch: got %d bytes, want %d bytes", len(storedBytes), len(content))
		}
	})

	t.Run("optional_fields_stored_when_provided", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title:       "Optional Map",
			institution: "National Library",
			sourceURL:   "https://source.example.com/map",
			rightsNote:  "Public domain",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if uploadedSheet.Institution != "National Library" {
			t.Errorf("institution: got %q, want %q", uploadedSheet.Institution, "National Library")
		}
		if uploadedSheet.SourceURL != "https://source.example.com/map" {
			t.Errorf("source_url: got %q, want %q", uploadedSheet.SourceURL, "https://source.example.com/map")
		}
		if uploadedSheet.RightsNote != "Public domain" {
			t.Errorf("rights_note: got %q, want %q", uploadedSheet.RightsNote, "Public domain")
		}
	})

	t.Run("optional_fields_empty_when_not_provided", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Minimal Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if uploadedSheet.Institution != "" {
			t.Errorf("institution should be empty, got %q", uploadedSheet.Institution)
		}
		if uploadedSheet.SourceURL != "" {
			t.Errorf("source_url should be empty, got %q", uploadedSheet.SourceURL)
		}
		if uploadedSheet.RightsNote != "" {
			t.Errorf("rights_note should be empty, got %q", uploadedSheet.RightsNote)
		}
	})

	// ── Validation failures ───────────────────────────────────────────────

	t.Run("missing_title_returns_400_with_message", func(t *testing.T) {
		rec := doUpload(t, handler, uploadForm{
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "title is required")
	})

	t.Run("missing_file_returns_400_with_message", func(t *testing.T) {
		// No fileContent → buildMultipartForm omits the file part entirely.
		rec := doUpload(t, handler, uploadForm{title: "No File Map"})
		assertErrorResponse(t, rec, http.StatusBadRequest, "file is required")
	})

	t.Run("unsupported_file_type_returns_400_with_message", func(t *testing.T) {
		// Real PNG magic bytes so http.DetectContentType identifies it as image/png.
		pngContent := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, []byte("png data")...)
		rec := doUpload(t, handler, uploadForm{
			title:       "PNG Map",
			fileContent: pngContent,
			mimeType:    "image/png",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "unsupported file type; accepted: image/tiff, image/jpeg")
	})

	t.Run("unknown_mime_type_returns_400", func(t *testing.T) {
		// Plain text bytes are detected as text/plain, which is not accepted.
		rec := doUpload(t, handler, uploadForm{
			title:       "Unknown Type Map",
			fileContent: []byte("plain text, not an image"),
			mimeType:    "application/octet-stream",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "unsupported file type; accepted: image/tiff, image/jpeg")
	})

	t.Run("file_too_large_returns_413", func(t *testing.T) {
		// Create a handler with a tiny limit so we don't need to send gigabytes.
		limitedHandler := api.New(sheets, store, "https://maps.example.com", 100)
		// TIFF magic (4 bytes) + 200 bytes of padding = 204 bytes, well over the 100-byte limit.
		oversizedContent := makeTIFFFile(strings.Repeat("x", 200))
		rec := doUpload(t, limitedHandler, uploadForm{
			title:       "Big Map",
			fileContent: oversizedContent,
			mimeType:    "image/tiff",
		})
		assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "file too large")
	})

	t.Run("sheet_record_stores_original_path", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, handler, uploadForm{
			title: "Path DB Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		uploadedSheet, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		wantSuffix := "sheets/" + resp.ID + "/original"
		if !strings.HasSuffix(uploadedSheet.OriginalPath, wantSuffix) {
			t.Errorf("original_path: got %q, does not end with %q", uploadedSheet.OriginalPath, wantSuffix)
		}
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────

type uploadForm struct {
	title       string
	institution string
	sourceURL   string
	rightsNote  string
	fileContent []byte
	mimeType    string
}

// doUpload builds and sends a POST /sheets request, returning the recorder.
func doUpload(t *testing.T, handler *api.Handler, form uploadForm) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := buildMultipartForm(t, form)
	req := httptest.NewRequest(http.MethodPost, "/sheets", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	return rec
}

// buildMultipartForm constructs a multipart body from the given fields.
// The file part is omitted when fileContent is nil and mimeType is empty.
func buildMultipartForm(t *testing.T, form uploadForm) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)

	if form.title != "" {
		multipartWriter.WriteField("title", form.title) //nolint:errcheck
	}
	if form.institution != "" {
		multipartWriter.WriteField("institution", form.institution) //nolint:errcheck
	}
	if form.sourceURL != "" {
		multipartWriter.WriteField("source_url", form.sourceURL) //nolint:errcheck
	}
	if form.rightsNote != "" {
		multipartWriter.WriteField("rights_note", form.rightsNote) //nolint:errcheck
	}

	if len(form.fileContent) > 0 || form.mimeType != "" {
		filePartHeaders := make(textproto.MIMEHeader)
		filePartHeaders.Set("Content-Disposition", `form-data; name="file"; filename="scan.tif"`)
		filePartHeaders.Set("Content-Type", form.mimeType)
		filePartWriter, err := multipartWriter.CreatePart(filePartHeaders)
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := filePartWriter.Write(form.fileContent); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}

	multipartWriter.Close()
	return &body, multipartWriter.FormDataContentType()
}

// decodeUploadResponse decodes a 202 response body into an uploadResponse.
func decodeUploadResponse(t *testing.T, rec *httptest.ResponseRecorder) uploadResponse {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp uploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// assertErrorResponse checks the status code and error message of an error response.
func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMsg string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Errorf("status: got %d, want %d", rec.Code, wantStatus)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("content-type: got %q, want application/json", contentType)
	}
	var errResp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != wantMsg {
		t.Errorf("error message: got %q, want %q", errResp.Error, wantMsg)
	}
}

// runMigrations runs the actual migration files against the test database,
// so the test schema stays in sync with production.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrationSQL, err := os.ReadFile("../../db/migrations/001_create_sheets.up.sql")
	if err != nil {
		return fmt.Errorf("read migration file: %w", err)
	}
	_, err = pool.Exec(ctx, string(migrationSQL))
	return err
}
