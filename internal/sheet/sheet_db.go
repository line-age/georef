package sheet

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/line-age/georef/internal/model"
)

// db implements Access against a connection pool.
type db struct {
	pool *pgxpool.Pool
}

// NewDB returns an Accessor backed by the given connection pool.
func NewDB(pool *pgxpool.Pool) Accessor {
	return &db{pool: pool}
}

func (d *db) Create(ctx context.Context, s *model.Sheet) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO sheets (
			id, title, institution, source_url, rights_note,
			status, original_checksum, original_size, original_path,
			uploader_github_id
		) VALUES ($1,$2,$3,$4,$5,'processing',$6,$7,$8,$9)`,
		s.ID, s.Title, s.Institution, s.SourceURL, s.RightsNote,
		s.OriginalChecksum, s.OriginalSize, s.OriginalPath,
		s.UploaderGitHubID,
	)
	return err
}

func (d *db) GetByID(ctx context.Context, id string) (*model.Sheet, error) {
	row := d.pool.QueryRow(ctx, `SELECT * FROM sheets WHERE id = $1`, id)
	s, err := scanSheet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound{ID: id}
	}
	return s, err
}

func (d *db) List(ctx context.Context, f ListFilter) ([]*model.Sheet, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.pool.Query(ctx, `
		SELECT * FROM sheets
		WHERE ($1::sheet_status IS NULL OR status = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		f.Status, limit, f.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sheets []*model.Sheet
	for rows.Next() {
		s, err := scanSheet(rows)
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, s)
	}
	return sheets, rows.Err()
}

func (d *db) Update(ctx context.Context, id string, fn func(*model.Sheet) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row := tx.QueryRow(ctx, `SELECT * FROM sheets WHERE id = $1 FOR UPDATE`, id)
	s, err := scanSheet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound{ID: id}
	}
	if err != nil {
		return err
	}

	if err := fn(s); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE sheets SET
			title        = $2,
			institution  = $3,
			source_url   = $4,
			rights_note  = $5,
			updated_at   = now()
		WHERE id = $1`,
		s.ID, s.Title, s.Institution, s.SourceURL, s.RightsNote,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *db) SoftDelete(ctx context.Context, id string) error {
	tag, err := d.pool.Exec(ctx, `
		UPDATE sheets SET
			status      = 'deleted',
			deleted_at  = now(),
			updated_at  = now()
		WHERE id = $1 AND status != 'deleted'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound{ID: id}
	}
	return nil
}

func (d *db) ClaimNext(ctx context.Context) (*model.Sheet, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT * FROM sheets
		WHERE status IN ('processing', 'failed')
		ORDER BY created_at
		LIMIT 1
		FOR UPDATE SKIP LOCKED`)
	s, err := scanSheet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// scanSheet reads a sheet row from any pgx Row or Rows scanner.
func scanSheet(row interface {
	Scan(...any) error
}) (*model.Sheet, error) {
	var s model.Sheet
	// Nullable columns scanned into pointers; non-pointer fields in the struct
	// hold their zero value when the column is NULL.
	var errorMessage, institution, sourceURL, rightsNote, pyramidPath *string
	var deletedAt *time.Time

	err := row.Scan(
		&s.ID,
		&s.Title,
		&institution,
		&sourceURL,
		&rightsNote,
		&s.Status,
		&errorMessage,
		&s.OriginalChecksum,
		&s.OriginalSize,
		&s.OriginalPath,
		&pyramidPath,
		&s.UploaderGitHubID,
		&s.CreatedAt,
		&s.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}
	s.Institution  = derefStr(institution)
	s.SourceURL    = derefStr(sourceURL)
	s.RightsNote   = derefStr(rightsNote)
	s.ErrorMessage = derefStr(errorMessage)
	s.PyramidPath  = derefStr(pyramidPath)
	s.DeletedAt    = deletedAt
	return &s, nil
}

// derefStr returns the string value of a nullable pointer, or "" if nil.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
