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

	// Upload form
	router.Get("/", handler.serveUploadForm)

	// Auth — login, callback, logout
	router.Get("/auth/login", handler.authenticator.LoginHandler)
	router.Get("/auth/callback", handler.authenticator.CallbackHandler)
	router.Post("/auth/logout", handler.authenticator.LogoutHandler)

	// Sheet API — reads are open; writes require a GitHub login in an allowed team.
	router.Get("/sheets", handler.listSheets)
	router.Get("/sheets/{id}", handler.getSheet)
	router.With(handler.authenticator.RequireWrite).Post("/sheets", handler.uploadSheet)
	router.With(handler.authenticator.RequireWrite).Patch("/sheets/{id}", handler.updateSheet)
	router.With(handler.authenticator.RequireWrite).Delete("/sheets/{id}", handler.deleteSheet)

	// IIIF Presentation API and Image API.
	// RequireRead is a no-op when READ_LOGIN_REQUIRED is false (the default).
	// When enabled it checks for a valid session or Authorization: Bearer token.
	router.With(handler.authenticator.RequireRead).Get("/iiif/{id}/manifest", handler.serveManifest)
	router.With(handler.authenticator.RequireRead).Get("/iiif/{id}/info.json", handler.serveInfoJSON)
	router.With(handler.authenticator.RequireRead).Get("/iiif/{id}/*", handler.serveTile)

	return router
}
