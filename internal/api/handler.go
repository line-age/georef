package api

import (
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

// Handler holds the shared dependencies for all HTTP handlers.
type Handler struct {
	sheets        sheet.Accessor
	store         storage.Store
	baseURL       string
	maxUploadSize int64
}

// New returns a Handler wired to the given dependencies.
// baseURL is the public base URL of the service (e.g. "https://maps.example.com").
// maxUploadSize is the maximum accepted request body in bytes.
func New(sheets sheet.Accessor, store storage.Store, baseURL string, maxUploadSize int64) *Handler {
	return &Handler{
		sheets:        sheets,
		store:         store,
		baseURL:       baseURL,
		maxUploadSize: maxUploadSize,
	}
}
