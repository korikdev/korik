package queue

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEnqueuePendingRemove(t *testing.T) {
	dir := t.TempDir()
	q, err := New(filepath.Join(dir, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(OutgoingMessage{ID: "m1", ToJID: "korik:abc", Body: "hello", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatalf("expected 1 queued, got %d", q.Len())
	}
	pending := q.Pending()
	if len(pending) != 1 || pending[0].Body != "hello" {
		t.Fatal("pending message mismatch")
	}
	if err := q.Remove("m1"); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 0 {
		t.Fatal("queue should be empty after remove")
	}
}

func TestQueuePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	q, _ := New(path)
	_ = q.Enqueue(OutgoingMessage{ID: "m2", ToJID: "korik:xyz", Body: "retry me", CreatedAt: time.Now()})
	reloaded, _ := New(path)
	if reloaded.Len() != 1 {
		t.Fatalf("expected persisted message, got %d", reloaded.Len())
	}
}
