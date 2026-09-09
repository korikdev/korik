package circuit

import (
	"fmt"
	"sync"
	"time"
)

// Breaker skips peers that repeatedly fail handshakes instead of
// retrying them forever. After threshold consecutive failures the
// address is skipped until cooldown elapses; any success resets it.
type Breaker struct {
	mu        sync.Mutex
	failures  map[string]int
	blockedAt map[string]time.Time
	threshold int
	cooldown  time.Duration
}

// New creates a breaker with threshold failures and cooldown skip window.
func New(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	return &Breaker{
		failures:  make(map[string]int),
		blockedAt: make(map[string]time.Time),
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// Skip reports whether addr must currently be skipped.
func (b *Breaker) Skip(addr string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	since, blocked := b.blockedAt[addr]
	if !blocked {
		return false
	}
	if time.Since(since) > b.cooldown {
		delete(b.blockedAt, addr)
		delete(b.failures, addr)
		return false
	}
	return true
}

// RecordSuccess clears failure state for addr.
func (b *Breaker) RecordSuccess(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.failures, addr)
	delete(b.blockedAt, addr)
}

// RecordFailure counts a failure; at threshold the addr is opened.
func (b *Breaker) RecordFailure(addr string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures[addr]++
	if b.failures[addr] >= b.threshold {
		b.blockedAt[addr] = time.Now()
		return fmt.Errorf("circuit open for %s after %d failures, cooling down %s", addr, b.failures[addr], b.cooldown)
	}
	return nil
}

// Failures returns the current consecutive failure count (observability).
func (b *Breaker) Failures(addr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures[addr]
}
