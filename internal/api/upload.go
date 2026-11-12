package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/line-age/georef/internal/model"
)

const (
	thirtyTwoMB     = 32 << 20
	multipartMaxMem = thirtyTwoMB // memory budget for multipart form parsing

	errMsgStoreFailed  = "failed to store scan"
	errMsgRecordFailed = "failed to create sheet record"
)

// uploadResponse is the JSON body returned on a successful upload.
type uploadResponse struct {
	ID          string `json:"id"`
	SheetURL    string `json:"sheet_url"`
	IIIFBaseURL string `json:"iiif_base_url"`
	ManifestURL string `json:"manifest_url"`
}

func (h *Handler) uploadSheet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize)

	req, err := parseUploadRequest(r)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, msgFileTooLarge)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	defer req.File.Close()

	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id := ulid.Make().String()

	checksum, size, originalPath, err := h.storeOriginalScan(r.Context(), id, req.File)
	if err != nil {
		slog.ErrorContext(r.Context(), "store original scan", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, errMsgStoreFailed)
		return
	}

	s := &model.Sheet{
		ID:               id,
		Title:            req.Title,
		Institution:      req.Institution,
		SourceURL:        req.SourceURL,
		RightsNote:       req.RightsNote,
		OriginalChecksum: checksum,
		OriginalSize:     size,
		OriginalPath:     originalPath,
	}

	if err := h.sheets.Create(r.Context(), s); err != nil {
		slog.ErrorContext(r.Context(), "create sheet record", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, errMsgRecordFailed)
		return
	}

	writeJSON(w, http.StatusAccepted, uploadResponse{
		ID:          id,
		SheetURL:    h.baseURL + "/sheets/" + id,
		IIIFBaseURL: h.baseURL + "/iiif/" + id,
		ManifestURL: h.baseURL + "/iiif/" + id + "/manifest",
	})
}

// storeOriginalScan streams the file to storage in a single pass,
// computing the SHA-256 checksum and byte count along the way.
// It returns the path at which the file was stored.
func (h *Handler) storeOriginalScan(ctx context.Context, id string, file io.Reader) (checksum string, size int64, path string, err error) {
	hash := sha256.New()
	cr := &countingReader{r: io.TeeReader(io.LimitReader(file, h.maxUploadSize), hash)}
	storedPath, err := h.store.SaveOriginalScan(ctx, id, cr)
	if err != nil {
		return "", 0, "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), cr.n, storedPath, nil
}

// countingReader wraps an io.Reader and tracks the total bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	c.n += int64(n)
	return
}
