package api

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/upload.html
var uploadTemplateSource string

var uploadTemplate = template.Must(template.New("upload").Parse(uploadTemplateSource))

// templateData holds the values rendered into the upload form template.
type templateData struct {
	BaseURL     string
	AuthEnabled bool
	LoggedIn    bool
}

// serveUploadForm renders the scan upload form at GET /.
func (handler *Handler) serveUploadForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uploadTemplate.Execute(w, templateData{
		BaseURL:     handler.baseURL,
		AuthEnabled: handler.authenticator.IsEnabled(),
		LoggedIn:    handler.authenticator.IsLoggedIn(r),
	}); err != nil {
		slog.ErrorContext(r.Context(), "render upload form", "err", err)
	}
}
