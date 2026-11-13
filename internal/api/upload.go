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
	multipartMemoryLimit = 32 << 20 // bytes kept in memory when parsing multipart form; excess spills to disk

	msgStoreFailed  = "failed to store scan"
	msgRecordFailed = "failed to create sheet record"

	logMsgStoreOriginalScanFailed = "store original scan"
	logMsgCreateSheetRecordFailed = "create sheet record"
)

// uploadResponse is the JSON body returned on a successful upload.
type uploadResponse struct {
	ID          string `json:"id"`
	SheetURL    string `json:"sheet_url"`
	IIIFBaseURL string `json:"iiif_base_url"`
	ManifestURL string `json:"manifest_url"`
}

func (handler *Handler) uploadSheet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, handler.maxUploadSize)

	uploadFormData, err := parseUploadRequest(r)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, msgFileTooLarge)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	defer uploadFormData.File.Close()

	if err := uploadFormData.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id := ulid.Make().String()

	checksum, size, originalPath, err := handler.storeOriginalScan(r.Context(), id, uploadFormData.File)
	if err != nil {
		slog.ErrorContext(r.Context(), logMsgStoreOriginalScanFailed, "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, msgStoreFailed)
		return
	}

	sheet := &model.Sheet{
		ID:               id,
		Title:            uploadFormData.Title,
		Institution:      uploadFormData.Institution,
		SourceURL:        uploadFormData.SourceURL,
		RightsNote:       uploadFormData.RightsNote,
		OriginalChecksum: checksum,
		OriginalSize:     size,
		OriginalPath:     originalPath,
	}

	if err := handler.sheets.Create(r.Context(), sheet); err != nil {
		slog.ErrorContext(r.Context(), logMsgCreateSheetRecordFailed, "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, msgRecordFailed)
		return
	}

	writeJSON(w, http.StatusAccepted, uploadResponse{
		ID:          id,
		SheetURL:    handler.baseURL + "/sheets/" + id,
		IIIFBaseURL: handler.baseURL + "/iiif/" + id,
		ManifestURL: handler.baseURL + "/iiif/" + id + "/manifest",
	})
}

// storeOriginalScan streams the file to storage in a single pass,
// computing the SHA-256 checksum and byte count along the way.
// It returns the path at which the file was stored.
func (handler *Handler) storeOriginalScan(ctx context.Context, id string, file io.Reader) (checksum string, size int64, path string, err error) {
	hash := sha256.New()
	counter := &countingReader{reader: io.TeeReader(io.LimitReader(file, handler.maxUploadSize), hash)}
	storedPath, err := handler.store.SaveOriginalScan(ctx, id, counter)
	if err != nil {
		return "", 0, "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), counter.bytesRead, storedPath, nil
}

// countingReader wraps an io.Reader and tracks the total bytes read.
type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (counter *countingReader) Read(buf []byte) (n int, err error) {
	n, err = counter.reader.Read(buf)
	counter.bytesRead += int64(n)
	return
}
