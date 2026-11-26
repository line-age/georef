package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
)

// Routes returns the fully configured router with all API routes registered.
func (handler *Handler) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	// Upload form
	router.Get("/", handler.serveUploadForm)

	// Sheet management page — lists all sheets with visibility controls.
	router.With(handler.authenticator.RequireLoginRedirect).Get("/manage", handler.serveManageSheets)

	// API reference — Swagger UI at /docs, raw spec at /docs/openapi.yaml
	router.Get("/docs", handler.serveAPIDocs)
	router.Get("/docs/openapi.yaml", handler.serveOpenAPISpec)

	// Auth — login, callback, logout
	router.Get("/auth/login", handler.authenticator.LoginHandler)
	router.Get("/auth/callback", handler.authenticator.CallbackHandler)
	router.Post("/auth/logout", handler.authenticator.LogoutHandler)

	// Sheet API — reads are open; writes require a GitHub login in an allowed team.
	router.Get("/sheets", handler.listSheets)
	router.Get("/sheets/{id}", handler.getSheet)
	router.With(handler.authenticator.RequireLogin).Post("/sheets", handler.uploadSheet)
	router.With(handler.authenticator.RequireLogin).Patch("/sheets/{id}", handler.updateSheet)
	router.With(handler.authenticator.RequireLogin).Delete("/sheets/{id}", handler.deleteSheet)

	// IIIF Presentation API and Image API.
	// CORS is required so browser-based IIIF viewers can fetch manifests, info.json
	// and tiles cross-origin. RequireIIIFAuth is a no-op when IIIF_AUTH_REQUIRED is false
	// (the default). When enabled it checks for a valid session or Authorization: Bearer token.
	iiifCORS := newIIIFCORSHandler(handler.corsAllowedOrigins)
	router.Group(func(iiifRouter chi.Router) {
		iiifRouter.Use(iiifCORS.Handler)
		iiifRouter.With(handler.authenticator.RequireIIIFAuth).Get("/iiif/{id}/manifest", handler.serveManifest)
		iiifRouter.With(handler.authenticator.RequireIIIFAuth).Get("/iiif/{id}/info.json", handler.serveInfoJSON)
		iiifRouter.With(handler.authenticator.RequireIIIFAuth).Get("/iiif/{id}/*", handler.serveTile)
	})

	return router
}

// newIIIFCORSHandler returns a cors.Cors configured for IIIF endpoints.
// When allowedOrigins is ["*"] every origin is accepted without credentials.
// A specific allowlist enables credentialed requests and correct Vary handling.
//
// Important: when allowedOrigins is ["*"], Allow-Credentials is false. The
// browser therefore strips session cookies from cross-origin IIIF requests,
// so private sheets (public=false) always return 401 for cross-origin viewers.
// To serve private sheets to cross-origin viewers, set CORS_ALLOWED_ORIGINS
// to an explicit allowlist instead of *.
func newIIIFCORSHandler(allowedOrigins []string) *cors.Cors {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"
	return cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: !allowAll,
		AllowedMethods:   []string{http.MethodGet, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Accept"},
	})
}
