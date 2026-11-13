// Package worker polls the sheet queue and converts originals to pyramidal TIFFs.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/line-age/georef/internal/convert"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

const (
	errMsgGetOriginalScanPath = "original scan path"
	errMsgCreateTempFile      = "create temp file"
	errMsgOpenConvertedFile   = "open converted file"
	errMsgSavePyramidalTIFF   = "save pyramidal tiff"

	logMsgProcessNextFailed = "worker: process next"
	logMsgSheetConverted    = "worker: sheet converted"
)

// Worker claims sheets from the queue and converts them one at a time.
type Worker struct {
	sheets       sheet.Accessor
	store        storage.Store
	converter    convert.Converter
	pollInterval time.Duration
}

// New returns a Worker wired to the given dependencies.
func New(sheets sheet.Accessor, store storage.Store, converter convert.Converter, pollInterval time.Duration) *Worker {
	return &Worker{
		sheets:       sheets,
		store:        store,
		converter:    converter,
		pollInterval: pollInterval,
	}
}

// Run polls for sheets to convert until ctx is cancelled.
// When the queue is empty it sleeps for pollInterval before checking again.
// If ctx is cancelled mid-conversion the current job is abandoned and the
// sheet remains in its previous state to be retried on the next run.
func (worker *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		found, err := worker.sheets.ProcessNext(ctx, worker.convertSheet)
		if err != nil {
			slog.ErrorContext(ctx, logMsgProcessNextFailed, "err", err)
		}

		// Always sleep between iterations. When the queue is empty we wait the
		// full poll interval; when a job was processed we still yield briefly so
		// a permanently-broken sheet (re-enqueued as 'failed' every cycle) does
		// not spin the loop at full speed.
		sleep := worker.pollInterval
		if found {
			sleep = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
	}
}

// convertSheet is passed to ProcessNext as the conversion callback.
// It runs inside the sheet's database transaction so the row lock is held
// for the duration of the conversion.
func (worker *Worker) convertSheet(ctx context.Context, sheet *model.Sheet) (pyramidPath string, err error) {
	originalPath, err := worker.store.OriginalScanPath(ctx, sheet.ID)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgGetOriginalScanPath, err)
	}

	// vips writes to a file path, so we give it a temp file and stream the
	// result into the storage layer once conversion succeeds.
	// Place the temp file in the same directory as the original scan so it
	// lands on the same filesystem as storage, keeping the subsequent copy
	// on-disk rather than through a RAM-backed tmpfs.
	tempFile, err := os.CreateTemp(filepath.Dir(originalPath), "pyramid-*.tif")
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgCreateTempFile, err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath) //nolint:errcheck

	if err := worker.converter.Convert(ctx, originalPath, tempPath); err != nil {
		return "", err
	}

	pyramidFile, err := os.Open(tempPath)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgOpenConvertedFile, err)
	}
	defer pyramidFile.Close() //nolint:errcheck

	storedPath, err := worker.store.SavePyramidalTIFF(ctx, sheet.ID, pyramidFile)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgSavePyramidalTIFF, err)
	}

	slog.InfoContext(ctx, logMsgSheetConverted, "id", sheet.ID)
	return storedPath, nil
}
