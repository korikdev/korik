package config

import (
	"path/filepath"
	"testing"

	"korik/internal/pretty"
)

func TestConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := DefaultConfig()
	cfg.Name = "alice"
	cfg.Port = 10000

	if err := cfg.Save(path); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Name != cfg.Name || loaded.Port != cfg.Port {
		t.Fatalf("config field mismatch after roundtrip: got %+v, want %+v", loaded, cfg)
	}

	// Verify pretty round-trip integrity
	data, err := pretty.LoadAndPretty(path)
	if err != nil {
		t.Fatalf("failed to load and pretty-print config: %v", err)
	}

	if _, err := pretty.RoundTrip(data); err != nil {
		t.Fatalf("config pretty roundtrip integrity check failed: %v", err)
	}
}
