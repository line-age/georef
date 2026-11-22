// Package worker polls the sheet queue and converts originals to pyramidal TIFFs.
package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/line-age/georef/internal/convert"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

const (
	errMsgOpenOriginalScan  = "open original scan"
	errMsgCopyOriginalScan  = "copy original scan to temp"
	errMsgCreateTempFile    = "create temp file"
	errMsgOpenConvertedFile = "open converted file"
	errMsgSavePyramidalTIFF = "save pyramidal tiff"

	logMsgProcessNextFailed = "worker: process next"
	logMsgSheetConverted    = "worker: sheet converted"
)

// Worker claims sheets from the queue and converts them one at a time.
type Worker struct {
	sheets       sheet.Accessor
	store        storage.Store
	converter    convert.Converter
	pollInterval time.Duration
	maxRetries   int
}

// New returns a Worker wired to the given dependencies.
func New(sheets sheet.Accessor, store storage.Store, converter convert.Converter, pollInterval time.Duration, maxRetries int) *Worker {
	return &Worker{
		sheets:       sheets,
		store:        store,
		converter:    converter,
		pollInterval: pollInterval,
		maxRetries:   maxRetries,
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

		found, err := worker.sheets.ProcessNext(ctx, worker.maxRetries, worker.convertSheet)
		if err != nil {
			slog.ErrorContext(ctx, logMsgProcessNextFailed, "err", err)
		}

		// When a job was found, re-check the queue immediately — there may be more.
		// Sleep only when the queue is empty, so the loop does not busy-spin.
		if !found {
			select {
			case <-ctx.Done():
				return
			case <-time.After(worker.pollInterval):
			}
		}
	}
}

// convertSheet is passed to ProcessNext as the conversion callback.
// It runs inside the sheet's database transaction so the row lock is held
// for the duration of the conversion.
func (worker *Worker) convertSheet(ctx context.Context, sheet *model.Sheet) (pyramidPath string, err error) {
	// Stream the original scan to a local temp file so vips always receives a
	// local path. For the filesystem backend OpenOriginalScan opens the file
	// in place; for the S3 backend it downloads from the bucket. Both cases
	// land at a local path the vips subprocess can read.
	originalReader, err := worker.store.OpenOriginalScan(ctx, sheet.ID)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgOpenOriginalScan, err)
	}
	defer originalReader.Close() //nolint:errcheck

	srcFile, err := os.CreateTemp("", "original-*.scan")
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgCreateTempFile, err)
	}
	srcPath := srcFile.Name()
	defer os.Remove(srcPath) //nolint:errcheck

	if _, err := io.Copy(srcFile, originalReader); err != nil {
		srcFile.Close() //nolint:errcheck
		return "", fmt.Errorf("%s: %w", errMsgCopyOriginalScan, err)
	}
	if err := srcFile.Close(); err != nil {
		return "", fmt.Errorf("%s: %w", errMsgCreateTempFile, err)
	}

	// vips writes to a file path; give it a second temp for the output.
	dstFile, err := os.CreateTemp("", "pyramid-*.tif")
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgCreateTempFile, err)
	}
	dstPath := dstFile.Name()
	dstFile.Close()
	defer os.Remove(dstPath) //nolint:errcheck

	convertStart := time.Now()
	if err := worker.converter.Convert(ctx, srcPath, dstPath); err != nil {
		return "", err
	}
	convertDuration := time.Since(convertStart)

	pyramidFile, err := os.Open(dstPath)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgOpenConvertedFile, err)
	}
	defer pyramidFile.Close() //nolint:errcheck

	storedPath, err := worker.store.SavePyramidalTIFF(ctx, sheet.ID, pyramidFile)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsgSavePyramidalTIFF, err)
	}

	slog.InfoContext(ctx, logMsgSheetConverted, "id", sheet.ID, "convert_ms", convertDuration.Milliseconds())
	return storedPath, nil
}
