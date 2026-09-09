package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"korik/internal/pretty"
)

// ChatMessage represents a single chat message in history
type ChatMessage struct {
	ID           string    `json:"id,omitempty"`
	From         string    `json:"from"`
	JID          string    `json:"jid"`
	LID          string    `json:"lid"`
	SenderDevice string    `json:"sender_device,omitempty"`
	Seq          uint64    `json:"seq,omitempty"`
	GroupID      string    `json:"group_id,omitempty"`
	ReplyTo      string    `json:"reply_to,omitempty"`
	Body         string    `json:"body"`
	Timestamp    time.Time `json:"timestamp"`
	Direction    string    `json:"direction"` // "sent" or "received"
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// History manages persistent message storage.
// For large files the in-memory window can be capped via NewWithTail:
// the full file stays on disk while only the tail is kept in RAM.
// LoadOlder re-reads the file for deep paging (rare path).
type History struct {
	path        string
	maxSize     int
	mu          sync.RWMutex
	messages    []ChatMessage
	ttl         time.Duration
	totalStored int
	tailOnly    bool
}

type storedHistory struct {
	Messages []ChatMessage `json:"messages"`
}

// New creates a new History instance
func New(path string, maxSize int) (*History, error) {
	return NewWithTail(path, maxSize, 0)
}

// NewWithTail loads only the last tailLimit messages into memory when
// tailLimit > 0, keeping startup light for large history files.
func NewWithTail(path string, maxSize, tailLimit int) (*History, error) {
	h := &History{
		path:     path,
		maxSize:  maxSize,
		messages: make([]ChatMessage, 0),
	}
	if err := h.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if tailLimit > 0 && len(h.messages) > tailLimit {
		h.messages = append([]ChatMessage(nil), h.messages[len(h.messages)-tailLimit:]...)
		h.tailOnly = true
	}
	return h, nil
}

// Add adds a message to history
func (h *History) Add(msg ChatMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ttl > 0 && msg.ExpiresAt.IsZero() {
		msg.ExpiresAt = time.Now().Add(h.ttl)
	}
	h.messages = append(h.messages, msg)
	h.totalStored++
	h.pruneExpiredLocked()

	// Trim if exceeds max size
	if h.maxSize > 0 && len(h.messages) > h.maxSize {
		dropped := len(h.messages) - h.maxSize
		h.messages = h.messages[len(h.messages)-h.maxSize:]
		h.totalStored -= dropped
		if h.totalStored < len(h.messages) {
			h.totalStored = len(h.messages)
		}
	}

	return h.save()
}

// SetTTL configures auto-expiry for new messages (0 disables).
func (h *History) SetTTL(ttl time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ttl = ttl
}

// PruneExpired removes disappearing messages whose time has passed.
func (h *History) PruneExpired() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	before := len(h.messages)
	h.pruneExpiredLocked()
	removed := before - len(h.messages)
	if removed > 0 {
		h.totalStored -= removed
		if h.totalStored < len(h.messages) {
			h.totalStored = len(h.messages)
		}
		_ = h.save()
	}
	return removed
}

func (h *History) pruneExpiredLocked() {
	if len(h.messages) == 0 {
		return
	}
	now := time.Now()
	kept := h.messages[:0]
	for _, m := range h.messages {
		if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) {
			continue
		}
		kept = append(kept, m)
	}
	h.messages = kept
}

// GetAll returns all messages in history
func (h *History) GetAll() []ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]ChatMessage, len(h.messages))
	copy(result, h.messages)
	return result
}

// GetLast returns the last N messages
func (h *History) GetLast(n int) []ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n <= 0 || len(h.messages) == 0 {
		return []ChatMessage{}
	}

	start := 0
	if len(h.messages) > n {
		start = len(h.messages) - n
	}

	result := make([]ChatMessage, len(h.messages)-start)
	copy(result, h.messages[start:])
	return result
}

// GetSince returns messages newer than since (delta sync source).
func (h *History) GetSince(since time.Time) []ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ChatMessage, 0)
	for _, m := range h.messages {
		if m.Timestamp.After(since) {
			out = append(out, m)
		}
	}
	return out
}

// GetPage returns a page of the in-memory window (page 0 = oldest).
func (h *History) GetPage(page, pageSize int) []ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if pageSize <= 0 || page < 0 || len(h.messages) == 0 {
		return []ChatMessage{}
	}
	start := page * pageSize
	if start >= len(h.messages) {
		return []ChatMessage{}
	}
	end := start + pageSize
	if end > len(h.messages) {
		end = len(h.messages)
	}
	out := make([]ChatMessage, end-start)
	copy(out, h.messages[start:end])
	return out
}

// FindByIDPrefix resolves a message by full ID or unique short prefix.
func (h *History) FindByIDPrefix(prefix string) (ChatMessage, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var match *ChatMessage
	for i := range h.messages {
		if h.messages[i].ID == prefix {
			return h.messages[i], true
		}
		if prefix != "" && len(h.messages[i].ID) >= len(prefix) && h.messages[i].ID[:len(prefix)] == prefix {
			if match != nil {
				return ChatMessage{}, false // ambiguous
			}
			c := h.messages[i]
			match = &c
		}
	}
	if match == nil {
		return ChatMessage{}, false
	}
	return *match, true
}

// TotalStored reports messages counted on disk at load time.
func (h *History) TotalStored() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.tailOnly {
		return h.totalStored
	}
	return len(h.messages)
}

// LoadOlder re-reads the file and returns a window before the in-memory
// tail (for deep paging without keeping everything in RAM).
func (h *History) LoadOlder(offset, limit int) []ChatMessage {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return []ChatMessage{}
	}
	var stored storedHistory
	if err := json.Unmarshal(data, &stored); err != nil {
		return []ChatMessage{}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(stored.Messages) || limit <= 0 {
		return []ChatMessage{}
	}
	end := offset + limit
	if end > len(stored.Messages) {
		end = len(stored.Messages)
	}
	out := make([]ChatMessage, end-offset)
	copy(out, stored.Messages[offset:end])
	return out
}

// Clear removes all messages from history
func (h *History) Clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.messages = make([]ChatMessage, 0)
	h.totalStored = 0
	return h.save()
}

func (h *History) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}

	var stored storedHistory
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	h.messages = stored.Messages
	h.totalStored = len(stored.Messages)
	h.pruneExpiredLocked()
	return nil
}

func (h *History) save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}

	stored := storedHistory{Messages: h.messages}
	return pretty.SavePretty(h.path, stored)
}

// DefaultHistoryPath returns the default path for history file
func DefaultHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "history.json")
}
