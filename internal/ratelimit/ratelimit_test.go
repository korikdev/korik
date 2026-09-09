package ratelimit

import (
	"testing"
)

func TestBurstThenThrottle(t *testing.T) {
	l := New(1000, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("peer") {
			t.Fatal("burst must be allowed")
		}
	}
	if l.Allow("peer") {
		t.Fatal("over-burst must be throttled")
	}
}

func TestIndependentKeys(t *testing.T) {
	l := New(1000, 1)
	if !l.Allow("a") {
		t.Fatal("first event must pass")
	}
	if !l.Allow("b") {
		t.Fatal("different key must have its own bucket")
	}
}

func TestPeerGuard_RateLimit(t *testing.T) {
	g := NewPeerGuard()
	// Drain the burst bucket.
	for i := 0; i < MessageRatePerMinute; i++ {
		g.Allow("peer1")
	}
	// Next call should be throttled.
	if g.Allow("peer1") {
		t.Fatal("expected peer to be rate-limited after exhausting burst")
	}
}

func TestPeerGuard_AutoBlock(t *testing.T) {
	g := NewPeerGuard()
	// Exhaust the entire burst to get the first strike.
	for i := 0; i < MessageRatePerMinute; i++ {
		g.Allow("peer1")
	}
	// Trigger MaxStrikesBeforeBlock violations.
	for i := 0; i < MaxStrikesBeforeBlock; i++ {
		g.Allow("peer1") // each call over limit adds a strike
	}
	if !g.IsBlocked("peer1") {
		t.Fatal("expected peer to be auto-blocked after max strikes")
	}
}

func TestPeerGuard_StrikeCount(t *testing.T) {
	g := NewPeerGuard()
	// Exhaust burst then trigger one violation.
	for i := 0; i < MessageRatePerMinute; i++ {
		g.Allow("peer2")
	}
	g.Allow("peer2") // first strike
	if g.Strikes("peer2") != 1 {
		t.Fatalf("expected 1 strike, got %d", g.Strikes("peer2"))
	}
}

func TestPeerGuard_Unblock(t *testing.T) {
	g := NewPeerGuard()
	// Force a block.
	for i := 0; i < MessageRatePerMinute; i++ {
		g.Allow("peer3")
	}
	for i := 0; i < MaxStrikesBeforeBlock; i++ {
		g.Allow("peer3")
	}
	if !g.IsBlocked("peer3") {
		t.Fatal("expected peer to be blocked")
	}
	g.Unblock("peer3")
	if g.IsBlocked("peer3") {
		t.Fatal("expected peer to be unblocked after Unblock()")
	}
	// Should be able to send again after unblock.
	if !g.Allow("peer3") {
		t.Fatal("expected first message to be allowed after unblock")
	}
}

func TestConnectionGuard_Allow(t *testing.T) {
	g := NewConnectionGuard()
	// First connection should be allowed.
	if !g.Allow("192.168.1.1") {
		t.Fatal("expected first connection to be allowed")
	}
}

func TestConnectionGuard_RateLimit(t *testing.T) {
	g := NewConnectionGuard()
	// Drain the burst.
	for i := 0; i < ConnectionRatePerMinute; i++ {
		g.Allow("10.0.0.1")
	}
	if g.Allow("10.0.0.1") {
		t.Fatal("expected connection to be throttled after limit")
	}
}

func TestConnectionGuard_IndependentIPs(t *testing.T) {
	g := NewConnectionGuard()
	if !g.Allow("1.1.1.1") {
		t.Fatal("first IP must be allowed")
	}
	if !g.Allow("2.2.2.2") {
		t.Fatal("second IP must have its own bucket")
	}
}

func TestValidateFileSize_Valid(t *testing.T) {
	if err := ValidateFileSize(1024 * 1024); err != nil {
		t.Fatalf("expected no error for 1MB, got %v", err)
	}
}

func TestValidateFileSize_Exceeds2GB(t *testing.T) {
	if err := ValidateFileSize(MaxFileTransferBytes + 1); err == nil {
		t.Fatal("expected error for size exceeding 2GiB")
	}
}

func TestValidateFileSize_Negative(t *testing.T) {
	if err := ValidateFileSize(-1); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestValidateFileSize_ExactLimit(t *testing.T) {
	if err := ValidateFileSize(MaxFileTransferBytes); err != nil {
		t.Fatalf("expected no error at exact limit, got %v", err)
	}
}
