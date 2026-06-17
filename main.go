package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- helpers ---------------------------------------------------------------

func truncate(s string, n int) string {
	if n < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func agoString(d time.Duration) string {
	switch {
	case d < 2*time.Second:
		return "just now"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s ago"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	default:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	}
}

// fuzzyFilter ranks threads whose titles fuzzy-match the query.
func fuzzyFilter(threads []thread, query string) []thread {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return threads
	}
	type scored struct {
		t thread
		s int
	}
	matches := make([]scored, 0, len(threads))
	for _, t := range threads {
		if s, ok := fuzzyScore(strings.ToLower(t.title), q); ok {
			matches = append(matches, scored{t, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].s > matches[j].s })
	out := make([]thread, len(matches))
	for i, mt := range matches {
		out[i] = mt.t
	}
	return out
}

// onOff renders a bool as "on"/"off" for help text.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// typedInto appends a printable key (a rune or a space) to s, reporting whether
// it consumed the key. Shared by the list filter, feed search, and ask fields.
// Runes are passed through stripControl: a pasted control byte could otherwise
// land in the field and, once echoed to the status bar, inject terminal escapes.
func typedInto(s string, msg tea.KeyMsg) (string, bool) {
	switch msg.Type {
	case tea.KeyRunes:
		return s + stripControl(string(msg.Runes)), true
	case tea.KeySpace:
		return s + " ", true
	}
	return s, false
}

// dropLastRune removes the final rune of s (the backspace edit for text fields).
func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// fuzzyScore scores q as a subsequence of text, rewarding contiguous runs and
// a whole-substring hit. ok is false if q's chars don't all appear in order.
// Both sides are compared rune by rune so non-ASCII titles match correctly.
func fuzzyScore(text, q string) (int, bool) {
	tr := []rune(text)
	ti, streak, score := 0, 0, 0
	for _, qr := range q {
		found := false
		for ti < len(tr) {
			if tr[ti] == qr {
				streak++
				score += streak * 2
				ti++
				found = true
				break
			}
			streak = 0
			ti++
		}
		if !found {
			return 0, false
		}
	}
	if strings.Contains(text, q) {
		score += 50
	}
	return score, true
}

// loadDotEnv reads a local .env (KEY=VALUE per line) and sets any vars not
// already in the environment. The only key it carries is OPENAI_API_KEY (the
// reddit session is handled in-app, never stored). Split on the first '=' only.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "=")
		if i < 1 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		val = strings.Trim(val, `"'`) // tolerate quoted values
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}

// setTerminalBG forces the terminal window background to the app's dark color
// via OSC 11, so the palette stays readable and uniform on any theme (including
// light or transparent ones). resetTerminalBG restores the default on exit.
// Terminals that don't support OSC 11 ignore both sequences harmlessly.
func setTerminalBG()   { fmt.Print("\x1b]11;" + string(colBg) + "\a") }
func resetTerminalBG() { fmt.Print("\x1b]111\a") }

func main() {
	loadDotEnv()
	initCookie() // load the cached reddit session, if any
	setTerminalBG()
	// No mouse capture: keep the terminal's native text selection and click-to-open links.
	_, err := tea.NewProgram(newModel(), tea.WithAltScreen()).Run()
	resetTerminalBG() // restore the user's background before we hand the terminal back
	if err != nil {
		fmt.Fprintln(os.Stderr, "oh-my-reddit error:", err)
		os.Exit(1)
	}
}
