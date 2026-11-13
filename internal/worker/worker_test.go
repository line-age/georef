package worker_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/line-age/georef/internal/convert"
	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
	"github.com/line-age/georef/internal/worker"
)

// inMemorySheetAccessor implements sheet.Accessor using an in-memory queue.
// It records the final state of each processed sheet in results.
type inMemorySheetAccessor struct {
	queue   []*model.Sheet
	results []*model.Sheet
}

func (accessor *inMemorySheetAccessor) ProcessNext(ctx context.Context, processor func(context.Context, *model.Sheet) (string, error)) (bool, error) {
	if len(accessor.queue) == 0 {
		return false, nil
	}
	claimedSheet := accessor.queue[0]
	accessor.queue = accessor.queue[1:]

	pyramidPath, err := processor(ctx, claimedSheet)
	if err != nil {
		claimedSheet.Status = model.StatusFailed
		claimedSheet.ErrorMessage = err.Error()
	} else {
		claimedSheet.Status = model.StatusReady
		claimedSheet.PyramidPath = pyramidPath
	}
	accessor.results = append(accessor.results, claimedSheet)
	return true, nil
}

func (accessor *inMemorySheetAccessor) Create(_ context.Context, _ *model.Sheet) error { return nil }
func (accessor *inMemorySheetAccessor) GetByID(_ context.Context, _ string) (*model.Sheet, error) {
	return nil, sheet.ErrNotFound{}
}
func (accessor *inMemorySheetAccessor) List(_ context.Context, _ sheet.ListFilter) ([]*model.Sheet, error) {
	return nil, nil
}
func (accessor *inMemorySheetAccessor) Update(_ context.Context, _ string, _ func(*model.Sheet) error) error {
	return nil
}
func (accessor *inMemorySheetAccessor) SoftDelete(_ context.Context, _ string) error { return nil }

// forcedErrorConverter always returns a configured error without touching the filesystem.
// Use it to test conversion failure paths.
type forcedErrorConverter struct{ forcedError error }

func (converter *forcedErrorConverter) Convert(_ context.Context, _, _ string) error {
	return converter.forcedError
}

// fakePyramidConverter writes a placeholder file to the destination path without running vips.
// Use it to test the success path without requiring vips to be installed.
type fakePyramidConverter struct{}

func (fakePyramidConverter) Convert(_ context.Context, _, destinationPath string) error {
	return os.WriteFile(destinationPath, []byte("fake pyramid tiff bytes"), 0o644)
}

var _ convert.Converter = fakePyramidConverter{}
var _ convert.Converter = &forcedErrorConverter{}

func TestWorkerRun(t *testing.T) {
	t.Run("successful_conversion_marks_sheet_ready", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		store := storage.NewLocalStore(t.TempDir())

		sheetID := "01JWORKER00000000000000001"
		if _, err := store.SaveOriginalScan(ctx, sheetID, strings.NewReader("fake scan")); err != nil {
			t.Fatalf("seed original scan: %v", err)
		}

		accessor := &inMemorySheetAccessor{queue: []*model.Sheet{
			{ID: sheetID, Status: model.StatusProcessing, Title: "Map"},
		}}
		testWorker := worker.New(accessor, store, fakePyramidConverter{}, 10*time.Millisecond)

		done := make(chan struct{})
		go func() {
			testWorker.Run(ctx)
			close(done)
		}()

		deadline := time.After(3 * time.Second)
		for {
			if len(accessor.results) > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("sheet was not processed within 3s")
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
		<-done

		processedSheet := accessor.results[0]
		if processedSheet.Status != model.StatusReady {
			t.Errorf("status: got %q, want ready", processedSheet.Status)
		}
		if processedSheet.PyramidPath == "" {
			t.Error("pyramid_path should be set after successful conversion")
		}
	})

	t.Run("convert_error_marks_sheet_failed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		store := storage.NewLocalStore(t.TempDir())

		sheetID := "01JWORKER00000000000000002"
		if _, err := store.SaveOriginalScan(ctx, sheetID, strings.NewReader("fake scan")); err != nil {
			t.Fatalf("seed original scan: %v", err)
		}

		expectedConvertErr := errors.New("vips: not a tiff")
		accessor := &inMemorySheetAccessor{queue: []*model.Sheet{
			{ID: sheetID, Status: model.StatusProcessing, Title: "Map"},
		}}
		testWorker := worker.New(accessor, store, &forcedErrorConverter{forcedError: expectedConvertErr}, 10*time.Millisecond)

		done := make(chan struct{})
		go func() {
			testWorker.Run(ctx)
			close(done)
		}()

		deadline := time.After(3 * time.Second)
		for {
			if len(accessor.results) > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("sheet was not processed within 3s")
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
		<-done

		processedSheet := accessor.results[0]
		if processedSheet.Status != model.StatusFailed {
			t.Errorf("status: got %q, want failed", processedSheet.Status)
		}
		if processedSheet.ErrorMessage != expectedConvertErr.Error() {
			t.Errorf("error_message: got %q, want %q", processedSheet.ErrorMessage, expectedConvertErr.Error())
		}
	})

	t.Run("run_stops_on_context_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		accessor := &inMemorySheetAccessor{} // empty queue — worker will sleep between polls
		store := storage.NewLocalStore(t.TempDir())
		testWorker := worker.New(accessor, store, &forcedErrorConverter{}, 100*time.Millisecond)

		done := make(chan struct{})
		go func() {
			testWorker.Run(ctx)
			close(done)
		}()

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not stop within 2s after context cancel")
		}
	})
}
