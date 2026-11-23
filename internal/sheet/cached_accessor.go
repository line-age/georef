package sheet

import (
	"context"
	"sync"
	"time"

	"github.com/line-age/georef/internal/model"
)

// CachedAccessor wraps an Accessor and caches GetByID results for a short TTL.
// Only GetByID is cached; all other methods pass through directly so mutations
// are always applied to the real store.
//
// Use CachedAccessor on IIIF read paths where the same sheet ID is looked up
// on every tile request. Sheet API handlers should use the unwrapped Accessor
// so they always see current data.
type CachedAccessor struct {
	inner Accessor
	mu    sync.RWMutex
	cache map[string]cachedEntry
	ttl   time.Duration
}

type cachedEntry struct {
	sheet     *model.Sheet
	expiresAt time.Time
}

// NewCachedAccessor returns a CachedAccessor that caches GetByID results for ttl.
// When ttl is zero, GetByID always passes through to inner with no caching.
func NewCachedAccessor(inner Accessor, ttl time.Duration) *CachedAccessor {
	return &CachedAccessor{
		inner: inner,
		cache: make(map[string]cachedEntry),
		ttl:   ttl,
	}
}

// GetByID returns the cached sheet if the entry is still within TTL.
// On a cache miss or expired entry it fetches from inner and caches the result.
// Errors from inner are not cached.
func (cached *CachedAccessor) GetByID(ctx context.Context, id string) (*model.Sheet, error) {
	if cached.ttl == 0 {
		return cached.inner.GetByID(ctx, id)
	}

	cached.mu.RLock()
	entry, ok := cached.cache[id]
	cached.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.sheet, nil
	}

	sheetRecord, err := cached.inner.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	cached.mu.Lock()
	// Double-check: another goroutine may have populated the cache between our
	// RUnlock and this Lock. Return its entry rather than overwriting it.
	if existing, ok := cached.cache[id]; ok && time.Now().Before(existing.expiresAt) {
		cached.mu.Unlock()
		return existing.sheet, nil
	}
	cached.cache[id] = cachedEntry{sheet: sheetRecord, expiresAt: time.Now().Add(cached.ttl)}
	cached.evictExpiredLocked()
	cached.mu.Unlock()

	return sheetRecord, nil
}

// evictExpiredLocked removes entries whose TTL has elapsed.
// Must be called with cached.mu held for writing.
func (cached *CachedAccessor) evictExpiredLocked() {
	now := time.Now()
	for id, entry := range cached.cache {
		if now.After(entry.expiresAt) {
			delete(cached.cache, id)
		}
	}
}

// The methods below pass directly through to inner. They are listed
// explicitly so CachedAccessor satisfies the Accessor interface.

func (cached *CachedAccessor) Create(ctx context.Context, sheet *model.Sheet) error {
	return cached.inner.Create(ctx, sheet)
}

func (cached *CachedAccessor) List(ctx context.Context, filter ListFilter) ([]*model.Sheet, error) {
	return cached.inner.List(ctx, filter)
}

func (cached *CachedAccessor) Update(ctx context.Context, id string, mutate func(*model.Sheet) error) (*model.Sheet, error) {
	return cached.inner.Update(ctx, id, mutate)
}

func (cached *CachedAccessor) SoftDelete(ctx context.Context, id string) error {
	if err := cached.inner.SoftDelete(ctx, id); err != nil {
		return err
	}
	cached.mu.Lock()
	delete(cached.cache, id)
	cached.mu.Unlock()
	return nil
}

func (cached *CachedAccessor) ProcessNext(ctx context.Context, maxRetries int, processor func(ctx context.Context, sheet *model.Sheet) (string, error)) (bool, error) {
	return cached.inner.ProcessNext(ctx, maxRetries, processor)
}
