package sheet_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
)

func TestSheetDB(t *testing.T) {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "georef_test",
				"POSTGRES_USER":     "georef",
				"POSTGRES_PASSWORD": "georef",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Cleanup(func() { ctr.Terminate(ctx) }) //nolint:errcheck

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	dsn := "postgres://georef:georef@" + host + ":" + port.Port() + "/georef_test"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}
	defer pool.Close()

	if err := runMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sheets := sheet.NewRepository(pool)

	const defaultMaxRetries = 3

	// drainQueue processes all claimable sheets so each subtest starts with a
	// known-empty queue. Required because subtests share one database.
	drainQueue := func() {
		for {
			found, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
				return "drained", nil
			})
			if err != nil || !found {
				break
			}
		}
	}

	// Must run before any sheet exists in the DB so the queue is genuinely empty.
	t.Run("process_next_returns_false_when_queue_empty", func(t *testing.T) {
		found, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "", nil
		})
		if err != nil {
			t.Fatalf("ProcessNext: %v", err)
		}
		if found {
			t.Error("want false when nothing claimable, got true")
		}
	})

	t.Run("create_and_get", func(t *testing.T) {
		newSheet := newTestSheet("01JTEST00000000000000000001")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}
		fetchedSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if fetchedSheet.Title != newSheet.Title {
			t.Errorf("title: got %q, want %q", fetchedSheet.Title, newSheet.Title)
		}
		if fetchedSheet.Status != model.StatusProcessing {
			t.Errorf("status: got %q, want %q", fetchedSheet.Status, model.StatusProcessing)
		}
	})

	t.Run("process_next_marks_sheet_ready_on_success", func(t *testing.T) {
		drainQueue() // consume any sheets left processing by prior subtests
		newSheet := newTestSheet("01JTEST00000000000000000010")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}

		wantPyramidPath := "sheets/" + newSheet.ID + "/pyramid.tif"
		found, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, claimedSheet *model.Sheet) (string, error) {
			if claimedSheet.ID != newSheet.ID {
				t.Errorf("processor received id %q, want %q", claimedSheet.ID, newSheet.ID)
			}
			return wantPyramidPath, nil
		})
		if err != nil {
			t.Fatalf("ProcessNext: %v", err)
		}
		if !found {
			t.Fatal("want found=true, got false")
		}

		processedSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if processedSheet.Status != model.StatusReady {
			t.Errorf("status: got %q, want %q", processedSheet.Status, model.StatusReady)
		}
		if processedSheet.PyramidPath != wantPyramidPath {
			t.Errorf("pyramid_path: got %q, want %q", processedSheet.PyramidPath, wantPyramidPath)
		}
	})

	t.Run("process_next_requeues_sheet_on_fn_error_with_retries_remaining", func(t *testing.T) {
		drainQueue() // consume any sheets left processing by prior subtests
		newSheet := newTestSheet("01JTEST00000000000000000011")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}

		convertErr := errors.New("vips exited with code 1")
		found, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "", convertErr
		})
		if err != nil {
			t.Fatalf("ProcessNext: %v", err)
		}
		if !found {
			t.Fatal("want found=true, got false")
		}

		// With retries remaining, the sheet re-enters the queue as processing.
		requeuedSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if requeuedSheet.Status != model.StatusProcessing {
			t.Errorf("status: got %q, want %q", requeuedSheet.Status, model.StatusProcessing)
		}
		if requeuedSheet.RetryCount != 1 {
			t.Errorf("retry_count: got %d, want 1", requeuedSheet.RetryCount)
		}
		if requeuedSheet.ErrorMessage != convertErr.Error() {
			t.Errorf("error_message: got %q, want %q", requeuedSheet.ErrorMessage, convertErr.Error())
		}
	})

	t.Run("process_next_picks_up_failed_sheet", func(t *testing.T) {
		drainQueue() // consume any sheets left processing/failed by prior subtests
		newSheet := newTestSheet("01JTEST00000000000000000012")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// First attempt: fail — with retries remaining, status returns to processing.
		_, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "", errors.New("transient error")
		})
		if err != nil {
			t.Fatalf("ProcessNext (fail): %v", err)
		}

		// Second attempt: succeed — processing sheets with retry_count < max_retries are claimable.
		found, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "sheets/" + newSheet.ID + "/pyramid.tif", nil
		})
		if err != nil {
			t.Fatalf("ProcessNext (retry): %v", err)
		}
		if !found {
			t.Error("want found=true on retry, got false")
		}

		retriedSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if retriedSheet.Status != model.StatusReady {
			t.Errorf("status after retry: got %q, want %q", retriedSheet.Status, model.StatusReady)
		}
	})

	t.Run("soft_delete_sets_status_and_deleted_at", func(t *testing.T) {
		newSheet := newTestSheet("01JTEST00000000000000000003")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := sheets.SoftDelete(ctx, newSheet.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		deletedSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID after delete: %v", err)
		}
		if deletedSheet.Status != model.StatusDeleted {
			t.Errorf("status after delete: got %q, want %q", deletedSheet.Status, model.StatusDeleted)
		}
		if deletedSheet.DeletedAt == nil {
			t.Error("deleted_at should be set after soft delete")
		}
	})

	t.Run("get_unknown_id_returns_ErrNotFound", func(t *testing.T) {
		_, err := sheets.GetByID(ctx, "DOESNOTEXIST")
		var notFoundErr sheet.ErrNotFound
		if !errors.As(err, &notFoundErr) {
			t.Errorf("expected ErrNotFound, got %T: %v", err, err)
		}
	})

	t.Run("process_next_increments_retry_count_on_failure", func(t *testing.T) {
		drainQueue()
		newSheet := newTestSheet("01JTEST00000000000000000013")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}

		_, err := sheets.ProcessNext(ctx, defaultMaxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "", errors.New("transient")
		})
		if err != nil {
			t.Fatalf("ProcessNext: %v", err)
		}

		afterFirstFail, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if afterFirstFail.RetryCount != 1 {
			t.Errorf("retry_count after first failure: got %d, want 1", afterFirstFail.RetryCount)
		}
		// Status must be processing — retries remain.
		if afterFirstFail.Status != model.StatusProcessing {
			t.Errorf("status after first failure: got %q, want processing", afterFirstFail.Status)
		}
	})

	t.Run("process_next_marks_terminal_when_retries_exhausted", func(t *testing.T) {
		drainQueue()
		newSheet := newTestSheet("01JTEST00000000000000000014")
		if err := sheets.Create(ctx, newSheet); err != nil {
			t.Fatalf("Create: %v", err)
		}

		maxRetries := 2
		convertErr := errors.New("permanent failure")

		for i := 0; i < maxRetries; i++ {
			_, err := sheets.ProcessNext(ctx, maxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
				return "", convertErr
			})
			if err != nil {
				t.Fatalf("ProcessNext attempt %d: %v", i+1, err)
			}
		}

		terminalSheet, err := sheets.GetByID(ctx, newSheet.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if terminalSheet.Status != model.StatusFailed {
			t.Errorf("status after exhausted retries: got %q, want failed", terminalSheet.Status)
		}
		if terminalSheet.RetryCount != int32(maxRetries) {
			t.Errorf("retry_count: got %d, want %d", terminalSheet.RetryCount, maxRetries)
		}

		// Exhausted sheet must not be claimable.
		found, err := sheets.ProcessNext(ctx, maxRetries, func(_ context.Context, _ *model.Sheet) (string, error) {
			return "should_not_reach", nil
		})
		if err != nil {
			t.Fatalf("ProcessNext after exhaustion: %v", err)
		}
		if found {
			t.Error("exhausted sheet must not be re-claimed")
		}
	})
}

func newTestSheet(id string) *model.Sheet {
	return &model.Sheet{
		ID:               id,
		Title:            "Test Sheet",
		Institution:      "Test Institution",
		OriginalChecksum: "abc123",
		OriginalSize:     1024,
		OriginalPath:     "sheets/" + id + "/original",
	}
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	files := []string{
		"../../db/migrations/001_create_sheets.up.sql",
		"../../db/migrations/002_add_retry_count.up.sql",
		"../../db/migrations/003_fix_claimable_idx.up.sql",
		"../../db/migrations/004_add_public_to_sheets.up.sql",
	}
	for _, path := range files {
		sql, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("exec %s: %w", path, err)
		}
	}
	return nil
}
