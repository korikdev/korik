package identity

import (
	"path/filepath"
	"testing"

	"korik/internal/pretty"
)

func TestIdentity_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	id1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("failed to create identity: %v", err)
	}

	id2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("failed to load identity: %v", err)
	}

	if id1.JID != id2.JID || id1.LID != id2.LID {
		t.Fatalf("identity mismatch: id1=%s, id2=%s", id1.JID, id2.JID)
	}

	if id1.PublicKeyHex() != id2.PublicKeyHex() {
		t.Fatalf("pubkey hex mismatch: %s != %s", id1.PublicKeyHex(), id2.PublicKeyHex())
	}

	// Pretty roundtrip check
	data, err := pretty.LoadAndPretty(path)
	if err != nil {
		t.Fatalf("failed to load pretty identity: %v", err)
	}

	if _, err := pretty.RoundTrip(data); err != nil {
		t.Fatalf("identity pretty roundtrip integrity check failed: %v", err)
	}
}
