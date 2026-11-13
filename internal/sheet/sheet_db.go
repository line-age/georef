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

// repository implements Accessor against a Postgres connection pool.
type repository struct {
	queries *dbgen.Queries
	pool    *pgxpool.Pool
}

// NewRepository returns an Accessor backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) Accessor {
	return &repository{
		queries: dbgen.New(pool),
		pool:    pool,
	}
}

func (repository *repository) Create(ctx context.Context, sheet *model.Sheet) error {
	_, err := repository.queries.CreateSheet(ctx, dbgen.CreateSheetParams{
		ID:               sheet.ID,
		Title:            sheet.Title,
		Institution:      pgtype.Text{String: sheet.Institution, Valid: sheet.Institution != ""},
		SourceUrl:        pgtype.Text{String: sheet.SourceURL, Valid: sheet.SourceURL != ""},
		RightsNote:       pgtype.Text{String: sheet.RightsNote, Valid: sheet.RightsNote != ""},
		OriginalChecksum: sheet.OriginalChecksum,
		OriginalSize:     sheet.OriginalSize,
		OriginalPath:     sheet.OriginalPath,
		UploaderGithubID: toPgNullableInt64(sheet.UploaderGitHubID),
	})
	return err
}

func (repository *repository) GetByID(ctx context.Context, id string) (*model.Sheet, error) {
	row, err := repository.queries.GetSheetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound{ID: id}
	}
	if err != nil {
		return nil, err
	}
	return toSheetModel(row), nil
}

func (repository *repository) List(ctx context.Context, filter ListFilter) ([]*model.Sheet, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	dbRows, err := repository.queries.ListSheets(ctx, dbgen.ListSheetsParams{
		Limit:  int32(limit),
		Offset: int32(filter.Offset),
		Status: toNullableStatus(filter.Status),
	})
	if err != nil {
		return nil, err
	}
	sheets := make([]*model.Sheet, len(dbRows))
	for i, dbRow := range dbRows {
		sheets[i] = toSheetModel(dbRow)
	}
	return sheets, nil
}

func (repository *repository) Update(ctx context.Context, id string, mutate func(*model.Sheet) error) error {
	transaction, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx) //nolint:errcheck

	transactionQueries := repository.queries.WithTx(transaction)
	row, err := transactionQueries.GetSheetByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound{ID: id}
	}
	if err != nil {
		return err
	}

	sheet := toSheetModel(row)
	if err := mutate(sheet); err != nil {
		return err
	}

	_, err = transactionQueries.UpdateSheet(ctx, dbgen.UpdateSheetParams{
		ID:          sheet.ID,
		Title:       sheet.Title,
		Institution: pgtype.Text{String: sheet.Institution, Valid: sheet.Institution != ""},
		SourceUrl:   pgtype.Text{String: sheet.SourceURL, Valid: sheet.SourceURL != ""},
		RightsNote:  pgtype.Text{String: sheet.RightsNote, Valid: sheet.RightsNote != ""},
	})
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (repository *repository) SoftDelete(ctx context.Context, id string) error {
	_, err := repository.queries.SoftDeleteSheet(ctx, id)
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// ErrNoRows means either the sheet never existed or it was already deleted.
	// Check which case applies so we can return ErrNotFound only for truly absent IDs.
	// An already-deleted sheet must stay 410, not flip to 404.
	_, getErr := repository.queries.GetSheetByID(ctx, id)
	if getErr == nil {
		return nil // already deleted; idempotent
	}
	return ErrNotFound{ID: id}
}

func (repository *repository) ProcessNext(ctx context.Context, processor func(ctx context.Context, sheet *model.Sheet) (string, error)) (bool, error) {
	transaction, err := repository.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer transaction.Rollback(ctx) //nolint:errcheck

	transactionQueries := repository.queries.WithTx(transaction)

	row, err := transactionQueries.ClaimNext(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	sheet := toSheetModel(row)
	pyramidPath, processorErr := processor(ctx, sheet)

	if processorErr != nil {
		_, err = transactionQueries.SetSheetFailed(ctx, dbgen.SetSheetFailedParams{
			ID:           sheet.ID,
			ErrorMessage: pgtype.Text{String: processorErr.Error(), Valid: true},
		})
	} else {
		_, err = transactionQueries.SetSheetReady(ctx, dbgen.SetSheetReadyParams{
			ID:          sheet.ID,
			PyramidPath: pgtype.Text{String: pyramidPath, Valid: true},
		})
	}
	if err != nil {
		return false, err
	}

	return true, transaction.Commit(ctx)
}

// toSheetModel converts a database row into the domain Sheet model.
func toSheetModel(row dbgen.Sheet) *model.Sheet {
	var deletedAt *time.Time
	if row.DeletedAt.Valid {
		deletedTime := row.DeletedAt.Time
		deletedAt = &deletedTime
	}
	var uploaderID *int64
	if row.UploaderGithubID.Valid {
		githubID := row.UploaderGithubID.Int64
		uploaderID = &githubID
	}
	return &model.Sheet{
		ID:               row.ID,
		Title:            row.Title,
		Institution:      row.Institution.String,
		SourceURL:        row.SourceUrl.String,
		RightsNote:       row.RightsNote.String,
		Status:           model.Status(row.Status),
		ErrorMessage:     row.ErrorMessage.String,
		OriginalChecksum: row.OriginalChecksum,
		OriginalSize:     row.OriginalSize,
		OriginalPath:     row.OriginalPath,
		PyramidPath:      row.PyramidPath.String,
		UploaderGitHubID: uploaderID,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
		DeletedAt:        deletedAt,
	}
}

// toNullableStatus converts an optional model.Status into the pgx nullable type for SQL queries.
func toNullableStatus(status *model.Status) dbgen.NullSheetStatus {
	if status == nil {
		return dbgen.NullSheetStatus{}
	}
	return dbgen.NullSheetStatus{SheetStatus: dbgen.SheetStatus(*status), Valid: true}
}

// toPgNullableInt64 converts an optional int64 pointer into the pgx nullable integer type.
// pgtype.Int8 is pgx's name for a nullable 64-bit integer — it is not Go's int8.
func toPgNullableInt64(id *int64) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *id, Valid: true}
}
