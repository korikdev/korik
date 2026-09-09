package filetransfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadChunkAtRandomAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.bin")
	payload := make([]byte, ChunkSize*3+100)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	transfer, err := m.CreateSendTransfer(path, "korik:peer")
	if err != nil {
		t.Fatal(err)
	}
	if transfer.ChunkTotal != 4 {
		t.Fatalf("expected 4 chunks, got %d", transfer.ChunkTotal)
	}
	// Random access: read last chunk first, then first.
	last, err := transfer.ReadChunkAt(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 100 {
		t.Fatalf("expected 100 bytes in last chunk, got %d", len(last))
	}
	first, err := transfer.ReadChunkAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != ChunkSize {
		t.Fatalf("expected full chunk, got %d", len(first))
	}
	for i := range first {
		if first[i] != payload[i] {
			t.Fatal("random-access content mismatch")
		}
	}
	if _, err := transfer.ReadChunkAt(99); err == nil {
		t.Fatal("expected out-of-range error")
	}
}
