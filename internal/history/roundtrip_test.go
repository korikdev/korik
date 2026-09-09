package history

import (
	"path/filepath"
	"testing"
	"time"

	"korik/internal/pretty"
)

func TestHistory_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	h, err := New(path, 100)
	if err != nil {
		t.Fatalf("failed to create history: %v", err)
	}

	msg := ChatMessage{
		ID:        "msg-123",
		From:      "Alice",
		JID:       "korik:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Body:      "Hello world!",
		Timestamp: time.Now().Truncate(time.Second),
		Direction: "sent",
	}

	if err := h.Add(msg); err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	h2, err := New(path, 100)
	if err != nil {
		t.Fatalf("failed to reload history: %v", err)
	}

	msgs := h2.GetAll()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after reload, got %d", len(msgs))
	}
	if msgs[0].Body != msg.Body {
		t.Fatalf("message body mismatch: got %q, want %q", msgs[0].Body, msg.Body)
	}

	// Pretty roundtrip check
	data, err := pretty.LoadAndPretty(path)
	if err != nil {
		t.Fatalf("failed to load pretty history: %v", err)
	}
	if _, err := pretty.RoundTrip(data); err != nil {
		t.Fatalf("history pretty roundtrip integrity check failed: %v", err)
	}
}
