package contacts

import (
	"path/filepath"
	"testing"

	"korik/internal/pretty"
)

func TestContacts_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")

	cb, err := NewContactBook(path)
	if err != nil {
		t.Fatalf("failed to create contact book: %v", err)
	}

	jid := "korik:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cb.Add(jid, "Bob")
	if err := cb.SetDisplayName(jid, "Bobbie"); err != nil {
		t.Fatalf("failed to set display name: %v", err)
	}
	if err := cb.SetFavorite(jid, true); err != nil {
		t.Fatalf("failed to set favorite: %v", err)
	}

	cb2, err := NewContactBook(path)
	if err != nil {
		t.Fatalf("failed to reload contact book: %v", err)
	}

	contact, ok := cb2.Get(jid)
	if !ok {
		t.Fatalf("contact %s missing after reload", jid)
	}
	if contact.DisplayName != "Bobbie" || !contact.Favorite {
		t.Fatalf("contact data mismatch after reload: %+v", contact)
	}

	// Pretty roundtrip check
	data, err := pretty.LoadAndPretty(path)
	if err != nil {
		t.Fatalf("failed to read pretty contacts: %v", err)
	}
	if _, err := pretty.RoundTrip(data); err != nil {
		t.Fatalf("contacts pretty roundtrip integrity check failed: %v", err)
	}
}
