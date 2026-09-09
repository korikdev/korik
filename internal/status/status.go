package status

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Phase represents the current connection lifecycle stage shown to users.
type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseDiscovering Phase = "discovering"
	PhaseConnecting  Phase = "connecting"
	PhaseTraversing  Phase = "nat-traversal"
	PhaseHandshaking Phase = "handshaking"
	PhaseConnected   Phase = "connected"
	PhaseRetrying    Phase = "retrying"
	PhaseFailed      Phase = "failed"
)

// Tracker holds the current phase plus a human-readable detail line.
type Tracker struct {
	mu       sync.RWMutex
	phase    Phase
	detail   string
	updated  time.Time
	OnChange func(Phase, string)
}

// NewTracker creates a tracker starting at idle.
func NewTracker() *Tracker {
	return &Tracker{phase: PhaseIdle, updated: time.Now()}
}

// Set updates the phase with a human-readable detail.
func (t *Tracker) Set(phase Phase, detail string) {
	t.mu.Lock()
	t.phase = phase
	t.detail = detail
	t.updated = time.Now()
	cb := t.OnChange
	t.mu.Unlock()
	if cb != nil {
		cb(phase, detail)
	}
}

// Snapshot returns the current phase and detail.
func (t *Tracker) Snapshot() (Phase, string, time.Duration) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.phase, t.detail, time.Since(t.updated)
}

// String renders a one-line status for the CLI prompt area.
func (t *Tracker) String() string {
	phase, detail, age := t.Snapshot()
	if detail == "" {
		return fmt.Sprintf("[%s]", phase)
	}
	return fmt.Sprintf("[%s] %s (%ds)", phase, detail, int(age.Seconds()))
}

// HumanizeError converts low-level Go/network errors into actionable hints.
// It never exposes stack traces, paths, or internal addresses.
func HumanizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "connection refused"):
		return "Connection refused. Is the peer online and is the port correct? Try /peers to check."
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "name resolution"),
		strings.Contains(msg, "dns"):
		return "Cannot resolve address. Check the IP/hostname and try again."
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return "Connection timed out. Peer may be offline, behind strict NAT, or firewall-blocked."
	case strings.Contains(msg, "network is unreachable"):
		return "Network unreachable. Check WiFi connection or switch LAN/Internet mode."
	case strings.Contains(msg, "stun"),
		strings.Contains(msg, "no stun response"):
		return "STUN failed (strict firewall or symmetric NAT). Try LAN mode or a different network."
	case strings.Contains(msg, "eof"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "reset by peer"):
		return "Peer disconnected unexpectedly. Will retry automatically if auto-reconnect is on."
	case strings.Contains(msg, "file not found"),
		strings.Contains(msg, "no such file"):
		return "File not found. Check the path and try /send again."
	case strings.Contains(msg, "permission"):
		return "Permission denied. Check file permissions for ~/.korik and the target file."
	case strings.Contains(msg, "e2e") || strings.Contains(msg, "handshake"):
		return "Encryption handshake not ready yet. Wait a moment and retry."
	default:
		return "Operation failed. Check connection with /peers, then retry. Use /status for details."
	}
}

// RetryPlan computes exponential backoff delays for attempts starting at 1.
// Base 1s, doubled each attempt, capped at 30s.
func RetryPlan(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt >= 6 {
		return 30 * time.Second
	}
	return time.Second << (attempt - 1)
}

// ProgressLine renders a retry progress indicator line.
func ProgressLine(attempt, maxAttempts int, target string) string {
	delay := RetryPlan(attempt)
	if maxAttempts <= 0 {
		return fmt.Sprintf("Retrying %s (attempt %d, next in %ds)...", target, attempt, int(delay.Seconds()))
	}
	return fmt.Sprintf("Retrying %s (attempt %d/%d, next in %ds)...", target, attempt, maxAttempts, int(delay.Seconds()))
}
