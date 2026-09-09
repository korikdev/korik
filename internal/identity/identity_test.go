package identity

import (
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

// TestJIDMatchesPubkeyRoundTrip is a property test over random keys:
// the handshake anti-spoof check (JID == fingerprint of pubkey) must
// hold for every honestly generated keypair and fail for tampered JIDs.
func TestJIDMatchesPubkeyRoundTrip(t *testing.T) {
	for i := 0; i < 50; i++ {
		priv, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		id, err := fromRawPrivate(priv.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		pubHex := id.PublicKeyHex()
		if got := JIDFromPublicKeyHex(pubHex); got != id.JID {
			t.Fatalf("JID mismatch: %s vs %s", got, id.JID)
		}
		if want := LIDFromPublicKeyHex(pubHex); want != id.LID {
			t.Fatalf("LID mismatch: %s vs %s", want, id.LID)
		}
		tampered := "korik:00" + pubHex[2:]
		if JIDFromPublicKeyHex(pubHex) == tampered {
			t.Fatal("tampered JID must not match fingerprint")
		}
	}
}
