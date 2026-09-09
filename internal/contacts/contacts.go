package contacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"korik/internal/pretty"
)

// Contact represents a saved peer contact
type Contact struct {
	JID          string    `json:"jid"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name,omitempty"` // Custom name/alias
	LastAddress  string    `json:"last_address,omitempty"` // Last known IP:port
	LastSeen     time.Time `json:"last_seen"`
	FirstMet     time.Time `json:"first_met"`
	MessageCount int       `json:"message_count"`
	Favorite     bool      `json:"favorite"`
	Blocked      bool      `json:"blocked"`
	Notes        string    `json:"notes,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
}

// ContactBook manages the contact list
type ContactBook struct {
	path     string
	mu       sync.RWMutex
	contacts map[string]*Contact // key: JID
}

type storedContactBook struct {
	Contacts []*Contact `json:"contacts"`
}

// NewContactBook creates a new contact book
func NewContactBook(path string) (*ContactBook, error) {
	cb := &ContactBook{
		path:     path,
		contacts: make(map[string]*Contact),
	}
	if err := cb.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return cb, nil
}

// Add adds or updates a contact
func (cb *ContactBook) Add(jid, name string) *Contact {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if exists {
		c.Name = name
		c.LastSeen = time.Now()
	} else {
		c = &Contact{
			JID:      jid,
			Name:     name,
			FirstMet: time.Now(),
			LastSeen: time.Now(),
		}
		cb.contacts[jid] = c
	}

	_ = cb.save()
	return c
}

// Get retrieves a contact by JID
func (cb *ContactBook) Get(jid string) (*Contact, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.contacts[jid]
	return c, ok
}

// UpdateLastSeen updates the last seen timestamp
func (cb *ContactBook) UpdateLastSeen(jid, address string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if c, exists := cb.contacts[jid]; exists {
		c.LastSeen = time.Now()
		if address != "" {
			c.LastAddress = address
		}
		_ = cb.save()
	}
}

// IncrementMessageCount increments message count for a contact
func (cb *ContactBook) IncrementMessageCount(jid string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if c, exists := cb.contacts[jid]; exists {
		c.MessageCount++
		_ = cb.save()
	}
}

// SetDisplayName sets a custom display name/alias
func (cb *ContactBook) SetDisplayName(jid, displayName string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if !exists {
		return os.ErrNotExist
	}

	c.DisplayName = displayName
	_ = cb.save()
	return nil
}

// SetFavorite marks a contact as favorite
func (cb *ContactBook) SetFavorite(jid string, favorite bool) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if !exists {
		return os.ErrNotExist
	}

	c.Favorite = favorite
	_ = cb.save()
	return nil
}

// SetBlocked marks a contact as blocked
func (cb *ContactBook) SetBlocked(jid string, blocked bool) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if !exists {
		return os.ErrNotExist
	}

	c.Blocked = blocked
	_ = cb.save()
	return nil
}

// IsBlocked checks if a contact is blocked
func (cb *ContactBook) IsBlocked(jid string) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if c, exists := cb.contacts[jid]; exists {
		return c.Blocked
	}
	return false
}

// AddTag adds a tag to a contact
func (cb *ContactBook) AddTag(jid, tag string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if !exists {
		return os.ErrNotExist
	}

	// Check if tag already exists
	for _, t := range c.Tags {
		if t == tag {
			return nil
		}
	}

	c.Tags = append(c.Tags, tag)
	_ = cb.save()
	return nil
}

// RemoveTag removes a tag from a contact
func (cb *ContactBook) RemoveTag(jid, tag string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.contacts[jid]
	if !exists {
		return os.ErrNotExist
	}

	for i, t := range c.Tags {
		if t == tag {
			c.Tags = append(c.Tags[:i], c.Tags[i+1:]...)
			break
		}
	}

	_ = cb.save()
	return nil
}

// Delete removes a contact
func (cb *ContactBook) Delete(jid string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if _, exists := cb.contacts[jid]; !exists {
		return os.ErrNotExist
	}

	delete(cb.contacts, jid)
	_ = cb.save()
	return nil
}

// Search searches contacts by name, display name, or JID
func (cb *ContactBook) Search(query string) []*Contact {
	return cb.FuzzySearch(query)
}

// FuzzySearch ranks contacts by subsequence match so "jo" finds "John".
// Exact substring matches score best, subsequence matches still qualify.
func (cb *ContactBook) FuzzySearch(query string) []*Contact {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	trimmed := strings.ToLower(strings.TrimSpace(query))
	if trimmed == "" {
		return []*Contact{}
	}

	type scored struct {
		contact *Contact
		score   int
	}
	var ranked []scored

	for _, c := range cb.contacts {
		fields := []string{c.Name, c.DisplayName, c.JID, c.Notes}
		fields = append(fields, c.Tags...)
		best := -1
		for _, field := range fields {
			lowered := strings.ToLower(field)
			if lowered == "" {
				continue
			}
			if strings.Contains(lowered, trimmed) {
				// Exact substring: prefer early position and short field.
				score := strings.Index(lowered, trimmed) * 2
				if score < best || best < 0 {
					best = score
				}
				continue
			}
			if score, ok := fuzzyScore(trimmed, lowered); ok {
				score += 100
				if score < best || best < 0 {
					best = score
				}
			}
		}
		if best >= 0 {
			ranked = append(ranked, scored{c, best})
		}
	}

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score < ranked[j].score })
	results := make([]*Contact, 0, len(ranked))
	for _, r := range ranked {
		results = append(results, r.contact)
	}
	return results
}

func fuzzyScore(query, target string) (int, bool) {
	qi := 0
	score := 0
	lastMatch := -1
	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if target[ti] == query[qi] {
			if lastMatch >= 0 {
				score += ti - lastMatch - 1
			} else {
				score += ti
			}
			lastMatch = ti
			qi++
		}
	}
	if qi != len(query) {
		return 0, false
	}
	return score, true
}

// List returns all contacts, optionally filtered
func (cb *ContactBook) List(filter string) []*Contact {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	results := make([]*Contact, 0)

	for _, c := range cb.contacts {
		switch filter {
		case "favorites":
			if c.Favorite {
				results = append(results, c)
			}
		case "blocked":
			if c.Blocked {
				results = append(results, c)
			}
		case "recent":
			// Recent = seen in last 7 days
			if time.Since(c.LastSeen) < 7*24*time.Hour {
				results = append(results, c)
			}
		default:
			if !c.Blocked {
				results = append(results, c)
			}
		}
	}

	// Sort by last seen (most recent first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].LastSeen.After(results[j].LastSeen)
	})

	return results
}

// GetDisplayName returns the display name or fallback to name
func (c *Contact) GetDisplayName() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Name
}

// ShortJID returns the short version of JID (LID)
func (c *Contact) ShortJID() string {
	parts := strings.Split(c.JID, ":")
	if len(parts) == 2 && len(parts[1]) >= 8 {
		return parts[1][:8]
	}
	return c.JID
}

func (cb *ContactBook) load() error {
	data, err := os.ReadFile(cb.path)
	if err != nil {
		return err
	}

	var stored storedContactBook
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	for _, c := range stored.Contacts {
		cb.contacts[c.JID] = c
	}

	return nil
}

func (cb *ContactBook) save() error {
	contacts := make([]*Contact, 0, len(cb.contacts))
	for _, c := range cb.contacts {
		contacts = append(contacts, c)
	}

	stored := storedContactBook{Contacts: contacts}
	if err := os.MkdirAll(filepath.Dir(cb.path), 0o700); err != nil {
		return err
	}

	return pretty.SavePretty(cb.path, stored)
}

// DefaultContactsPath returns the default path for contacts file
func DefaultContactsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "contacts.json")
}
