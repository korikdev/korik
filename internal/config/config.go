package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"korik/internal/pretty"
)

// Config represents application configuration
type Config struct {
	Name            string `json:"name,omitempty"`
	Port            int    `json:"port,omitempty"`
	DiscoveryPort   int    `json:"discovery_port,omitempty"`
	HistorySize     int    `json:"history_size,omitempty"`
	STUNServer      string `json:"stun_server,omitempty"`
	NoDiscovery     bool   `json:"no_discovery,omitempty"`
	InternetMode    bool   `json:"internet_mode,omitempty"`
	ReadReceipts    bool   `json:"read_receipts,omitempty"`
	TypingIndicator bool   `json:"typing_indicator,omitempty"`
	AutoReconnect   bool   `json:"auto_reconnect,omitempty"`
	DeviceID        string `json:"device_id,omitempty"`
	DeviceLabel     string `json:"device_label,omitempty"`
}

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		Name:            "korik-user",
		Port:            9999,
		DiscoveryPort:   9998,
		HistorySize:     1000,
		STUNServer:      "stun.l.google.com:19302",
		NoDiscovery:     false,
		InternetMode:    false,
		ReadReceipts:    false,
		TypingIndicator: true,
		AutoReconnect:   true,
		DeviceID:        newDeviceID(),
		DeviceLabel:     "cli-1",
	}
}

func newDeviceID() string {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "cli-1"
	}
	return "cli-" + hex.EncodeToString(raw)[:8]
}

// EnsureDeviceID assigns a stable device ID when missing (CLI-only linked devices).
func (c *Config) EnsureDeviceID() bool {
	if c.DeviceID != "" {
		return false
	}
	c.DeviceID = newDeviceID()
	if c.DeviceLabel == "" {
		c.DeviceLabel = "cli-1"
	}
	return true
}

// Load reads config from file, or returns default if not exists
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes config to file using pretty printing.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return pretty.SavePretty(path, c)
}

// DefaultConfigPath returns the default path for config file
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "config.json")
}
