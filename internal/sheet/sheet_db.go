package sheet

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/line-age/georef/internal/db"
	"github.com/line-age/georef/internal/model"
)

// db implements Accessor against a connection pool.
type db struct {
	q    *dbgen.Queries
	pool *pgxpool.Pool
}

// NewDB returns an Accessor backed by the given connection pool.
func NewDB(pool *pgxpool.Pool) Accessor {
	return &db{
		q:    dbgen.New(pool),
		pool: pool,
	}
}

func (d *db) Create(ctx context.Context, s *model.Sheet) error {
	_, err := d.q.CreateSheet(ctx, dbgen.CreateSheetParams{
		ID:               s.ID,
		Title:            s.Title,
		Institution:      pgtype.Text{String: s.Institution, Valid: s.Institution != ""},
		SourceUrl:        pgtype.Text{String: s.SourceURL, Valid: s.SourceURL != ""},
		RightsNote:       pgtype.Text{String: s.RightsNote, Valid: s.RightsNote != ""},
		OriginalChecksum: s.OriginalChecksum,
		OriginalSize:     s.OriginalSize,
		OriginalPath:     s.OriginalPath,
		UploaderGithubID: int8FromPtr(s.UploaderGitHubID),
	})
	return err
}

func (d *db) GetByID(ctx context.Context, id string) (*model.Sheet, error) {
	row, err := d.q.GetSheetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound{ID: id}
	}
	if err != nil {
		return nil, err
	}
	return toModel(row), nil
}

func (d *db) List(ctx context.Context, f ListFilter) ([]*model.Sheet, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.q.ListSheets(ctx, dbgen.ListSheetsParams{
		Limit:  int32(limit),
		Offset: int32(f.Offset),
		Status: nullStatus(f.Status),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*model.Sheet, len(rows))
	for i, r := range rows {
		out[i] = toModel(r)
	}
	return out, nil
}

func (d *db) Update(ctx context.Context, id string, fn func(*model.Sheet) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := d.q.WithTx(tx)
	row, err := qtx.GetSheetByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound{ID: id}
	}
	if err != nil {
		return err
	}

	s := toModel(row)
	if err := fn(s); err != nil {
		return err
	}

	_, err = qtx.UpdateSheet(ctx, dbgen.UpdateSheetParams{
		ID:          s.ID,
		Title:       s.Title,
		Institution: pgtype.Text{String: s.Institution, Valid: s.Institution != ""},
		SourceUrl:   pgtype.Text{String: s.SourceURL, Valid: s.SourceURL != ""},
		RightsNote:  pgtype.Text{String: s.RightsNote, Valid: s.RightsNote != ""},
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *db) SoftDelete(ctx context.Context, id string) error {
	_, err := d.q.SoftDeleteSheet(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound{ID: id}
	}
	return err
}

func (d *db) ClaimNext(ctx context.Context) (*model.Sheet, error) {
	row, err := d.q.ClaimNext(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toModel(row), nil
}

func toModel(r dbgen.Sheet) *model.Sheet {
	var deletedAt *time.Time
	if r.DeletedAt.Valid {
		t := r.DeletedAt.Time
		deletedAt = &t
	}
	var uploaderID *int64
	if r.UploaderGithubID.Valid {
		id := r.UploaderGithubID.Int64
		uploaderID = &id
	}
	return &model.Sheet{
		ID:               r.ID,
		Title:            r.Title,
		Institution:      r.Institution.String,
		SourceURL:        r.SourceUrl.String,
		RightsNote:       r.RightsNote.String,
		Status:           model.Status(r.Status),
		ErrorMessage:     r.ErrorMessage.String,
		OriginalChecksum: r.OriginalChecksum,
		OriginalSize:     r.OriginalSize,
		OriginalPath:     r.OriginalPath,
		PyramidPath:      r.PyramidPath.String,
		UploaderGitHubID: uploaderID,
		CreatedAt:        r.CreatedAt.Time,
		UpdatedAt:        r.UpdatedAt.Time,
		DeletedAt:        deletedAt,
	}
}

func nullStatus(s *model.Status) dbgen.NullSheetStatus {
	if s == nil {
		return dbgen.NullSheetStatus{}
	}
	return dbgen.NullSheetStatus{SheetStatus: dbgen.SheetStatus(*s), Valid: true}
}

func int8FromPtr(id *int64) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *id, Valid: true}
}
