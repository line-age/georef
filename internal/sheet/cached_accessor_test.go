package sheet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/line-age/georef/internal/model"
	"github.com/line-age/georef/internal/sheet"
)

// stubAccessor is a minimal Accessor that counts GetByID calls and can inject errors.
type stubAccessor struct {
	sheet             *model.Sheet
	err               error
	getByIDCallCount  int
}

func (stub *stubAccessor) GetByID(_ context.Context, _ string) (*model.Sheet, error) {
	stub.getByIDCallCount++
	return stub.sheet, stub.err
}

func (stub *stubAccessor) Create(_ context.Context, _ *model.Sheet) error               { return nil }
func (stub *stubAccessor) List(_ context.Context, _ sheet.ListFilter) ([]*model.Sheet, error) {
	return nil, nil
}
func (stub *stubAccessor) Update(_ context.Context, _ string, _ func(*model.Sheet) error) (*model.Sheet, error) {
	return nil, nil
}
func (stub *stubAccessor) SoftDelete(_ context.Context, _ string) error { return nil }
func (stub *stubAccessor) ProcessNext(_ context.Context, _ int, _ func(context.Context, *model.Sheet) (string, error)) (bool, error) {
	return false, nil
}

func TestCachedAccessor_GetByID_CachesResult(t *testing.T) {
	inner := &stubAccessor{sheet: &model.Sheet{ID: "sheet-1", Status: model.StatusReady}}
	cached := sheet.NewCachedAccessor(inner, time.Second)

	for range 5 {
		got, err := cached.GetByID(context.Background(), "sheet-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "sheet-1" {
			t.Fatalf("got ID %q, want %q", got.ID, "sheet-1")
		}
	}

	if inner.getByIDCallCount != 1 {
		t.Errorf("inner called %d times, want 1 (should be cached)", inner.getByIDCallCount)
	}
}

func TestCachedAccessor_GetByID_RefetchesAfterTTL(t *testing.T) {
	inner := &stubAccessor{sheet: &model.Sheet{ID: "sheet-2", Status: model.StatusReady}}
	cached := sheet.NewCachedAccessor(inner, 50*time.Millisecond)

	if _, err := cached.GetByID(context.Background(), "sheet-2"); err != nil {
		t.Fatalf("first call: %v", err)
	}

	time.Sleep(80 * time.Millisecond) // let the TTL expire

	if _, err := cached.GetByID(context.Background(), "sheet-2"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if inner.getByIDCallCount != 2 {
		t.Errorf("inner called %d times, want 2 (one before expiry, one after)", inner.getByIDCallCount)
	}
}

func TestCachedAccessor_GetByID_DoesNotCacheErrors(t *testing.T) {
	sentinel := errors.New("db error")
	inner := &stubAccessor{err: sentinel}
	cached := sheet.NewCachedAccessor(inner, time.Second)

	for range 3 {
		_, err := cached.GetByID(context.Background(), "sheet-3")
		if !errors.Is(err, sentinel) {
			t.Fatalf("want sentinel error, got %v", err)
		}
	}

	if inner.getByIDCallCount != 3 {
		t.Errorf("inner called %d times, want 3 (errors must not be cached)", inner.getByIDCallCount)
	}
}

func TestCachedAccessor_GetByID_ZeroTTLPassesThrough(t *testing.T) {
	inner := &stubAccessor{sheet: &model.Sheet{ID: "sheet-4", Status: model.StatusReady}}
	cached := sheet.NewCachedAccessor(inner, 0)

	for range 3 {
		if _, err := cached.GetByID(context.Background(), "sheet-4"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if inner.getByIDCallCount != 3 {
		t.Errorf("inner called %d times, want 3 (zero TTL means no caching)", inner.getByIDCallCount)
	}
}
