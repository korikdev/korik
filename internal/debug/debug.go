package debug

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	Enabled bool
	Logger  *log.Logger
	mu      sync.Mutex
	logDir  string
	currentDate string
)

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	logDir = filepath.Join(home, ".korik", "logs")
}

// Init initializes the debug logger. When enabled, it creates the log
// directory, sets up the daily log file, and starts the 7-day retention
// cleanup. When disabled, it returns nil and writes no logs.
func Init(enabled bool) *log.Logger {
	mu.Lock()
	defer mu.Unlock()

	Enabled = enabled
	if !enabled {
		Logger = nil
		return nil
	}

	_ = os.MkdirAll(logDir, 0o700)
	cleanupExpiredLogs()

	date := time.Now().Format("2006-01-02")
	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", date))
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		Logger = nil
		return nil
	}

	Logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	currentDate = date

	return Logger
}

// rotate checks if the date has changed and rotates the log file.
func rotate() {
	mu.Lock()
	defer mu.Unlock()

	date := time.Now().Format("2006-01-02")
	if date == currentDate && Logger != nil {
		return
	}

	if Logger != nil {
		if src := Logger.Writer(); src != nil {
			if f, ok := src.(*os.File); ok {
				f.Close()
			}
		}
	}

	currentDate = date
	logFile := filepath.Join(logDir, fmt.Sprintf("korik-%s.log", date))
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	Logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	cleanupExpiredLogs()
}

// cleanupExpiredLogs removes log files older than 7 days.
func cleanupExpiredLogs() {
	cutoff := time.Now().AddDate(0, 0, -7)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "korik-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), "korik-")
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
}

// Log writes a debug message at the given level. If debug is disabled or
// the logger is nil, nothing is written. The message is sanitized before
// writing.
func Log(level, message string) {
	if !Enabled || Logger == nil {
		return
	}
	rotate()
	if Logger == nil {
		return
	}
	Logger.Printf("[%s] %s", strings.ToUpper(level), Sanitize(message))
}

// LogConnection logs a connection event with the peer JID.
func LogConnection(event, peerJID string) {
	if !Enabled || Logger == nil {
		return
	}
	rotate()
	if Logger == nil {
		return
	}
	Logger.Printf("[CONNECTION] event=%s peer=%s", Sanitize(event), Sanitize(peerJID))
}

// LogHandshake logs a handshake step for the given peer.
func LogHandshake(peerJID string, step string) {
	if !Enabled || Logger == nil {
		return
	}
	rotate()
	if Logger == nil {
		return
	}
	Logger.Printf("[HANDSHAKE] peer=%s step=%s", Sanitize(peerJID), Sanitize(step))
}

// LogNAT logs a NAT traversal step with its detail.
func LogNAT(step string, detail string) {
	if !Enabled || Logger == nil {
		return
	}
	rotate()
	if Logger == nil {
		return
	}
	Logger.Printf("[NAT] step=%s detail=%s", Sanitize(step), Sanitize(detail))
}

// LogProtocol logs protocol message metadata (type, length, peer). It does
// NOT log message content.
func LogProtocol(msgType string, length int, peerJID string) {
	if !Enabled || Logger == nil {
		return
	}
	rotate()
	if Logger == nil {
		return
	}
	Logger.Printf("[PROTOCOL] type=%s length=%d peer=%s encrypted=true",
		Sanitize(msgType), length, Sanitize(peerJID))
}

// Sanitize removes or masks sensitive data from log output. It strips
// private keys, session keys, and other potentially sensitive patterns.
func Sanitize(data string) string {
	data = strings.TrimSpace(data)

	privateKeyPatterns := []string{
		`(?s)-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----.*?-----END (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`,
	}
	for _, p := range privateKeyPatterns {
		re := regexp.MustCompile(p)
		data = re.ReplaceAllString(data, "[REDACTED_PRIVATE_KEY]")
	}

	sensitivePatterns := []struct {
		pattern string
		mask    string
	}{
		{`(?i)session[_-]?key["'\s:=]*["'\s]?[A-Za-z0-9+/=]{16,}`, "[REDACTED_SESSION_KEY]"},
		{`(?i)private[_-]?key["'\s:=]*["'\s]?[A-Za-z0-9+/=]{16,}`, "[REDACTED_PRIVATE_KEY]"},
		{`(?i)(?:secret|password|passphrase)["'\s:=]*["'\s]?[^\s,;}\]]+`, "[REDACTED_SECRET]"},
		{`(?i)encryption[_-]?key["'\s:=]*["'\s]?[A-Za-z0-9+/=]{16,}`, "[REDACTED_KEY]"},
		{`(?i)public[- ]?key["'\s:=]*["'\s]?[A-Za-z0-9+/=]{16,}`, "[REDACTED_PUBLIC_KEY]"},
	}
	for _, sp := range sensitivePatterns {
		re := regexp.MustCompile(sp.pattern)
		data = re.ReplaceAllStringFunc(data, func(string) string {
			return sp.mask
		})
	}

	// JID masking MUST run before the generic hex/base64 patterns below,
	// otherwise the hex suffix gets replaced first and the JID regex never matches.
	jidMask := regexp.MustCompile(`(?i)(korik:)[A-Za-z0-9+/=]{1,}`)
	data = jidMask.ReplaceAllStringFunc(data, func(m string) string {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) == 2 {
			return parts[0] + ":[REDACTED]"
		}
		return "[REDACTED]"
	})

	hexKeys := regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	data = hexKeys.ReplaceAllString(data, "[REDACTED_HEX_KEY]")

	base64Keys := regexp.MustCompile(`\b[A-Za-z0-9+/]{16,}={0,2}\b`)
	data = base64Keys.ReplaceAllString(data, "[REDACTED_BASE64_KEY]")

	data = regexp.MustCompile(`(?i)token["'\s:=]*["'\s]?[A-Za-z0-9_\-]{16,}`).ReplaceAllString(data, "[REDACTED_TOKEN]")

	data = regexp.MustCompile(`(?i)sig["'\s:=]*["'\s]?[A-Za-z0-9+/=]{16,}`).ReplaceAllString(data, "[REDACTED_SIG]")

	return data
}

// dateStr returns the current date string for log file naming.
func dateStr() string {
	return time.Now().Format("2006-01-02")
}

// logFilePath returns the log file path for the given date.
func logFilePath(date string) string {
	return filepath.Join(logDir, fmt.Sprintf("korik-%s.log", date))
}

// EnsureDebugFlag returns true if the --debug flag was passed, used by
// the main package to initialize the debug logger.
func EnsureDebugFlag() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--debug" || arg == "-debug" {
			return true
		}
	}
	return false
}
