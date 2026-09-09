package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one local security audit record. It never leaves the device:
// no keys, message bodies, or ciphertext are recorded — only who did
// what and when (e.g. who displayed a safety number and when).
type Entry struct {
	Timestamp time.Time `json:"timestamp"`
	Event     string    `json:"event"`
	Actor     string    `json:"actor,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

var (
	mu   sync.Mutex
	path string
)

// Init sets the audit log location (defaults to ~/.korik/audit.log).
func Init(customPath string) {
	mu.Lock()
	defer mu.Unlock()
	path = customPath
}

func logPath() string {
	mu.Lock()
	defer mu.Unlock()
	if path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "audit.log")
}

// Log appends one entry. Failures are silent so auditing can never
// break chat; use Len/checks in tests where needed.
func Log(event, actor, detail string) {
	entry := Entry{Timestamp: time.Now(), Event: event, Actor: actor, Detail: detail}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	target := logPath()
	_ = os.MkdirAll(filepath.Dir(target), 0o700)
	file, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}
