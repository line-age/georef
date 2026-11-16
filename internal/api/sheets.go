package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
)

// errDeletedSheet is returned from the Update mutate func when the row is already deleted.
// This sentinel propagates out of the transaction so the handler can return 410.
var errDeletedSheet = errors.New("sheet is deleted")

const (
	msgSheetNotFound   = "sheet not found"
	msgSheetGone       = "sheet permanently removed"
	msgInvalidJSON     = "invalid JSON body"
	msgInternalError   = "internal server error"
	msgTitleRequiredPatch = "title is required"

	msgInvalidLimit  = "limit must be an integer between 1 and 100"
	msgInvalidOffset = "offset must be a non-negative integer"
	msgInvalidStatus = "status must be one of: processing, ready, failed"

	defaultListLimit = 50
	maxListLimit     = 100

	logMsgGetSheetFailed    = "get sheet"
	logMsgListSheetsFailed  = "list sheets"
	logMsgUpdateSheetFailed = "update sheet"
	logMsgDeleteSheetFailed = "delete sheet"
)

// getSheet handles GET /sheets/{id}.
// Returns 200 with sheet JSON, 404 if not found, 410 if deleted.
func (handler *Handler) getSheet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	s, err := handler.sheets.GetByID(r.Context(), id)
	if err != nil {
		var notFound sheet.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, msgSheetNotFound)
		} else {
			slog.ErrorContext(r.Context(), logMsgGetSheetFailed, "id", id, "err", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
		}
		return
	}

	if s.Status == model.StatusDeleted {
		writeGone(w, msgSheetGone)
		return
	}

	writeJSON(w, http.StatusOK, handler.toSheetResponse(s))
}

// listSheets handles GET /sheets.
// Supports optional query params: limit, offset, status (processing|ready|failed).
// Deleted sheets are always excluded.
func (handler *Handler) listSheets(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, map[string]any{
		"sheets": responses,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// updateSheetRequest is the JSON body accepted by PATCH /sheets/{id}.
// All editable fields are replaced; title is required.
type updateSheetRequest struct {
	Title       string `json:"title"`
	Institution string `json:"institution"`
	SourceURL   string `json:"source_url"`
	RightsNote  string `json:"rights_note"`
}

// updateSheet handles PATCH /sheets/{id}.
// Replaces all editable metadata fields. Returns 200 with the updated sheet.
// Returns 404 if not found, 410 if deleted, 400 if validation fails.
func (handler *Handler) updateSheet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var updateRequest updateSheetRequest
	if err := json.NewDecoder(r.Body).Decode(&updateRequest); err != nil {
		writeError(w, http.StatusBadRequest, msgInvalidJSON)
		return
	}

	if updateRequest.Title == "" {
		writeError(w, http.StatusBadRequest, msgTitleRequiredPatch)
		return
	}

	// The deleted check runs inside the transaction so the row lock prevents a
	// concurrent DELETE from slipping between a pre-check and the UPDATE.
	updated, updateErr := handler.sheets.Update(r.Context(), id, func(s *model.Sheet) error {
		if s.Status == model.StatusDeleted {
			return errDeletedSheet
		}
		s.Title = updateRequest.Title
		s.Institution = updateRequest.Institution
		s.SourceURL = updateRequest.SourceURL
		s.RightsNote = updateRequest.RightsNote
		return nil
	})
	if updateErr != nil {
		if errors.Is(updateErr, errDeletedSheet) {
			writeGone(w, msgSheetGone)
			return
		}
		var notFound sheet.ErrNotFound
		if errors.As(updateErr, &notFound) {
			writeError(w, http.StatusNotFound, msgSheetNotFound)
			return
		}
		slog.ErrorContext(r.Context(), logMsgUpdateSheetFailed, "id", id, "err", updateErr)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return
	}

	writeJSON(w, http.StatusOK, handler.toSheetResponse(updated))
}

// deleteSheet handles DELETE /sheets/{id}.
// Soft-deletes the sheet; the row and ID are retained permanently.
// Returns 204 on success (including if already deleted), 404 if not found.
func (handler *Handler) deleteSheet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := handler.sheets.SoftDelete(r.Context(), id)
	if err != nil {
		var notFound sheet.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, msgSheetNotFound)
		} else {
			slog.ErrorContext(r.Context(), logMsgDeleteSheetFailed, "id", id, "err", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseListFilter reads limit, offset, and status query params from r.
func parseListFilter(r *http.Request) (sheet.ListFilter, error) {
	limit := defaultListLimit
	offset := 0

	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxListLimit {
			return sheet.ListFilter{}, errors.New(msgInvalidLimit)
		}
		limit = value
	}

	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return sheet.ListFilter{}, errors.New(msgInvalidOffset)
		}
		offset = value
	}

	var statusFilter *model.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		status := model.Status(raw)
		if status != model.StatusProcessing && status != model.StatusReady && status != model.StatusFailed {
			return sheet.ListFilter{}, errors.New(msgInvalidStatus)
		}
		statusFilter = &status
	}

	return sheet.ListFilter{
		Status: statusFilter,
		Limit:  limit,
		Offset: offset,
	}, nil
}
