package api

import (
	"fmt"
	"time"

	"github.com/line-age/georef/internal/auth"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/proxy"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

// Handler holds the shared dependencies for all HTTP handlers.
type Handler struct {
	sheets               sheet.Accessor // direct accessor used by sheet API handlers (always fresh)
	iiifSheets           sheet.Accessor // TTL-cached accessor used by IIIF handlers only
	store                storage.Store
	imageProxy           *proxy.Proxy
	authenticator        *auth.Authenticator
	baseURL              string
	imageServerURL       string
	iiifImageAPIVersion  int
	iiifImageAPILevel    int
	retryAfterSeconds    string
	multipartMemoryLimit  int64
	maxUploadSize         int64
	multipartFormOverhead int64
	openAPISpec          []byte
	corsAllowedOrigins   []string
}

// Config holds the tunable parameters for a Handler.
type Config struct {
	BaseURL              string
	ImageServerURL       string
	IIIFImageAPIVersion  int
	IIIFImageAPILevel    int
	RetryAfterSeconds    int
	MultipartMemoryLimit  int64
	MaxUploadSize         int64
	MultipartFormOverhead int64         // extra bytes allowed above MaxUploadSize for multipart boundary and form fields; default 1024
	IIIFStatusCacheTTL    time.Duration // how long IIIF sheet status lookups are cached; 0 disables the cache
	OpenAPISpec          []byte        // embedded OpenAPI spec served at GET /docs/openapi.yaml
	CORSAllowedOrigins   []string      // origins allowed in Access-Control-Allow-Origin; ["*"] allows all
}

// New returns a Handler wired to the given dependencies.
// authenticator may be nil; auth.New with an empty ClientID produces a disabled (passthrough) Authenticator.
func New(sheets sheet.Accessor, store storage.Store, imageProxy *proxy.Proxy, authenticator *auth.Authenticator, cfg Config) *Handler {
	if authenticator == nil {
		authenticator = auth.New(auth.Config{}) // disabled passthrough
	}
	corsOrigins := cfg.CORSAllowedOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"*"}
	}
	formOverhead := cfg.MultipartFormOverhead
	if formOverhead <= 0 {
		formOverhead = 1024
	}
	return &Handler{
		sheets:                sheets,
		iiifSheets:            sheet.NewCachedAccessor(sheets, cfg.IIIFStatusCacheTTL),
		store:                 store,
		imageProxy:            imageProxy,
		authenticator:         authenticator,
		baseURL:               cfg.BaseURL,
		imageServerURL:        cfg.ImageServerURL,
		iiifImageAPIVersion:   cfg.IIIFImageAPIVersion,
		iiifImageAPILevel:     cfg.IIIFImageAPILevel,
		retryAfterSeconds:     fmt.Sprintf("%d", cfg.RetryAfterSeconds),
		multipartMemoryLimit:  cfg.MultipartMemoryLimit,
		maxUploadSize:         cfg.MaxUploadSize,
		multipartFormOverhead: formOverhead,
		openAPISpec:           cfg.OpenAPISpec,
		corsAllowedOrigins:    corsOrigins,
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
	Public           bool       `json:"public"`
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
func (handler *Handler) toSheetResponse(sheetRecord *model.Sheet) sheetResponse {
	return sheetResponse{
		ID:               sheetRecord.ID,
		Title:            sheetRecord.Title,
		Institution:      sheetRecord.Institution,
		SourceURL:        sheetRecord.SourceURL,
		RightsNote:       sheetRecord.RightsNote,
		Public:           sheetRecord.Public,
		Status:           string(sheetRecord.Status),
		ErrorMessage:     sheetRecord.ErrorMessage,
		RetryCount:       sheetRecord.RetryCount,
		OriginalChecksum: sheetRecord.OriginalChecksum,
		OriginalSize:     sheetRecord.OriginalSize,
		IIIFBaseURL:      handler.baseURL + "/iiif/" + sheetRecord.ID,
		ManifestURL:      handler.baseURL + "/iiif/" + sheetRecord.ID + "/manifest",
		CreatedAt:        sheetRecord.CreatedAt,
		UpdatedAt:        sheetRecord.UpdatedAt,
		DeletedAt:        sheetRecord.DeletedAt,
	}
}

// iiifCacheControl returns the Cache-Control value for IIIF responses.
// Private when READ_LOGIN_REQUIRED is on (all tiles gated), or when the
// sheet itself is private (per-sheet visibility takes precedence).
func (handler *Handler) iiifCacheControl(sheetRecord *model.Sheet) string {
	if handler.authenticator.ReadLoginEnabled() || !sheetRecord.Public {
		return "private"
	}
	return "public"
}

// imageInfoURL returns the info.json URL on the image server for a given sheet ID.
// The identifier is the bare ULID. Cantaloupe is configured with
// path_prefix=/data/storage/sheets/ and path_suffix=/pyramid.tif, so it resolves
// the identifier to /data/storage/sheets/{id}/pyramid.tif with no slash encoding needed.
func (handler *Handler) imageInfoURL(sheetID string) string {
	return fmt.Sprintf("%s/iiif/%d/%s/info.json", handler.imageServerURL, handler.iiifImageAPIVersion, sheetID)
}

// imageTileURL returns the tile URL on the image server for a given sheet ID and tile suffix.
// suffix is the IIIF Image API path after the identifier, e.g. "full/max/0/default.jpg".
func (handler *Handler) imageTileURL(sheetID, suffix string) string {
	return fmt.Sprintf("%s/iiif/%d/%s/%s", handler.imageServerURL, handler.iiifImageAPIVersion, sheetID, suffix)
}
