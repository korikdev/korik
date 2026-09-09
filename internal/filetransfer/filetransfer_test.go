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

func TestMulticastSendTransfers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multicast.bin")
	if err := os.WriteFile(path, []byte("multicast file content data"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	peers := []string{"korik:peer1", "korik:peer2", "korik:peer3"}
	transfers, err := m.CreateMulticastSendTransfers(path, peers)
	if err != nil {
		t.Fatalf("unexpected error creating multicast transfers: %v", err)
	}

	if len(transfers) != len(peers) {
		t.Fatalf("expected %d transfers, got %d", len(peers), len(transfers))
	}

	for i, tr := range transfers {
		if tr.PeerJID != peers[i] {
			t.Errorf("expected PeerJID %s, got %s", peers[i], tr.PeerJID)
		}
		if tr.Direction != "send" {
			t.Errorf("expected direction send, got %s", tr.Direction)
		}
	}

	peer1Transfers := m.GetTransfersByPeer("korik:peer1")
	if len(peer1Transfers) != 1 {
		t.Fatalf("expected 1 transfer for peer1, got %d", len(peer1Transfers))
	}
}
