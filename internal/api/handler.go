package api

import (
	"net/url"
	"time"

	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/proxy"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

// Handler holds the shared dependencies for all HTTP handlers.
type Handler struct {
	sheets         sheet.Accessor
	store          storage.Store
	imageProxy     *proxy.Proxy
	baseURL        string
	imageServerURL string
	maxUploadSize  int64
}

// New returns a Handler wired to the given dependencies.
// baseURL is the public root URL (e.g. "https://maps.example.com"), no trailing slash.
// imageServerURL is the internal IIIF image server URL, not reachable from outside.
// maxUploadSize is the maximum accepted request body in bytes.
func New(sheets sheet.Accessor, store storage.Store, imageProxy *proxy.Proxy, baseURL, imageServerURL string, maxUploadSize int64) *Handler {
	return &Handler{
		sheets:         sheets,
		store:          store,
		imageProxy:     imageProxy,
		baseURL:        baseURL,
		imageServerURL: imageServerURL,
		maxUploadSize:  maxUploadSize,
	}
}

// sheetResponse is the JSON representation of a sheet returned by the sheet API.
// Internal storage paths are excluded; only client-useful fields are included.
type sheetResponse struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Institution      string     `json:"institution,omitempty"`
	SourceURL        string     `json:"source_url,omitempty"`
	RightsNote       string     `json:"rights_note,omitempty"`
	Status           string     `json:"status"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	RetryCount       int32      `json:"retry_count"`
	OriginalChecksum string     `json:"original_checksum"`
	OriginalSize     int64      `json:"original_size"`
	IIIFBaseURL      string     `json:"iiif_base_url"`
	ManifestURL      string     `json:"manifest_url"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// toSheetResponse converts a domain sheet into the API response type.
func (handler *Handler) toSheetResponse(s *model.Sheet) sheetResponse {
	return sheetResponse{
		ID:               s.ID,
		Title:            s.Title,
		Institution:      s.Institution,
		SourceURL:        s.SourceURL,
		RightsNote:       s.RightsNote,
		Status:           string(s.Status),
		ErrorMessage:     s.ErrorMessage,
		RetryCount:       s.RetryCount,
		OriginalChecksum: s.OriginalChecksum,
		OriginalSize:     s.OriginalSize,
		IIIFBaseURL:      handler.baseURL + "/iiif/" + s.ID,
		ManifestURL:      handler.baseURL + "/iiif/" + s.ID + "/manifest",
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		DeletedAt:        s.DeletedAt,
	}
}

// imageInfoURL returns the info.json URL on the image server for a given sheet ID.
// Cantaloupe's FilesystemSource maps the identifier sheets/{id}/pyramid.tif to
// the file at {STORAGE_ROOT}/sheets/{id}/pyramid.tif.
func (handler *Handler) imageInfoURL(sheetID string) string {
	return handler.imageServerURL + "/iiif/2/" + url.PathEscape("sheets/"+sheetID+"/pyramid.tif") + "/info.json"
}

// imageTileURL returns the tile URL on the image server for a given sheet ID and tile suffix.
// suffix is the IIIF Image API path after the identifier, e.g. "full/max/0/default.jpg".
func (handler *Handler) imageTileURL(sheetID, suffix string) string {
	return handler.imageServerURL + "/iiif/2/" + url.PathEscape("sheets/"+sheetID+"/pyramid.tif") + "/" + suffix
}
