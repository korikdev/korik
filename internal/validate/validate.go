// Package validate provides central input validation for all incoming data.
// It enforces security invariants at trust boundaries to prevent parsing
// vulnerabilities, memory exhaustion, and path traversal attacks.
package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxMessageSize is the maximum allowed byte length of any message field.
	// Messages exceeding this limit are rejected to prevent memory exhaustion.
	MaxMessageSize = 1 << 20 // 1 MiB

	// MaxNicknameLen is the maximum allowed character count for a nickname.
	MaxNicknameLen = 32

	// MinNicknameLen is the minimum allowed character count for a nickname.
	MinNicknameLen = 1

	// PublicKeyLen is the exact byte length required for X25519 public keys.
	PublicKeyLen = 32

	// MaxFileTransferSize is the maximum allowed size for a file transfer offer.
	MaxFileTransferSize = 2 * 1024 * 1024 * 1024 // 2 GiB

	// JIDHexLen is the exact hex-encoded length of the public key in a JID.
	JIDHexLen = 64
)

// jidPattern matches the canonical JID format "korik:<64-hex-chars>".
var jidPattern = regexp.MustCompile(`^korik:[0-9a-fA-F]{64}$`)

// controlCharPattern matches Unicode control characters (categories Cc and Cf).
var controlCharPattern = regexp.MustCompile(`[\x00-\x1F\x7F]`)

// pathTraversalPatterns captures sequences used in path traversal attacks.
var pathTraversalPatterns = []string{
	"..",
	"\x00", // null byte
	"//",
}

// PublicKey validates that the given byte slice is exactly 32 bytes long,
// as required for an X25519 public key.
func PublicKey(key []byte) error {
	if len(key) != PublicKeyLen {
		return fmt.Errorf("public key must be exactly %d bytes, got %d", PublicKeyLen, len(key))
	}
	return nil
}

// JID validates that s matches the canonical JID format "korik:<64-hex-chars>".
func JID(s string) error {
	if !jidPattern.MatchString(s) {
		return fmt.Errorf("invalid JID format: must match korik:[64 hex chars], got %q", s)
	}
	return nil
}

// MessageJSON validates that data is a non-empty JSON object and does not
// exceed the maximum message size limit.
func MessageJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("message is empty")
	}
	if len(data) > MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum allowed %d bytes", len(data), MaxMessageSize)
	}
	// Quick structure check: must start with '{' after optional whitespace.
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("message must be a JSON object")
	}
	return nil
}

// Filename validates that the filename is safe: no path traversal sequences,
// no null bytes, and is not an absolute path.
func Filename(name string) error {
	if name == "" {
		return fmt.Errorf("filename must not be empty")
	}
	// Reject absolute paths.
	if strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':') {
		return fmt.Errorf("filename must not be an absolute path")
	}
	for _, pattern := range pathTraversalPatterns {
		if strings.Contains(name, pattern) {
			return fmt.Errorf("filename contains unsafe sequence %q", pattern)
		}
	}
	return nil
}

// ChunkIndex validates that the chunk index is within the expected range [0, total).
func ChunkIndex(index, total int) error {
	if total <= 0 {
		return fmt.Errorf("chunk total must be positive, got %d", total)
	}
	if index < 0 || index >= total {
		return fmt.Errorf("chunk index %d is out of range [0, %d)", index, total)
	}
	return nil
}

// Nickname validates that the nickname is 1-32 UTF-8 characters with no
// ASCII control characters.
func Nickname(name string) error {
	if !utf8.ValidString(name) {
		return fmt.Errorf("nickname contains invalid UTF-8 sequences")
	}
	runes := []rune(name)
	length := len(runes)
	if length < MinNicknameLen {
		return fmt.Errorf("nickname must be at least %d character", MinNicknameLen)
	}
	if length > MaxNicknameLen {
		return fmt.Errorf("nickname must not exceed %d characters, got %d", MaxNicknameLen, length)
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return fmt.Errorf("nickname must not contain control characters")
		}
	}
	return nil
}

// FileTransferSize validates that the offered file size does not exceed the
// configured 2 GiB maximum.
func FileTransferSize(size int64) error {
	if size < 0 {
		return fmt.Errorf("file size must not be negative")
	}
	if size > MaxFileTransferSize {
		return fmt.Errorf("file size %d exceeds maximum allowed %d bytes (2 GiB)", size, MaxFileTransferSize)
	}
	return nil
}

// MessageField validates that a single string field within a message does not
// exceed the maximum allowed size.
func MessageField(field string, value string) error {
	if len(value) > MaxMessageSize {
		return fmt.Errorf("field %q exceeds maximum size: %d > %d bytes", field, len(value), MaxMessageSize)
	}
	return nil
}
