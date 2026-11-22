package api

import "net/http"

const swaggerUIPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API reference</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
<style>body { margin: 0; }</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
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
</html>`

func (handler *Handler) serveAPIDocs(responseWriter http.ResponseWriter, _ *http.Request) {
	responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = responseWriter.Write([]byte(swaggerUIPage))
}

func (handler *Handler) serveOpenAPISpec(responseWriter http.ResponseWriter, _ *http.Request) {
	responseWriter.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	responseWriter.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = responseWriter.Write(handler.openAPISpec)
}
