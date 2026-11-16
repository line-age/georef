package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes returns the fully configured router with all API routes registered.
func (handler *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	// Sheet API
	router.Post("/sheets", handler.uploadSheet)
	router.Get("/sheets", handler.listSheets)
	router.Get("/sheets/{id}", handler.getSheet)
	router.Patch("/sheets/{id}", handler.updateSheet)
	router.Delete("/sheets/{id}", handler.deleteSheet)

	// IIIF Presentation API
	router.Get("/iiif/{id}/manifest", handler.serveManifest)

	// IIIF Image API — proxied to the internal image server.
	// info.json is handled separately because the @id field must be rewritten
	// to the public URL before the response is forwarded to the client.
	router.Get("/iiif/{id}/info.json", handler.serveInfoJSON)
	router.Get("/iiif/{id}/*", handler.serveTile)

	return router
}
