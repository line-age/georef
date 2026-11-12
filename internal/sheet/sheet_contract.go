package sheet

import (
	"context"

	"github.com/line-age/georef/internal/model"
)

// ListFilter controls which sheets List returns.
type ListFilter struct {
	Status *model.Status // nil returns all non-deleted sheets
	Limit  int
	Offset int
}

// Accessor is the data access contract for sheets.
// All methods accept a context for cancellation and tracing.
// Callers depend on this interface only; the concrete implementation is in sheet_db.go.
type Accessor interface {
	// Create inserts a new sheet row. The sheet must have a pre-generated ID (ULID).
	Create(ctx context.Context, s *model.Sheet) error

	// GetByID returns the sheet with the given ID, including deleted ones.
	// Returns ErrNotFound when the ID does not exist.
	GetByID(ctx context.Context, id string) (*model.Sheet, error)

	// List returns sheets in reverse-chronological order, applying the filter.
	List(ctx context.Context, f ListFilter) ([]*model.Sheet, error)

	// Update applies fn to the sheet and persists the result in a single transaction.
	// fn should mutate only the descriptive metadata fields (title, institution, etc.).
	Update(ctx context.Context, id string, fn func(*model.Sheet) error) error

	// SoftDelete sets status = 'deleted'. The row and its ID are never removed.
	// Subsequent IIIF requests for the ID must return 410.
	SoftDelete(ctx context.Context, id string) error

	// ClaimNext atomically claims one processing or failed sheet using SELECT … FOR UPDATE
	// SKIP LOCKED. Returns nil, nil when nothing is claimable.
	ClaimNext(ctx context.Context) (*model.Sheet, error)
}

// ErrNotFound is returned by GetByID when the ID does not exist.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return "sheet not found: " + e.ID }
