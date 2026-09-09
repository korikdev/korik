package ui

import (
	"fmt"
	"strings"
	"time"
)

// ANSI color codes for terminal clarity. No external dependency.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"
	White   = "\033[97m"
)

// TimestampMode controls how timestamps are rendered.
type TimestampMode string

const (
	TimestampAbsolute TimestampMode = "absolute"
	TimestampRelative TimestampMode = "relative"
)

// FormatTimestamp renders ts according to mode.
// Absolute: "15:04:05", Relative: "2m ago" / "just now".
func FormatTimestamp(ts time.Time, mode TimestampMode) string {
	if mode == TimestampRelative {
		return FormatRelative(ts)
	}
	return Dim + ts.Format("15:04:05") + Reset
}

// FormatRelative renders a human relative time like "2m ago".
func FormatRelative(ts time.Time) string {
	if ts.IsZero() {
		return Dim + "never" + Reset
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return Dim + "just now" + Reset
	case d < time.Hour:
		return Dim + fmt.Sprintf("%dm ago", int(d.Minutes())) + Reset
	case d < 24*time.Hour:
		return Dim + fmt.Sprintf("%dh ago", int(d.Hours())) + Reset
	case d < 7*24*time.Hour:
		return Dim + fmt.Sprintf("%dd ago", int(d.Hours()/24)) + Reset
	default:
		return Dim + ts.Format("2006-01-02") + Reset
	}
}

// Incoming formats a received chat message in green.
func Incoming(name, body string, ts time.Time, mode TimestampMode) string {
	return fmt.Sprintf("%s%s%s %s: %s",
		FormatTimestamp(ts, mode), Reset, Green, name, body)
}

// Outgoing formats a sent chat message in cyan with arrow.
func Outgoing(name, body string, ts time.Time, mode TimestampMode) string {
	return fmt.Sprintf("%s %s→ %s: %s%s",
		FormatTimestamp(ts, mode), Cyan, name, body, Reset)
}

// System formats informational system messages in yellow.
func System(body string) string {
	return Yellow + "[*] " + body + Reset
}

// PeerEvent formats join/leave events in blue.
func PeerEvent(body string) string {
	return Blue + body + Reset
}

// Typing formats typing indicators in gray italic-like dim.
func Typing(body string) string {
	return Gray + body + Reset
}

// Success formats success confirmations in green bold.
func Success(body string) string {
	return Green + Bold + "[✓] " + body + Reset
}

// Failure formats errors in red, never exposing raw internals.
func Failure(hint string) string {
	return Red + Bold + "Error: " + Reset + Red + hint + Reset
}

// FileEvent formats file transfer updates in magenta.
func FileEvent(body string) string {
	return Magenta + body + Reset
}

// Hint formats usage hints in dim white.
func Hint(body string) string {
	return Gray + body + Reset
}

// Truncate shortens long strings for compact display.
func Truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}

// PadRight pads s to width for aligned columns.
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
