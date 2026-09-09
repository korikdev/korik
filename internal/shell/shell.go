package shell

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

// Shell is a minimal interactive line reader with history file,
// arrow up/down recall, and Tab completion for commands and contacts.
type Shell struct {
	historyPath string
	history     []string
	commands    []string
	contacts    func() []string
}

// New creates a shell with persistent history.
func New(historyPath string, commands []string) *Shell {
	s := &Shell{historyPath: historyPath, commands: commands}
	s.loadHistory()
	return s
}

// SetContactProvider sets a callback returning contact names for completion.
func (s *Shell) SetContactProvider(fn func() []string) {
	s.contacts = fn
}

// AddHistory appends a line to history and persists it.
func (s *Shell) AddHistory(line string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(s.history) > 0 && s.history[len(s.history)-1] == line {
		return
	}
	s.history = append(s.history, line)
	if len(s.history) > 500 {
		s.history = s.history[len(s.history)-500:]
	}
	s.saveHistory()
}

// Complete returns candidates for a Tab press given the current buffer.
func (s *Shell) Complete(buffer string) []string {
	if strings.HasPrefix(buffer, "/") {
		fields := strings.Fields(buffer)
		if len(fields) <= 1 && !strings.Contains(buffer, " ") {
			prefix := buffer
			var out []string
			for _, c := range s.commands {
				if strings.HasPrefix(c, prefix) {
					out = append(out, c)
				}
			}
			sort.Strings(out)
			return out
		}
		// Complete contact names as second argument.
		if s.contacts != nil && len(fields) >= 1 {
			last := ""
			if !strings.HasSuffix(buffer, " ") {
				last = fields[len(fields)-1]
			}
			var out []string
			for _, name := range s.contacts() {
				if last == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(last)) {
					out = append(out, fields[0]+" "+name)
				}
			}
			sort.Strings(out)
			return out
		}
		return nil
	}
	// Plain message: complete contact names with @mention.
	if s.contacts == nil {
		return nil
	}
	idx := strings.LastIndex(buffer, "@")
	if idx < 0 {
		return nil
	}
	prefix := buffer[idx+1:]
	if strings.Contains(prefix, " ") {
		return nil
	}
	var out []string
	for _, name := range s.contacts() {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			out = append(out, buffer[:idx+1]+name)
		}
	}
	return out
}

// FuzzyFilter ranks candidates by subsequence match (case-insensitive).
// "jo" matches "John" and "Marijo".
func FuzzyFilter(query string, candidates []string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return candidates
	}
	type scored struct {
		value string
		score int
	}
	var out []scored
	for _, c := range candidates {
		lowered := strings.ToLower(c)
		score, ok := fuzzyScore(q, lowered)
		if ok {
			out = append(out, scored{c, score})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score < out[j].score })
	result := make([]string, 0, len(out))
	for _, s := range out {
		result = append(result, s.value)
	}
	return result
}

func fuzzyScore(query, target string) (int, bool) {
	qi := 0
	score := 0
	lastMatch := -1
	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if target[ti] == query[qi] {
			if lastMatch >= 0 {
				score += ti - lastMatch - 1
			} else if ti > 0 {
				score += ti
			}
			lastMatch = ti
			qi++
		}
	}
	if qi != len(query) {
		return 0, false
	}
	return score, true
}

// ReadLine reads one line with arrow history and Tab completion.
// Falls back to plain scanning when stdin is not a terminal.
func (s *Shell) ReadLine(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Fallback path uses a simple reader.
		buf := make([]byte, 0, 256)
		tmp := make([]byte, 1)
		fmt.Fprint(os.Stdout, prompt)
		for {
			n, err := os.Stdin.Read(tmp)
			if n == 0 || err != nil {
				if err == io.EOF && len(buf) > 0 {
					return string(buf), nil
				}
				return "", err
			}
			if tmp[0] == '\n' {
				return string(buf), nil
			}
			if tmp[0] != '\r' {
				buf = append(buf, tmp[0])
			}
		}
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), oldState)
	}()

	fmt.Fprint(os.Stdout, prompt)
	buf := make([]rune, 0, 256)
	pos := 0
	histIdx := len(s.history)

	redraw := func() {
		fmt.Fprint(os.Stdout, "\r\033[K"+prompt+string(buf))
		// Move cursor back to pos.
		if len(buf)-pos > 0 {
			fmt.Fprintf(os.Stdout, "\033[%dD", len(buf)-pos)
		}
	}

	tmp := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(tmp)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		b := tmp[0]

		// Enter.
		if b == '\r' || b == '\n' {
			fmt.Fprint(os.Stdout, "\r\n")
			return string(buf), nil
		}
		// Ctrl-C / Ctrl-D.
		if b == 3 || b == 4 {
			if len(buf) == 0 {
				fmt.Fprint(os.Stdout, "\r\n")
				return "", io.EOF
			}
			continue
		}
		// Tab completion.
		if b == '\t' {
			cands := s.Complete(string(buf))
			if len(cands) == 1 {
				buf = []rune(cands[0])
				pos = len(buf)
				redraw()
			} else if len(cands) > 1 {
				fmt.Fprint(os.Stdout, "\r\n")
				for _, c := range cands {
					fmt.Fprint(os.Stdout, "  "+c+"\r\n")
				}
				redraw()
			}
			continue
		}
		// Backspace.
		if b == 127 || b == 8 {
			if pos > 0 {
				buf = append(buf[:pos-1], buf[pos:]...)
				pos--
				redraw()
			}
			continue
		}
		// Escape sequences (arrows).
		if b == 27 && n >= 3 && tmp[1] == '[' {
			switch tmp[2] {
			case 'A': // Up: older history.
				if histIdx > 0 {
					histIdx--
					buf = []rune(s.history[histIdx])
					pos = len(buf)
					redraw()
				}
			case 'B': // Down: newer history.
				if histIdx < len(s.history) {
					histIdx++
					if histIdx == len(s.history) {
						buf = buf[:0]
					} else {
						buf = []rune(s.history[histIdx])
					}
					pos = len(buf)
					redraw()
				}
			case 'C': // Right.
				if pos < len(buf) {
					pos++
					fmt.Fprint(os.Stdout, "\033[C")
				}
			case 'D': // Left.
				if pos > 0 {
					pos--
					fmt.Fprint(os.Stdout, "\033[D")
				}
			}
			continue
		}
		if b == 27 {
			continue
		}
		// Printable.
		if b >= 32 && b < 127 {
			r := rune(b)
			buf = append(buf[:pos], append([]rune{r}, buf[pos:]...)...)
			pos++
			redraw()
			continue
		}
	}
}

func (s *Shell) loadHistory() {
	data, err := os.ReadFile(s.historyPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" {
			s.history = append(s.history, l)
		}
	}
	if len(s.history) > 500 {
		s.history = s.history[len(s.history)-500:]
	}
}

func (s *Shell) saveHistory() {
	start := 0
	if len(s.history) > 500 {
		start = len(s.history) - 500
	}
	content := strings.Join(s.history[start:], "\n") + "\n"
	_ = os.MkdirAll(dirOf(s.historyPath), 0o700)
	_ = os.WriteFile(s.historyPath, []byte(content), 0o600)
}

func dirOf(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}
