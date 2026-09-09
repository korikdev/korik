package cryptoutil

import (
	"bytes"
	"testing"
)

func TestMixKeysChangesSession(t *testing.T) {
	oldKey := bytes.Repeat([]byte{0x01}, 32)
	fresh := bytes.Repeat([]byte{0x02}, 32)
	mixed := MixKeys(oldKey, fresh)
	if len(mixed) != 32 {
		t.Fatalf("expected 32-byte mixed key, got %d", len(mixed))
	}
	if bytes.Equal(mixed, oldKey) || bytes.Equal(mixed, fresh) {
		t.Fatal("mixed key must differ from both inputs")
	}
	if !bytes.Equal(mixed, MixKeys(oldKey, fresh)) {
		t.Fatal("MixKeys must be deterministic")
	}
}
