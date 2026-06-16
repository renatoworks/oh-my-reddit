package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) renderInput() string {
	banner := wordmark() // header is static; only the recents stagger in
	tagline := taglineFor(m.colW()-4, colMuted)
	boxW := max(20, m.colW()-4) // span the content column so the full placeholder fits
	inner := max(8, boxW-6)     // text area: inside border + padding, minus the "› " prompt
	m.input.Placeholder = inputPlaceholder(inner)
	m.input.Width = inner // scroll a long URL horizontally instead of wrapping it
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Padding(0, 1).
		Width(boxW).
		Render(m.input.View())

	parts := []string{banner, tagline, "", box}
	if m.err != nil {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(colAccent).Render("  "+m.err.Error()))
	}

	// Recents — selectable with ↑/↓ (index runs subs first, then threads), each
	// staggering in (fade index capped so a long list isn't slow).
	row := func(sel bool, i int) (string, lipgloss.Style) {
		t := m.fade(i)
		if sel {
			return lipgloss.NewStyle().Foreground(fadeColor(colAccent, t)).Render("▌ "),
				lipgloss.NewStyle().Foreground(fadeColor(colText, t)).Bold(true)
		}
		return "  ", lipgloss.NewStyle().Foreground(fadeColor(colMuted, t))
	}
	label := func(s string, i int) string {
		return lipgloss.NewStyle().Foreground(fadeColor(colFaint, m.fade(i))).Bold(true).Render(strings.ToUpper(s))
	}
	idx := 0
	if len(m.recentSubs) > 0 {
		parts = append(parts, "", label("recent subreddits", idx))
		for _, s := range m.recentSubs {
			bar, st := row(idx == m.inputCursor, idx)
			parts = append(parts, bar+st.Render("r/"+s))
			idx++
		}
	}
	if len(m.recentThreads) > 0 {
		avail := max(20, m.colW()-6) // column minus block padding (4) and the row bar (2)
		parts = append(parts, "", label("recent threads", idx))
		for _, t := range m.recentThreads {
			bar, st := row(idx == m.inputCursor, idx)
			sub := lipgloss.NewStyle().Foreground(fadeColor(colFaint, m.fade(idx))).Render("  r/" + t.Sub)
			titleW := max(10, avail-lipgloss.Width(sub)) // leave room so r/sub always fits
			parts = append(parts, bar+st.Render(hideEmoji(truncate(t.Title, titleW), m.fade(idx) >= 1))+sub)
			idx++
		}
	}

	block := lipgloss.NewStyle().Padding(0, 2).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return m.centerPage(block, m.footerWithNotice(m.footerBarCentered(m.inputFooter())))
}

// inputFooter is the responsive footer line. The signed-in username is always
// first (collapsing to just "u/name" when tight); shortcuts follow and drop by
// priority as the window narrows. Drop/collapse order (narrowing): ctrl+x clear,
// ctrl+l log out, ↑/↓ recents, the "signed in as" prefix, then enter, then quit.
func (m model) inputFooter() string {
	avail := m.width - 2 // fits inside the full-width footer bar (statusStyle pads 1 each side)
	hasRecents := len(m.recentSubs) > 0 || len(m.recentThreads) > 0
	signedIn := m.username != ""

	prefix := signedIn // show "signed in as " before the username chip
	show := map[string]bool{
		"recents": hasRecents,
		"enter":   true,
		"clear":   hasRecents,
		"logout":  signedIn, // ctrl+l is only meaningful when signed in
		"quit":    true,
	}

	build := func() string {
		var parts []string
		if signedIn {
			chip := lipgloss.NewStyle().Foreground(colMuted).Render("u/" + m.username)
			if prefix {
				chip = metaStyle.Render("signed in as ") + chip
			}
			parts = append(parts, chip)
		}
		add := func(key, text string) {
			if show[key] {
				parts = append(parts, metaStyle.Render(text))
			}
		}
		add("recents", "↑/↓ recents")
		add("enter", "enter select")
		add("clear", "ctrl+x clear")
		add("logout", "ctrl+l log out")
		add("quit", "ctrl+c quit")
		return strings.Join(parts, metaStyle.Render(" · "))
	}

	// Shed pieces in ascending priority until the line fits.
	drops := []struct {
		key  string
		drop func()
	}{
		{"clear", func() { show["clear"] = false }},
		{"logout", func() { show["logout"] = false }},
		{"recents", func() { show["recents"] = false }},
		{"prefix", func() { prefix = false }}, // collapse "signed in as u/x" → "u/x"
		{"enter", func() { show["enter"] = false }},
		{"quit", func() { show["quit"] = false }},
	}
	for _, d := range drops {
		if lipgloss.Width(build()) <= avail {
			break
		}
		d.drop()
	}
	return build()
}

// inputPlaceholder picks the longest placeholder that fits the box, shedding
// detail as it narrows (like the footer) so it never wraps to a second line.
func inputPlaceholder(width int) string {
	for _, o := range []string{
		"r/soccer   ·   paste a thread URL   ·   or type 'demo'",
		"r/soccer · a thread URL · 'demo'",
		"r/soccer or a thread URL",
		"a subreddit or URL",
		"r/soccer",
	} {
		if lipgloss.Width(o) <= width {
			return o
		}
	}
	return "r/…"
}
