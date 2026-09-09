package debug

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitDisabled(t *testing.T) {
	Logger = nil
	Enabled = false
	result := Init(false)
	if result != nil {
		t.Error("expected nil logger when disabled")
	}
	if Enabled {
		t.Error("expected Enabled to be false")
	}
}

func TestInitEnabled(t *testing.T) {
	Logger = nil
	result := Init(true)
	if result == nil {
		t.Fatal("expected non-nil logger when enabled")
	}
	if !Enabled {
		t.Error("expected Enabled to be true")
	}
	if Logger == nil {
		t.Fatal("expected Logger to be set")
	}

	_ = os.RemoveAll(logDir)
}

func TestLogWhenDisabled(t *testing.T) {
	Enabled = false
	Logger = nil
	Init(false)

	logDir := filepath.Join(getHome(), ".korik", "logs")
	filesBefore, _ := os.ReadDir(logDir)

	Log("debug", "should not appear")
	LogConnection("connect", "korik:abc123")

	filesAfter, _ := os.ReadDir(logDir)
	if len(filesAfter) != len(filesBefore) {
		t.Error("no new log files should be created when disabled")
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "private key redacted",
			input:    "key: -----BEGIN RSA PRIVATE KEY-----\nabc123\n-----END RSA PRIVATE KEY-----",
			contains: "[REDACTED_PRIVATE_KEY]",
		},
		{
			name:     "session key redacted",
			input:    "session_key=abc123def456ghi789jkl012mno345pqr",
			contains: "[REDACTED_SESSION_KEY]",
		},
		{
			name:     "password redacted",
			input:    "password=supersecret",
			contains: "[REDACTED_SECRET]",
		},
		{
			name:     "hex key redacted",
			input:    "key=abcdef1234567890abcdef1234567890",
			contains: "[REDACTED_HEX_KEY]",
		},
		{
			name:     "JID redacted",
			input:    "peer=korik:abcdef1234567890",
			contains: "korik:[REDACTED]",
		},
		{
			name:     "normal text preserved",
			input:    "connection established with peer",
			contains: "connection established with peer",
		},
		{
			name:     "base64 key redacted",
			input:    "key=YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=",
			contains: "[REDACTED_BASE64_KEY]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Sanitize(tt.input)
			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("expected %q to contain %q, got %q", result, tt.contains, result)
			}
			if tt.excludes != "" && strings.Contains(result, tt.excludes) {
				t.Errorf("expected %q to not contain %q", result, tt.excludes)
			}
		})
	}
}

func TestSanitizePreservesMetadata(t *testing.T) {
	input := "PROTOCOL type=chat length=42 peer=korik:xyz timestamp=2026-09-09T12:00:00Z encrypted=true"
	result := Sanitize(input)
	if !strings.Contains(result, "type=chat") {
		t.Error("expected message type to be preserved")
	}
	if !strings.Contains(result, "length=42") {
		t.Error("expected length to be preserved")
	}
	if !strings.Contains(result, "encrypted=true") {
		t.Error("expected encrypted status to be preserved")
	}
}

func TestLogConnection(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	LogConnection("connected", "korik:peer123")
	LogConnection("disconnected", "korik:peer456")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[CONNECTION]") {
		t.Error("expected [CONNECTION] in log output")
	}
	if !strings.Contains(content, "event=connected") {
		t.Error("expected connected event in log")
	}
	if !strings.Contains(content, "peer=korik:[REDACTED]") {
		t.Error("expected peer JID to be sanitized")
	}
}

func TestLogProtocol(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	LogProtocol("chat", 1024, "korik:peer789")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[PROTOCOL]") {
		t.Error("expected [PROTOCOL] in log output")
	}
	if !strings.Contains(content, "length=1024") {
		t.Error("expected length to be logged")
	}
	if !strings.Contains(content, "encrypted=true") {
		t.Error("expected encrypted status to be logged")
	}
}

func TestLogWhenDisabledDoesNotWrite(t *testing.T) {
	Init(false)
	if Logger != nil {
		t.Fatal("expected nil logger")
	}

	logDir := filepath.Join(getHome(), ".korik", "logs")
	_ = os.MkdirAll(logDir, 0o700)

	before, _ := os.ReadDir(logDir)
	countBefore := len(before)

	Log("info", "test message")

	after, _ := os.ReadDir(logDir)
	if len(after) != countBefore {
		t.Error("no new log files should be created when debug is disabled")
	}
}

func TestCleanupExpiredLogs(t *testing.T) {
	_ = os.MkdirAll(logDir, 0o700)

	oldDate := time.Now().AddDate(0, 0, -8).Format("2006-01-02")
	oldLog := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", oldDate))
	if err := os.WriteFile(oldLog, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	recentDate := time.Now().Format("2006-01-02")
	recentLog := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", recentDate))
	if err := os.WriteFile(recentLog, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupExpiredLogs()

	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Error("expected old log file to be removed")
	}
	if _, err := os.Stat(recentLog); err != nil {
		t.Error("expected recent log file to exist")
	}

	_ = os.RemoveAll(logDir)
}

func TestLogRotation(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	rotate()
	if Logger == nil {
		t.Fatal("expected logger after rotation")
	}
}

func TestEnsureDebugFlag(t *testing.T) {
	os.Args = []string{"korik", "--debug"}
	if !EnsureDebugFlag() {
		t.Error("expected --debug to be detected")
	}

	os.Args = []string{"korik"}
	if EnsureDebugFlag() {
		t.Error("expected no debug flag")
	}
}

func getHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func TestSanitizeEmpty(t *testing.T) {
	result := Sanitize("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSanitizeNormalMessage(t *testing.T) {
	result := Sanitize("handshake complete")
	if result != "handshake complete" {
		t.Errorf("expected unchanged, got %q", result)
	}
}

func TestLogMultipleLevels(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	Log("debug", "debug message")
	Log("info", "info message")
	Log("error", "error message")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[DEBUG]") {
		t.Error("expected [DEBUG] level")
	}
	if !strings.Contains(content, "[INFO]") {
		t.Error("expected [INFO] level")
	}
	if !strings.Contains(content, "[ERROR]") {
		t.Error("expected [ERROR] level")
	}
}

func TestLogHandshake(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	LogHandshake("korik:peerABC", "key exchange")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[HANDSHAKE]") {
		t.Error("expected [HANDSHAKE] in log")
	}
	if !strings.Contains(content, "peer=korik:[REDACTED]") {
		t.Error("expected peer JID to be redacted")
	}
}

func TestLogNAT(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	LogNAT("stun", "mapped address discovered")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[NAT]") {
		t.Error("expected [NAT] in log")
	}
	if !strings.Contains(content, "step=stun") {
		t.Error("expected step to be logged")
	}
}

func TestLogDoesNotContainMessageBody(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	LogProtocol("chat", 13, "korik:peer")
	Log("debug", "secret body content here")

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if strings.Contains(content, "secret body content here") {
		t.Error("debug message body should not contain sensitive content")
	}
	if strings.Contains(content, "body=") {
		t.Error("log should not contain message body fields")
	}
}

func TestLogFileExists(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("expected log file to exist")
	}
}

func TestLogWithWriter(t *testing.T) {
	Init(true)
	defer func() {
		if Logger != nil {
			if f, ok := Logger.Writer().(*os.File); ok {
				f.Close()
			}
		}
		_ = os.RemoveAll(logDir)
	}()

	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", time.Now().Format("2006-01-02")))
	Log("debug", "test")

	f, err := os.Open(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	found := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "[DEBUG]") {
			found = true
		}
	}
	if !found {
		t.Error("expected to find [DEBUG] entry in log file")
	}
}
