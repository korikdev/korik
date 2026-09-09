// Package ratelimit provides per-key token buckets and abuse prevention for
// the Korik P2P chat application. It implements:
//   - Per-peer message rate limiting (100 messages/minute)
//   - Connection rate limiting (10 attempts/minute per IP)
//   - 3-strike auto-block for persistent violators
//   - File transfer size enforcement (2 GiB maximum)
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

const (
	// MessageRatePerMinute is the maximum number of chat messages a single peer
	// may send per minute before excess messages are dropped (Req 18.1).
	MessageRatePerMinute = 100

	// ConnectionRatePerMinute is the maximum number of connection attempts
	// allowed from a single IP address per minute (Req 18.4).
	ConnectionRatePerMinute = 10

	// MaxStrikesBeforeBlock is the number of rate-limit violations a peer
	// triggers before being automatically blocked (Req 18.3).
	MaxStrikesBeforeBlock = 3

	// MaxFileTransferBytes is the maximum allowed file transfer size in bytes.
	// Offers exceeding this limit are rejected with an error (Req 18.6).
	MaxFileTransferBytes int64 = 2 * 1024 * 1024 * 1024 // 2 GiB
)

// Limiter is a per-key token bucket that drops flooding peers before
// their traffic can bloat the queue, history, or CPU.
// Defaults fit interactive chat: 20 events/sec sustained, burst 40.
type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	states map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New creates a limiter allowing rate tokens/sec with burst capacity.
func New(ratePerSec, burst float64) *Limiter {
	if ratePerSec <= 0 {
		ratePerSec = 20
	}
	if burst <= 0 {
		burst = 40
	}
	return &Limiter{rate: ratePerSec, burst: burst, states: make(map[string]*bucket)}
}

// Allow reports whether one event for key may proceed.
func (l *Limiter) Allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.states[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.states[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Reset clears state for key (used when a peer cleanly reconnects).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.states, key)
}

// ---------------------------------------------------------------------------
// PeerGuard: per-peer message rate + auto-block with strike counting.
// ---------------------------------------------------------------------------

// PeerGuard enforces per-peer message rate limits and maintains a strike
// counter. After MaxStrikesBeforeBlock violations the peer is auto-blocked.
type PeerGuard struct {
	mu      sync.Mutex
	peers   map[string]*peerState
}

type peerState struct {
	// Token bucket for message rate (100 msg/min = ~1.67/sec).
	tokens    float64
	last      time.Time
	strikes   int
	blocked   bool
}

// NewPeerGuard creates a guard with the spec-mandated limits.
func NewPeerGuard() *PeerGuard {
	return &PeerGuard{peers: make(map[string]*peerState)}
}

// Allow returns true if a message from peerJID is within rate limits.
// It increments the strike counter when the limit is exceeded and auto-blocks
// the peer after MaxStrikesBeforeBlock violations.
func (g *PeerGuard) Allow(peerJID string) bool {
	if peerJID == "" {
		peerJID = "unknown"
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	ps, ok := g.peers[peerJID]
	if !ok {
		// Start with a full burst equal to MessageRatePerMinute.
		ps = &peerState{tokens: float64(MessageRatePerMinute), last: time.Now()}
		g.peers[peerJID] = ps
	}

	if ps.blocked {
		return false
	}

	now := time.Now()
	elapsed := now.Sub(ps.last).Seconds()
	ratePerSec := float64(MessageRatePerMinute) / 60.0
	ps.tokens += elapsed * ratePerSec
	if ps.tokens > float64(MessageRatePerMinute) {
		ps.tokens = float64(MessageRatePerMinute)
	}
	ps.last = now

	if ps.tokens < 1 {
		// Rate limit exceeded: record a strike and potentially auto-block.
		ps.strikes++
		if ps.strikes >= MaxStrikesBeforeBlock {
			ps.blocked = true
		}
		return false
	}
	ps.tokens--
	return true
}

// IsBlocked returns true if the peer has been auto-blocked.
func (g *PeerGuard) IsBlocked(peerJID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	ps, ok := g.peers[peerJID]
	return ok && ps.blocked
}

// Strikes returns the current violation count for peerJID.
func (g *PeerGuard) Strikes(peerJID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ps, ok := g.peers[peerJID]; ok {
		return ps.strikes
	}
	return 0
}

// Unblock resets the blocked state and strike count for peerJID.
func (g *PeerGuard) Unblock(peerJID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.peers, peerJID)
}

// ---------------------------------------------------------------------------
// ConnectionGuard: per-IP connection rate limiting.
// ---------------------------------------------------------------------------

// ConnectionGuard enforces connection rate limits per source IP address.
// It allows up to ConnectionRatePerMinute new connections per minute.
type ConnectionGuard struct {
	mu     sync.Mutex
	states map[string]*connState
}

type connState struct {
	tokens float64
	last   time.Time
}

// NewConnectionGuard creates a connection rate limiter.
func NewConnectionGuard() *ConnectionGuard {
	return &ConnectionGuard{states: make(map[string]*connState)}
}

// Allow returns true if a new connection from ip is within rate limits.
func (g *ConnectionGuard) Allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	cs, ok := g.states[ip]
	if !ok {
		cs = &connState{tokens: float64(ConnectionRatePerMinute), last: time.Now()}
		g.states[ip] = cs
	}

	now := time.Now()
	elapsed := now.Sub(cs.last).Seconds()
	ratePerSec := float64(ConnectionRatePerMinute) / 60.0
	cs.tokens += elapsed * ratePerSec
	if cs.tokens > float64(ConnectionRatePerMinute) {
		cs.tokens = float64(ConnectionRatePerMinute)
	}
	cs.last = now

	if cs.tokens < 1 {
		return false
	}
	cs.tokens--
	return true
}

// ---------------------------------------------------------------------------
// File transfer size validation.
// ---------------------------------------------------------------------------

// ValidateFileSize returns an error if the offered file size exceeds the
// maximum allowed 2 GiB limit (Req 18.6).
func ValidateFileSize(sizeBytes int64) error {
	if sizeBytes < 0 {
		return fmt.Errorf("invalid file size: %d bytes", sizeBytes)
	}
	if sizeBytes > MaxFileTransferBytes {
		return fmt.Errorf("file size %d exceeds the maximum allowed size of 2 GiB (%d bytes)",
			sizeBytes, MaxFileTransferBytes)
	}
	return nil
}
