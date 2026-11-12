package sheet_test

import (
	"context"
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

	// Spin up a real database container. Requires Docker.
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

	access := sheet.NewDB(pool)

	t.Run("create and get", func(t *testing.T) {
		s := newTestSheet("01JTEST00000000000000000001")
		if err := access.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := access.GetByID(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Title != s.Title {
			t.Errorf("title: got %q, want %q", got.Title, s.Title)
		}
		if got.Status != model.StatusProcessing {
			t.Errorf("status: got %q, want %q", got.Status, model.StatusProcessing)
		}
	})

	t.Run("claim next picks up processing sheet", func(t *testing.T) {
		s := newTestSheet("01JTEST00000000000000000002")
		if err := access.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		claimed, err := access.ClaimNext(ctx)
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if claimed == nil {
			t.Fatal("ClaimNext: got nil, want a sheet")
		}
	})

	t.Run("soft delete sets status and deleted_at", func(t *testing.T) {
		s := newTestSheet("01JTEST00000000000000000003")
		if err := access.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := access.SoftDelete(ctx, s.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		got, err := access.GetByID(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetByID after delete: %v", err)
		}
		if got.Status != model.StatusDeleted {
			t.Errorf("status after delete: got %q, want %q", got.Status, model.StatusDeleted)
		}
		if got.DeletedAt == nil {
			t.Error("deleted_at should be set after soft delete")
		}
	})

	t.Run("get unknown id returns ErrNotFound", func(t *testing.T) {
		_, err := access.GetByID(ctx, "DOESNOTEXIST")
		var nf sheet.ErrNotFound
		if !isErrNotFound(err, &nf) {
			t.Errorf("expected ErrNotFound, got %T: %v", err, err)
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
	_, err := pool.Exec(ctx, `
		CREATE TYPE IF NOT EXISTS sheet_status AS ENUM ('processing','ready','failed','deleted');

		CREATE TABLE IF NOT EXISTS sheets (
			id                  TEXT            PRIMARY KEY,
			title               TEXT            NOT NULL,
			institution         TEXT,
			source_url          TEXT,
			rights_note         TEXT,
			status              sheet_status    NOT NULL DEFAULT 'processing',
			error_message       TEXT,
			original_checksum   TEXT            NOT NULL,
			original_size       BIGINT          NOT NULL,
			original_path       TEXT            NOT NULL,
			pyramid_path        TEXT,
			uploader_github_id  BIGINT,
			created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
			updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
			deleted_at          TIMESTAMPTZ
		);
	`)
	return err
}

func isErrNotFound(err error, target *sheet.ErrNotFound) bool {
	if err == nil {
		return false
	}
	nf, ok := err.(sheet.ErrNotFound)
	if ok {
		*target = nf
	}
	return ok
}
