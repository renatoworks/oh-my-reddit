package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderAuth is the first-run screen that acquires a logged-in reddit session
// from the browser: a searching state, a failure state with options, and a
// manual cookie-paste sub-mode.
func (m model) renderAuth() string {
	// Searching is minimal: just the loader centered, no wordmark/tagline.
	if m.authMode == authSearching {
		loader := m.loaderLine("searching your browser for a logged-in reddit session…")
		return m.centerPage(loader, m.footerBarCentered(metaStyle.Render("esc cancel · q quit")))
	}

	parts := []string{wordmarkFaded(m.fade(0)), taglineFor(m.colW()-4, fadeColor(colMuted, m.fade(1))), ""}
	hint := "↵ connect · o reddit.com · p paste · d demo · q quit"

	switch {
	case m.authMode == authPicking:
		parts = append(parts, bodyMutedStyle.Render("choose an account to sign in as:"), "")
		for i, a := range m.authAccounts {
			bar, nameStyle := "  ", bodyMutedStyle
			if i == m.authCursor {
				bar = lipgloss.NewStyle().Foreground(colAccent).Render("▌ ")
				nameStyle = lipgloss.NewStyle().Foreground(colText).Bold(true)
			}
			parts = append(parts, bar+nameStyle.Render("u/"+a.username)+metaStyle.Render("    "+a.source))
		}
		hint = "↑/↓ select · enter sign in · esc back"
	case m.authMode == authPasting:
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1).
			Width(min(70, max(24, m.colW()-4))).
			Render(m.authInput.View())
		gutter := lipgloss.NewStyle().Foreground(colAccent).Render("▌ ")
		parts = append(parts,
			gutter+lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("how")+metaStyle.Render("  on reddit.com (logged in):"),
			gutter+bodyMutedStyle.Render("open DevTools → Network tab → click any reddit request →"),
			gutter+bodyMutedStyle.Render("under Request Headers, copy the whole Cookie: value,"),
			gutter+bodyMutedStyle.Render("then paste it below."),
			"",
			box,
		)
		hint = "enter save session · esc back"
	default: // idle menu — and, after a failed attempt, why it came up empty
		wrapW := max(20, m.colW()-4) // wrap long lines within the content column
		msg := m.fade(2)             // the failure messages fade in together
		if m.authExpired {
			parts = append(parts, lipgloss.NewStyle().Foreground(fadeColor(colAccent, msg)).Width(wrapW).Render("your reddit session expired — reconnect to keep browsing."), "")
		}
		if m.authUnreached {
			parts = append(parts, lipgloss.NewStyle().Foreground(fadeColor(colAccent, msg)).Width(wrapW).Render("found a session but couldn't reach reddit to verify it — check your connection and try again."), "")
		} else if m.authTried {
			parts = append(parts, lipgloss.NewStyle().Foreground(fadeColor(colAccent, msg)).Width(wrapW).Render("couldn't find a logged-in reddit session."), "")
			if len(m.authChecked) > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(fadeColor(colMuted, msg)).Width(wrapW).Render("checked: "+strings.Join(m.authChecked, " · ")))
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(fadeColor(colFaint, msg)).Width(wrapW).Render("chrome, brave, edge must be logged in + grant the Keychain “Allow”; safari needs Full Disk Access"), "")
		}
		connect := "connect with my reddit session"
		if m.authTried {
			connect = "try again"
		}
		// Options stagger in after the wordmark, tagline, and any messages.
		parts = append(parts,
			m.authOption("↵", connect, 3),
			m.authOption("o", "open reddit.com to log in", 4),
			m.authOption("p", "paste a cookie manually", 5),
			m.authOption("d", "demo feed", 6),
			m.authOption("q", "quit", 7),
		)
	}
	block := lipgloss.NewStyle().Padding(0, 2).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	// The idle menu lists its own shortcuts as option rows, so it skips the footer
	// bar (which would just repeat them). The picker/paste sub-states keep their
	// contextual hints. The update notice, if any, stays pinned at the bottom.
	bar := ""
	if m.authMode == authPicking || m.authMode == authPasting {
		bar = m.footerBarCentered(metaStyle.Render(hint))
	}
	return m.centerPage(block, m.footerWithNotice(bar))
}

// authOption renders one "key  label" choice on the auth screen, faded in by the
// entrance animation at element index i.
func (m model) authOption(key, label string, i int) string {
	t := m.fade(i)
	return lipgloss.NewStyle().Foreground(fadeColor(colAccent, t)).Bold(true).Render(key) +
		lipgloss.NewStyle().Foreground(fadeColor(colFaint, t)).Render("   "+label)
}
