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

	// Sheet management page — lists all sheets with visibility controls.
	router.With(handler.authenticator.RequireWrite).Get("/manage", handler.serveManageSheets)

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
	router.With(handler.authenticator.RequireWrite).Post("/sheets", handler.uploadSheet)
	router.With(handler.authenticator.RequireWrite).Patch("/sheets/{id}", handler.updateSheet)
	router.With(handler.authenticator.RequireWrite).Delete("/sheets/{id}", handler.deleteSheet)

	// IIIF Presentation API and Image API.
	// CORS is required so browser-based IIIF viewers can fetch manifests, info.json
	// and tiles cross-origin. OPTIONS preflight routes are registered so chi routes
	// them into the group before the iiifCORSMiddleware writes the preflight response.
	// RequireRead is a no-op when READ_LOGIN_REQUIRED is false (the default).
	// When enabled it checks for a valid session or Authorization: Bearer token.
	noop := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.Group(func(iiifRouter chi.Router) {
		iiifRouter.Use(newIIIFCORSMiddleware(handler.corsAllowedOrigins))
		iiifRouter.Options("/iiif/{id}/manifest", noop)
		iiifRouter.Options("/iiif/{id}/info.json", noop)
		iiifRouter.Options("/iiif/{id}/*", noop)
		iiifRouter.With(handler.authenticator.RequireRead).Get("/iiif/{id}/manifest", handler.serveManifest)
		iiifRouter.With(handler.authenticator.RequireRead).Get("/iiif/{id}/info.json", handler.serveInfoJSON)
		iiifRouter.With(handler.authenticator.RequireRead).Get("/iiif/{id}/*", handler.serveTile)
	})

	return router
}

// newIIIFCORSMiddleware returns a middleware that adds CORS headers for IIIF endpoints.
// When allowedOrigins is ["*"], every origin is accepted and the wildcard is returned.
// When allowedOrigins lists specific origins, the request Origin is checked; a match
// echoes that origin back and adds Vary: Origin so caches key on it correctly.
// OPTIONS preflight is handled before auth middleware so viewers can probe without a session.
func newIIIFCORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"

	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				requestOrigin := r.Header.Get("Origin")
				if allowed[requestOrigin] {
					w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
					w.Header().Add("Vary", "Origin")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Accept")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
