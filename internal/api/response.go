package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const logMsgEncodeResponseFailed = "encode json response"

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error(logMsgEncodeResponseFailed, "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
