package dedup

import (
	"sync"
	"time"
)

// Seen tracks recently observed message IDs so reconnects, retries,
// and replays never deliver the same logical message twice.
// Bounded by max entries plus TTL expiry; safe for concurrent use.
type Seen struct {
	mu  sync.Mutex
	ids map[string]time.Time
	max int
	ttl time.Duration
}

// New creates a store holding up to max IDs for ttl each.
func New(capacity int, ttl time.Duration) *Seen {
	if capacity <= 0 {
		capacity = 10000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Seen{ids: make(map[string]time.Time, capacity), max: capacity, ttl: ttl}
}

// Add records id. It reports false when id was already seen
// (duplicate — caller must skip processing).
func (s *Seen) Add(id string) bool {
	if id == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if _, exists := s.ids[id]; exists {
		return false
	}
	// Opportunistic expiry + capacity eviction.
	if len(s.ids) >= s.max {
		for key, ts := range s.ids {
			if now.Sub(ts) > s.ttl {
				delete(s.ids, key)
			}
		}
	}
	for len(s.ids) >= s.max {
		// Evict oldest remaining entry.
		var oldest string
		var oldestTime time.Time
		first := true
		for key, ts := range s.ids {
			if first || ts.Before(oldestTime) {
				oldest, oldestTime, first = key, ts, false
			}
		}
		if oldest == "" {
			break
		}
		delete(s.ids, oldest)
	}
	s.ids[id] = now
	return true
}

// Has reports whether id was seen (and prunes nothing).
func (s *Seen) Has(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.ids[id]
	if !ok {
		return false
	}
	if time.Since(ts) > s.ttl {
		delete(s.ids, id)
		return false
	}
	return true
}

// Len returns the number of tracked IDs (for observability).
func (s *Seen) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ids)
}
