package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OutgoingMessage is a chat message queued while the peer is offline.
type OutgoingMessage struct {
	ID        string    `json:"id"`
	ToJID     string    `json:"to_jid"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Attempts  int       `json:"attempts"`
}

// Queue persists unsent messages and flushes them when peers come online.
type Queue struct {
	path  string
	mu    sync.Mutex
	items []OutgoingMessage
}

// New creates or loads a queue from disk.
func New(path string) (*Queue, error) {
	q := &Queue{path: path}
	if data, err := os.ReadFile(path); err == nil {
		var stored struct {
			Messages []OutgoingMessage `json:"messages"`
		}
		if err := json.Unmarshal(data, &stored); err == nil {
			q.items = stored.Messages
		}
	}
	if q.items == nil {
		q.items = make([]OutgoingMessage, 0)
	}
	return q, nil
}

// MaxMessages caps the queue to 1000 pending messages per spec (Req 7.6);
// when the limit is reached, the oldest messages are discarded first.
const MaxMessages = 1000

// Enqueue adds a message for later delivery.
func (q *Queue) Enqueue(msg OutgoingMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if msg.ID == "" {
		msg.ID = msg.ToJID + ":" + msg.CreatedAt.Format(time.RFC3339Nano)
	}
	q.items = append(q.items, msg)
	if len(q.items) > MaxMessages {
		q.items = append([]OutgoingMessage(nil), q.items[len(q.items)-MaxMessages:]...)
	}
	return q.saveLocked()
}

// Pending returns a copy of all queued messages.
func (q *Queue) Pending() []OutgoingMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]OutgoingMessage, len(q.items))
	copy(out, q.items)
	return out
}

// Remove deletes a message after successful delivery.
func (q *Queue) Remove(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	for _, m := range q.items {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	q.items = kept
	return q.saveLocked()
}

// MarkAttempt increments the retry counter for observability.
func (q *Queue) MarkAttempt(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.items {
		if q.items[i].ID == id {
			q.items[i].Attempts++
		}
	}
	_ = q.saveLocked()
}

// Len returns the number of queued messages.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *Queue) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Messages []OutgoingMessage `json:"messages"`
	}{Messages: q.items})
	if err != nil {
		return err
	}
	return os.WriteFile(q.path, data, 0o600)
}

// DefaultQueuePath returns the default queue file location.
func DefaultQueuePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "queue.json")
}
