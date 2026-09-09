package config

import (
	"testing"
)

func TestEnsureDeviceIDStable(t *testing.T) {
	cfg := DefaultConfig()
	first := cfg.DeviceID
	if first == "" {
		t.Fatal("default config must include a device ID")
	}
	if cfg.EnsureDeviceID() {
		t.Fatal("EnsureDeviceID must not overwrite an existing ID")
	}
	cfg.DeviceID = ""
	if !cfg.EnsureDeviceID() {
		t.Fatal("EnsureDeviceID must assign a missing ID")
	}
	if cfg.DeviceID == "" {
		t.Fatal("device ID must be assigned")
	}
}
