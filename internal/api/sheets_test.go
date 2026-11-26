package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/line-age/georef/internal/api"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

// sheetAPIResponse mirrors the JSON body returned by the sheet API endpoints.
type sheetAPIResponse struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Institution  string     `json:"institution"`
	SourceURL    string     `json:"source_url"`
	RightsNote   string     `json:"rights_note"`
	Status       string     `json:"status"`
	Public       bool       `json:"public"`
	ErrorMessage string     `json:"error_message"`
	SheetURL     string     `json:"sheet_url"`
	IIIFBaseURL  string     `json:"iiif_base_url"`
	ManifestURL  string     `json:"manifest_url"`
	DeletedAt    *time.Time `json:"deleted_at"`
}

// sheetListResponse mirrors the JSON body returned by GET /sheets.
type sheetListResponse struct {
	Sheets []sheetAPIResponse `json:"sheets"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func TestSheetCRUD(t *testing.T) {
	pool := startTestDB(t)

	sheets := sheet.NewRepository(pool)
	store := storage.NewLocalStore(t.TempDir())
	const oneGB = 1 << 30
	handler := api.New(sheets, store, nil, nil, api.Config{
		BaseURL:              "https://maps.example.com",
		IIIFImageAPIVersion:  2,
		IIIFImageAPILevel:    2,
		RetryAfterSeconds:    5,
		MultipartMemoryLimit: 32 << 20,
		MaxUploadSize:        oneGB,
	})

	// Upload a sheet used by most sub-tests below.
	uploadRec := doUpload(t, handler, uploadForm{
		title:       "CRUD Test Map",
		institution: "Test Library",
		fileContent: makeTIFFFile("content"),
		mimeType:    "image/tiff",
	})
	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("setup upload: got %d, want 202", uploadRec.Code)
	}
	var uploadedSheet uploadResponse
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploadedSheet); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	sheetID := uploadedSheet.ID

	// ── GET /sheets/{id} ─────────────────────────────────────────────────

	t.Run("get_returns_200_with_sheet_data", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets/"+sheetID)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp sheetAPIResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ID != sheetID {
			t.Errorf("id: got %q, want %q", resp.ID, sheetID)
		}
		if resp.Title != "CRUD Test Map" {
			t.Errorf("title: got %q, want %q", resp.Title, "CRUD Test Map")
		}
		if resp.Status != string(model.StatusProcessing) {
			t.Errorf("status: got %q, want %q", resp.Status, model.StatusProcessing)
		}
	})

	t.Run("get_unknown_id_returns_404", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets/01ZZZZZZZZZZZZZZZZZZZZZZZZ")
		if rec.Code != http.StatusNotFound {
			t.Errorf("want 404, got %d", rec.Code)
		}
	})

	// ── GET /sheets ───────────────────────────────────────────────────────

	t.Run("list_returns_200_with_sheets", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp sheetListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Sheets) == 0 {
			t.Error("expected at least one sheet in listing")
		}
	})

	t.Run("list_status_filter_returns_matching_sheets", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets?status=processing")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		var resp sheetListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, sheetRecord := range resp.Sheets {
			if sheetRecord.Status != "processing" {
				t.Errorf("got sheet with status %q in processing filter", sheetRecord.Status)
			}
		}
	})

	t.Run("list_invalid_status_returns_400", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets?status=invalid")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("want 400, got %d", rec.Code)
		}
	})

	t.Run("list_invalid_limit_returns_400", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets?limit=abc")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("want 400, got %d", rec.Code)
		}
	})

	// ── PATCH /sheets/{id} ────────────────────────────────────────────────

	t.Run("patch_updates_all_fields_and_returns_200", func(t *testing.T) {
		patchRec := doPatch(t, handler, "/sheets/"+sheetID, map[string]any{
			"title":        "Updated Title",
			"institution":  "Updated Institution",
			"source_url":   "https://updated.example.com",
			"rights_note":  "Updated rights",
		})
		if patchRec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", patchRec.Code, patchRec.Body.String())
		}
		var resp sheetAPIResponse
		if err := json.NewDecoder(patchRec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Title != "Updated Title" {
			t.Errorf("title: got %q, want %q", resp.Title, "Updated Title")
		}
		if resp.Institution != "Updated Institution" {
			t.Errorf("institution: got %q", resp.Institution)
		}
	})

	t.Run("patch_single_field_returns_200", func(t *testing.T) {
		patchRec := doPatch(t, handler, "/sheets/"+sheetID, map[string]any{
			"institution": "Only Institution Changed",
		})
		if patchRec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", patchRec.Code, patchRec.Body.String())
		}
		var resp sheetAPIResponse
		if err := json.NewDecoder(patchRec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Institution != "Only Institution Changed" {
			t.Errorf("institution: got %q", resp.Institution)
		}
	})

	t.Run("patch_visibility_only_returns_200", func(t *testing.T) {
		patchRec := doPatch(t, handler, "/sheets/"+sheetID, map[string]any{
			"public": false,
		})
		if patchRec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", patchRec.Code, patchRec.Body.String())
		}
		var resp sheetAPIResponse
		if err := json.NewDecoder(patchRec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Public {
			t.Errorf("public: want false, got true")
		}
	})

	t.Run("patch_empty_title_returns_400", func(t *testing.T) {
		rec := doPatch(t, handler, "/sheets/"+sheetID, map[string]any{
			"title": "",
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("want 400, got %d", rec.Code)
		}
	})

	t.Run("patch_empty_body_returns_400", func(t *testing.T) {
		rec := doPatch(t, handler, "/sheets/"+sheetID, map[string]any{})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("want 400, got %d", rec.Code)
		}
	})

	t.Run("patch_unknown_id_returns_404", func(t *testing.T) {
		rec := doPatch(t, handler, "/sheets/01ZZZZZZZZZZZZZZZZZZZZZZZZ", map[string]any{
			"title": "New Title",
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("want 404, got %d", rec.Code)
		}
	})

	// ── DELETE /sheets/{id} and IIIF 410 ─────────────────────────────────

	t.Run("delete_returns_204", func(t *testing.T) {
		rec := doDelete(t, handler, "/sheets/"+sheetID)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("want 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("get_deleted_sheet_returns_410", func(t *testing.T) {
		rec := doGet(t, handler, "/sheets/"+sheetID)
		if rec.Code != http.StatusGone {
			t.Errorf("want 410, got %d", rec.Code)
		}
	})

	t.Run("iiif_info_json_for_deleted_sheet_returns_410", func(t *testing.T) {
		rec := doGet(t, handler, "/iiif/"+sheetID+"/info.json")
		if rec.Code != http.StatusGone {
			t.Errorf("want 410, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("iiif_manifest_for_deleted_sheet_returns_410", func(t *testing.T) {
		rec := doGet(t, handler, "/iiif/"+sheetID+"/manifest")
		if rec.Code != http.StatusGone {
			t.Errorf("want 410, got %d", rec.Code)
		}
	})

	t.Run("delete_unknown_id_returns_404", func(t *testing.T) {
		rec := doDelete(t, handler, "/sheets/01ZZZZZZZZZZZZZZZZZZZZZZZZ")
		if rec.Code != http.StatusNotFound {
			t.Errorf("want 404, got %d", rec.Code)
		}
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────

func doGet(t *testing.T, handler *api.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	return rec
}

func doPatch(t *testing.T, handler *api.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	return rec
}

func doDelete(t *testing.T, handler *api.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	return rec
}
