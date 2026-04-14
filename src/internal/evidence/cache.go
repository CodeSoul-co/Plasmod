package evidence

import (
	"sync"

	"plasmod/src/internal/schemas"
)

// Cache stores pre-computed EvidenceFragments keyed by object ID.
// It is the hot-path lookup used by the Assembler to avoid re-deriving proof
// chains from raw events on every query.
type Cache struct {
	mu        sync.RWMutex
	fragments map[string]EvidenceFragment
	maxSize   int
	// lruOrder tracks insertion order for eviction (simple FIFO approximation).
	lruOrder []string
}

// NewCache creates an EvidenceCache with the given capacity.
// A maxSize of 0 means unbounded; falls back to schemas.DefaultEvidenceCacheSize.
func NewCache(maxSize int) *Cache {
	if maxSize <= 0 {
		maxSize = schemas.DefaultEvidenceCacheSize
	}
	return &Cache{
		fragments: make(map[string]EvidenceFragment, maxSize),
		maxSize:   maxSize,
		lruOrder:  make([]string, 0, maxSize),
	}
}

// Put stores a pre-computed fragment, evicting the oldest entry when at capacity.
func (c *Cache) Put(f EvidenceFragment) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.fragments[f.ObjectID]; !exists {
		if len(c.lruOrder) >= c.maxSize {
			oldest := c.lruOrder[0]
			c.lruOrder = c.lruOrder[1:]
			delete(c.fragments, oldest)
		}
		c.lruOrder = append(c.lruOrder, f.ObjectID)
	}
	c.fragments[f.ObjectID] = f
}

// Get retrieves a pre-computed fragment by object ID.
func (c *Cache) Get(objectID string) (EvidenceFragment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.fragments[objectID]
	return f, ok
}

// GetMany retrieves fragments for a batch of object IDs.
// The returned slice has the same length as objectIDs; missing entries are
// zero-value fragments with ObjectID == "".
func (c *Cache) GetMany(objectIDs []string) []EvidenceFragment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]EvidenceFragment, len(objectIDs))
	for i, id := range objectIDs {
		out[i] = c.fragments[id]
	}
	return out
}

// Invalidate removes a specific fragment from the cache (e.g. after a
// policy change that affects the pre-computed chain).
func (c *Cache) Invalidate(objectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.fragments, objectID)
	for i, id := range c.lruOrder {
		if id == objectID {
			c.lruOrder = append(c.lruOrder[:i], c.lruOrder[i+1:]...)
			break
		}
	}
}

// Clear removes all cached fragments (admin full data wipe).
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fragments = make(map[string]EvidenceFragment)
	c.lruOrder = c.lruOrder[:0]
}

// Len returns the current number of cached fragments.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.fragments)
}
