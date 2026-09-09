package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSendArgs(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("test content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Case 1: Only file path provided -> multicast target ("")
	fp, target, err := parseSendArgs(filePath, tempDir)
	if err != nil {
		t.Fatalf("unexpected error for single file path: %v", err)
	}
	if fp != filePath {
		t.Errorf("expected filePath %s, got %s", filePath, fp)
	}
	if target != "" {
		t.Errorf("expected empty target for multicast, got %s", target)
	}

	// Case 2: File path with target peer
	fp, target, err = parseSendArgs(filePath+" korik:peer123", tempDir)
	if err != nil {
		t.Fatalf("unexpected error for file path with target: %v", err)
	}
	if fp != filePath {
		t.Errorf("expected filePath %s, got %s", filePath, fp)
	}
	if target != "korik:peer123" {
		t.Errorf("expected target korik:peer123, got %s", target)
	}

	// Case 3: Invalid file path
	_, _, err = parseSendArgs("/nonexistent/file.txt peer1", tempDir)
	if err == nil {
		t.Error("expected error for non-existent file path, got nil")
	}
}
