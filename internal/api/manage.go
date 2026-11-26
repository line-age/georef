package api

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/manage.html
var manageTemplateSource string

var manageTemplate = template.Must(template.New("manage").Parse(manageTemplateSource))

const logMsgRenderManageFailed = "render manage page"

// managePageData is the template data passed to manage.html.
type managePageData struct {
	Sheets      []sheetResponse
	Limit       int
	Offset      int
	PrevOffset  int  // only valid when HasPrev is true
	NextOffset  int  // only valid when HasNext is true
	HasPrev     bool
	HasNext     bool
	AuthEnabled bool
	LoggedIn    bool
}

// serveManageSheets handles GET /manage.
// Lists all non-deleted sheets with their visibility state and controls to toggle it.
// Unauthenticated requests are redirected to /auth/login (RequireLoginRedirect middleware).
func (handler *Handler) serveManageSheets(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sheets, err := handler.sheets.List(r.Context(), filter)
	if err != nil {
		slog.ErrorContext(r.Context(), logMsgListSheetsFailed, "err", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return
	}

	responses := make([]sheetResponse, len(sheets))
	for idx, sheetRecord := range sheets {
		responses[idx] = handler.toSheetResponse(sheetRecord)
	}

	prevOffset := filter.Offset - filter.Limit
	if prevOffset < 0 {
		prevOffset = 0
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := manageTemplate.Execute(w, managePageData{
		Sheets:      responses,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
		PrevOffset:  prevOffset,
		NextOffset:  filter.Offset + filter.Limit,
		HasPrev:     filter.Offset > 0,
		HasNext:     len(responses) == filter.Limit,
		AuthEnabled: handler.authenticator.IsEnabled(),
		LoggedIn:    handler.authenticator.IsLoggedIn(r),
	}); err != nil {
		slog.ErrorContext(r.Context(), logMsgRenderManageFailed, "err", err)
	}
}
