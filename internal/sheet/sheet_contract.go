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
	Create(ctx context.Context, sheet *model.Sheet) error

	// GetByID returns the sheet with the given ID, including deleted ones.
	// Returns ErrNotFound when the ID does not exist.
	GetByID(ctx context.Context, id string) (*model.Sheet, error)

	// List returns sheets in reverse-chronological order, applying the filter.
	List(ctx context.Context, filter ListFilter) ([]*model.Sheet, error)

	// Update applies mutate to the sheet and persists the result in a single transaction.
	// mutate should modify only the descriptive metadata fields (title, institution, etc.).
	Update(ctx context.Context, id string, mutate func(*model.Sheet) error) error

	// SoftDelete sets status = 'deleted'. The row and its ID are never removed.
	// Subsequent IIIF requests for the ID must return 410.
	SoftDelete(ctx context.Context, id string) error

	// ProcessNext atomically claims one processable sheet (status processing or failed),
	// calls processor within the same transaction so the row lock is held during conversion,
	// then marks the sheet ready or failed based on processor's return value and commits.
	// Returns false when the queue is empty. If ctx is cancelled mid-conversion the
	// transaction rolls back and the sheet remains claimable.
	ProcessNext(ctx context.Context, processor func(ctx context.Context, sheet *model.Sheet) (pyramidPath string, err error)) (bool, error)
}

// ErrNotFound is returned by GetByID when the ID does not exist.
type ErrNotFound struct{ ID string }

func (e ErrNotFound) Error() string { return "sheet not found: " + e.ID }
