package notify

import (
	"os/exec"
	"runtime"
)

// Send shows a native desktop notification for an incoming message.
// It degrades silently when no notifier is available, so chat never breaks.
func Send(title, body string) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", title, body).Start()
		}
	case "darwin":
		if _, err := exec.LookPath("terminal-notifier"); err == nil {
			_ = exec.Command("terminal-notifier", "-title", title, "-message", body).Start()
		} else if _, err := exec.LookPath("osascript"); err == nil {
			script := `display notification "` + escapeApple(body) + `" with title "` + escapeApple(title) + `"`
			_ = exec.Command("osascript", "-e", script).Start()
		}
	case "windows":
		// Best effort via PowerShell toast. Failures are ignored.
		if _, err := exec.LookPath("powershell"); err == nil {
			script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null`
			_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Start()
		}
	}
}

func escapeApple(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			out = append(out, '\\')
		}
		if c == '\n' || c == '\r' {
			out = append(out, ' ')
			continue
		}
		out = append(out, c)
	}
	return string(out)
}
