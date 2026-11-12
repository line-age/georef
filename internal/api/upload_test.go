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

	sheets := sheet.NewDB(pool)
	storageRoot := t.TempDir()
	store := storage.NewLocalStore(storageRoot)
	const oneGB = 1 << 30
	h := api.New(sheets, store, "https://maps.example.com", oneGB)

	// ── Happy path ────────────────────────────────────────────────────────

	t.Run("tiff_upload_returns_202", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
			title:       "Test Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("jpeg_upload_returns_202", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
			title:       "JPEG Map",
			fileContent: makeJPEGFile("content"),
			mimeType:    "image/jpeg",
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("response_content_type_is_json", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
			title:       "JSON CT Map",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		ct := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("want application/json, got %q", ct)
		}
	})

	t.Run("response_contains_all_required_fields", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
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
		rec := doUpload(t, h, uploadForm{
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
		r1 := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Map A", fileContent: makeTIFFFile("a"), mimeType: "image/tiff",
		}))
		r2 := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Map B", fileContent: makeTIFFFile("b"), mimeType: "image/tiff",
		}))
		if r1.ID == r2.ID {
			t.Errorf("two uploads got the same id: %s", r1.ID)
		}
	})

	t.Run("urls_are_built_from_base_url_and_id", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
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
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Status Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if s.Status != model.StatusProcessing {
			t.Errorf("want status %q, got %q", model.StatusProcessing, s.Status)
		}
	})

	t.Run("checksum_matches_actual_file_content", func(t *testing.T) {
		content := makeTIFFFile("reproducible scan content")
		h256 := sha256.Sum256(content)
		expectedChecksum := hex.EncodeToString(h256[:])

		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Checksum Map", fileContent: content, mimeType: "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if s.OriginalChecksum != expectedChecksum {
			t.Errorf("checksum: got %q, want %q", s.OriginalChecksum, expectedChecksum)
		}
	})

	t.Run("file_size_matches_actual_bytes", func(t *testing.T) {
		content := makeTIFFFile("size test")
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Size Map", fileContent: content, mimeType: "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if s.OriginalSize != int64(len(content)) {
			t.Errorf("original_size: got %d, want %d", s.OriginalSize, len(content))
		}
	})

	t.Run("original_scan_stored_at_canonical_path", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Path Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		path, err := store.OriginalScanPath(ctx, resp.ID)
		if err != nil {
			t.Fatalf("OriginalScanPath: %v", err)
		}
		wantSuffix := "sheets/" + resp.ID + "/original"
		if !strings.HasSuffix(path, wantSuffix) {
			t.Errorf("path %q does not end with %q", path, wantSuffix)
		}
	})

	t.Run("original_scan_is_stored_in_storage", func(t *testing.T) {
		content := makeTIFFFile("stored scan content")
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Storage Map", fileContent: content, mimeType: "image/tiff",
		}))

		rc, err := store.OpenOriginalScan(ctx, resp.ID)
		if err != nil {
			t.Fatalf("OpenOriginalScan: %v", err)
		}
		defer rc.Close()

		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read stored file: %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("stored content mismatch: got %d bytes, want %d bytes", len(got), len(content))
		}
	})

	t.Run("optional_fields_stored_when_provided", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title:       "Optional Map",
			institution: "National Library",
			sourceURL:   "https://source.example.com/map",
			rightsNote:  "Public domain",
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if s.Institution != "National Library" {
			t.Errorf("institution: got %q, want %q", s.Institution, "National Library")
		}
		if s.SourceURL != "https://source.example.com/map" {
			t.Errorf("source_url: got %q, want %q", s.SourceURL, "https://source.example.com/map")
		}
		if s.RightsNote != "Public domain" {
			t.Errorf("rights_note: got %q, want %q", s.RightsNote, "Public domain")
		}
	})

	t.Run("optional_fields_empty_when_not_provided", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Minimal Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if s.Institution != "" {
			t.Errorf("institution should be empty, got %q", s.Institution)
		}
		if s.SourceURL != "" {
			t.Errorf("source_url should be empty, got %q", s.SourceURL)
		}
		if s.RightsNote != "" {
			t.Errorf("rights_note should be empty, got %q", s.RightsNote)
		}
	})

	// ── Validation failures ───────────────────────────────────────────────

	t.Run("missing_title_returns_400_with_message", func(t *testing.T) {
		rec := doUpload(t, h, uploadForm{
			fileContent: makeTIFFFile("content"),
			mimeType:    "image/tiff",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "title is required")
	})

	t.Run("missing_file_returns_400_with_message", func(t *testing.T) {
		// No fileContent → buildMultipartForm omits the file part entirely.
		rec := doUpload(t, h, uploadForm{title: "No File Map"})
		assertErrorResponse(t, rec, http.StatusBadRequest, "file is required")
	})

	t.Run("unsupported_file_type_returns_400_with_message", func(t *testing.T) {
		// Real PNG magic bytes so http.DetectContentType identifies it as image/png.
		pngContent := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, []byte("png data")...)
		rec := doUpload(t, h, uploadForm{
			title:       "PNG Map",
			fileContent: pngContent,
			mimeType:    "image/png",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "unsupported file type; accepted: image/tiff, image/jpeg")
	})

	t.Run("unknown_mime_type_returns_400", func(t *testing.T) {
		// Plain text bytes are detected as text/plain, which is not accepted.
		rec := doUpload(t, h, uploadForm{
			title:       "Unknown Type Map",
			fileContent: []byte("plain text, not an image"),
			mimeType:    "application/octet-stream",
		})
		assertErrorResponse(t, rec, http.StatusBadRequest, "unsupported file type; accepted: image/tiff, image/jpeg")
	})

	t.Run("file_too_large_returns_413", func(t *testing.T) {
		// Create a handler with a tiny limit so we don't need to send gigabytes.
		smallH := api.New(sheets, store, "https://maps.example.com", 100)
		// TIFF magic (4 bytes) + 200 bytes of padding = 204 bytes, well over the 100-byte limit.
		bigContent := makeTIFFFile(strings.Repeat("x", 200))
		rec := doUpload(t, smallH, uploadForm{
			title:       "Big Map",
			fileContent: bigContent,
			mimeType:    "image/tiff",
		})
		assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "file too large")
	})

	t.Run("sheet_record_stores_original_path", func(t *testing.T) {
		resp := decodeUploadResponse(t, doUpload(t, h, uploadForm{
			title: "Path DB Map", fileContent: makeTIFFFile("content"), mimeType: "image/tiff",
		}))

		s, err := sheets.GetByID(ctx, resp.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		wantSuffix := "sheets/" + resp.ID + "/original"
		if !strings.HasSuffix(s.OriginalPath, wantSuffix) {
			t.Errorf("original_path: got %q, does not end with %q", s.OriginalPath, wantSuffix)
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
func doUpload(t *testing.T, h *api.Handler, f uploadForm) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := buildMultipartForm(t, f)
	req := httptest.NewRequest(http.MethodPost, "/sheets", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

// buildMultipartForm constructs a multipart body from the given fields.
// The file part is omitted when fileContent is nil and mimeType is empty.
func buildMultipartForm(t *testing.T, f uploadForm) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if f.title != "" {
		mw.WriteField("title", f.title) //nolint:errcheck
	}
	if f.institution != "" {
		mw.WriteField("institution", f.institution) //nolint:errcheck
	}
	if f.sourceURL != "" {
		mw.WriteField("source_url", f.sourceURL) //nolint:errcheck
	}
	if f.rightsNote != "" {
		mw.WriteField("rights_note", f.rightsNote) //nolint:errcheck
	}

	if len(f.fileContent) > 0 || f.mimeType != "" {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="file"; filename="scan.tif"`)
		h.Set("Content-Type", f.mimeType)
		fw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := fw.Write(f.fileContent); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}

	mw.Close()
	return &body, mw.FormDataContentType()
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
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type: got %q, want application/json", ct)
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
	sql, err := os.ReadFile("../../db/migrations/001_create_sheets.sql")
	if err != nil {
		return fmt.Errorf("read migration file: %w", err)
	}
	_, err = pool.Exec(ctx, string(sql))
	return err
}
