package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDisappearingMessagesExpire(t *testing.T) {
	dir := t.TempDir()
	h, err := New(filepath.Join(dir, "history.json"), 100)
	if err != nil {
		t.Fatal(err)
	}
	h.SetTTL(50 * time.Millisecond)
	if err := h.Add(ChatMessage{From: "alice", Body: "secret", Timestamp: time.Now(), Direction: "sent"}); err != nil {
		t.Fatal(err)
	}
	if len(h.GetAll()) != 1 {
		t.Fatal("message should exist before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if removed := h.PruneExpired(); removed != 1 {
		t.Fatalf("expected 1 expired message, got %d", removed)
	}
	if len(h.GetAll()) != 0 {
		t.Fatal("expired message should be gone")
	}
}

func TestTTLDisabledKeepsMessages(t *testing.T) {
	dir := t.TempDir()
	h, _ := New(filepath.Join(dir, "history.json"), 100)
	h.SetTTL(0)
	_ = h.Add(ChatMessage{From: "bob", Body: "keep me", Timestamp: time.Now(), Direction: "received"})
	time.Sleep(10 * time.Millisecond)
	if removed := h.PruneExpired(); removed != 0 {
		t.Fatalf("nothing should expire, removed %d", removed)
	}
}
