package backup

import (
	"path/filepath"
	"testing"
)

func TestExportImportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	payload := Payload{PrivateKeyHex: "9d5c5c8e8f0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4"}
	if err := Export(path, "correct-horse-8", payload); err != nil {
		t.Fatal(err)
	}
	restored, err := Import(path, "correct-horse-8")
	if err != nil {
		t.Fatal(err)
	}
	if restored.PrivateKeyHex != payload.PrivateKeyHex {
		t.Fatal("private key mismatch after round trip")
	}
}

func TestImportWrongPasswordFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	payload := Payload{PrivateKeyHex: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}
	if err := Export(path, "right-password-1", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(path, "wrong-password-2"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestExportShortPasswordRejected(t *testing.T) {
	dir := t.TempDir()
	if err := Export(filepath.Join(dir, "b.json"), "short", Payload{}); err == nil {
		t.Fatal("expected rejection of short password")
	}
}
