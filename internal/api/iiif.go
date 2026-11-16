package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/line-age/georef/internal/iiif"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
)

const (
	// retryAfterSeconds is the Retry-After value sent when a sheet is still processing.
	// A rough estimate of minimum conversion time for a typical scan.
	retryAfterSeconds = "5"

	msgSheetProcessing = "sheet is being processed; retry after a few seconds"
	msgSheetFailed     = "sheet conversion failed"

	manifestContentType = `application/ld+json;profile="http://iiif.io/api/presentation/3/context.json"`

	logMsgProxyInfoFailed     = "proxy info.json"
	logMsgProxyTileFailed     = "proxy tile"
	logMsgFetchDimsFailed     = "fetch image dimensions"
	logMsgEncodeManifestFailed = "encode manifest"
)

// serveManifest handles GET /iiif/{id}/manifest.
// Builds a IIIF Presentation v3 manifest for a ready sheet.
// IIIF routing: processing → 503, failed → 404, deleted → 410, unknown → 404.
func (handler *Handler) serveManifest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	s := handler.resolveReadySheet(w, r, id)
	if s == nil {
		return
	}

	infoURL := handler.imageInfoURL(id)
	width, height, err := handler.imageProxy.FetchDimensions(r.Context(), infoURL)
	if err != nil {
		slog.ErrorContext(r.Context(), logMsgFetchDimsFailed, "id", id, "err", err)
		writeError(w, http.StatusBadGateway, "image server unavailable")
		return
	}

	manifest := iiif.Build(s, handler.baseURL, width, height)

	w.Header().Set("Content-Type", manifestContentType)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		slog.ErrorContext(r.Context(), logMsgEncodeManifestFailed, "id", id, "err", err)
	}
}

// serveInfoJSON handles GET /iiif/{id}/info.json.
// Proxies the response from the image server, rewriting @id to the public URL.
func (handler *Handler) serveInfoJSON(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	s := handler.resolveReadySheet(w, r, id)
	if s == nil {
		return
	}

	publicIIIFBase := handler.baseURL + "/iiif/" + id
	upstreamURL := handler.imageInfoURL(id)

	if err := handler.imageProxy.ServeInfoJSON(w, r, upstreamURL, publicIIIFBase); err != nil {
		slog.ErrorContext(r.Context(), logMsgProxyInfoFailed, "id", id, "err", err)
		writeError(w, http.StatusBadGateway, "image server unavailable")
	}
}

// serveTile handles GET /iiif/{id}/{*} — the IIIF Image API tile path.
// The wildcard suffix matches region/size/rotation/quality.format and any sub-paths.
func (handler *Handler) serveTile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	s := handler.resolveReadySheet(w, r, id)
	if s == nil {
		return
	}

	tileSuffix := chi.URLParam(r, "*")
	upstreamURL := handler.imageTileURL(id, tileSuffix)

	if err := handler.imageProxy.ServeTile(w, r, upstreamURL); err != nil {
		slog.ErrorContext(r.Context(), logMsgProxyTileFailed, "id", id, "err", err)
		writeError(w, http.StatusBadGateway, "image server unavailable")
	}
}

// resolveReadySheet looks up a sheet by ID and verifies it is in the 'ready' state.
// If the sheet cannot be served (wrong status, missing, deleted), it writes the
// appropriate error response and returns nil. Callers must return immediately on nil.
//
// IIIF status routing (from docs/image-service.md):
//
//	unknown ID   → 404
//	deleted      → 410  (ID is permanently retired; must never become 404)
//	processing   → 503  with Retry-After header
//	failed       → 404  (tile/info only; sheet API still shows the error)
//	ready        → continue
func (handler *Handler) resolveReadySheet(w http.ResponseWriter, r *http.Request, id string) *model.Sheet {
	s, err := handler.sheets.GetByID(r.Context(), id)
	if err != nil {
		var notFound sheet.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, msgSheetNotFound)
		} else {
			slog.ErrorContext(r.Context(), logMsgGetSheetFailed, "id", id, "err", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
		}
		return nil
	}

	switch s.Status {
	case model.StatusDeleted:
		writeGone(w, msgSheetGone)
		return nil
	case model.StatusProcessing:
		w.Header().Set("Retry-After", retryAfterSeconds)
		writeError(w, http.StatusServiceUnavailable, msgSheetProcessing)
		return nil
	case model.StatusFailed:
		writeError(w, http.StatusNotFound, msgSheetFailed)
		return nil
	}

	return s
}
