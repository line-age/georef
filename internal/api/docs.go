package api

import (
	"fmt"
	"log/slog"
	"net/http"
)

const (
	swaggerUIVersion       = "5.17.14"
	openAPISpecCacheMaxAge = 300 // seconds; spec is static content, safe to cache briefly
)

var swaggerUIPage = fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API reference</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@%s/swagger-ui.css">
<style>body { margin: 0; }</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@%s/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({
  url: '/docs/openapi.yaml',
  dom_id: '#swagger-ui',
  deepLinking: true,
  presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
  layout: 'BaseLayout',
});
</script>
</body>
</html>`, swaggerUIVersion, swaggerUIVersion)

func (handler *Handler) serveAPIDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(swaggerUIPage)); err != nil {
		slog.ErrorContext(r.Context(), "write api docs page", "err", err)
	}
}

func (handler *Handler) serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", openAPISpecCacheMaxAge))
	if _, err := w.Write(handler.openAPISpec); err != nil {
		slog.ErrorContext(r.Context(), "write openapi spec", "err", err)
	}
}
