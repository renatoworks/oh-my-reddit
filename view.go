package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// --- views -----------------------------------------------------------------

func (m model) View() string {
	var v string
	switch m.screen {
	case screenAuth:
		v = m.renderAuth()
	case screenInput:
		v = m.renderInput()
	case screenList:
		v = m.renderList()
	default:
		switch {
		case !m.ready:
			v = "\n  starting oh-my-reddit…"
		case m.opOpen && m.op != nil:
			v = m.center(m.renderOPScreen()) // full-screen post reader; feed keeps streaming behind it
		default:
			// Full-width header + footer bars; the comment column is centered
			// between, with a blank line padding it off each bar.
			v = lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), "", m.center(m.vp.View()), "", m.renderStatus())
		}
	}
	// Pad to the full terminal height so no row is left unpainted on resize. The
	// dark canvas itself comes from the OSC 11 terminal background set in main(),
	// which fills every cell uniformly (a wrapped lipgloss background goes ragged
	// where inner styles reset it).
	return m.fillHeight(v)
}

func wordmark() string { return gradientText("oh-my-reddit", "#ff6a3d", "#b14eff") }

// maxContentWidth caps the reading column; on wider terminals the content is
// centered with margins so lines stay short enough to scan.
const maxContentWidth = 88

// colW is the width of the content column: the full terminal up to the cap.
func (m model) colW() int {
	return min(m.width, maxContentWidth)
}

// center pads a column-width block with equal left/right margin so it sits in
// the middle of the terminal. A no-op once the terminal is narrower than the cap.
func (m model) center(s string) string {
	left := (m.width - m.colW()) / 2
	if left < 1 {
		return s
	}
	indent := strings.Repeat(" ", left)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		// Pad the right too, so every line is exactly the terminal width (flush to
		// the edge, no ragged right margin / stray cells).
		right := max(0, m.width-left-lipgloss.Width(l))
		lines[i] = indent + l + strings.Repeat(" ", right)
	}
	return strings.Join(lines, "\n")
}

// loaderLine renders a spinner + message that wraps within the content column,
// so a long message breaks onto a second line on a narrow terminal instead of
// running off the edge.
func (m model) loaderLine(text string) string {
	sp := m.sp.View()
	avail := max(16, m.colW()-lipgloss.Width(sp)-3)
	styled := bodyMutedStyle.Render(text)
	if lipgloss.Width(styled) > avail {
		styled = bodyMutedStyle.Width(avail).Render(text) // wrap only when it won't fit
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sp+" ", styled)
}

// taglineFor returns the tagline in the given color (for the entrance fade),
// wrapped onto two lines at the comma when it doesn't fit the given width.
func taglineFor(width int, col lipgloss.Color) string {
	const full = "beautiful reddit threads, live in your terminal"
	st := lipgloss.NewStyle().Foreground(col).Italic(true)
	if lipgloss.Width(full) <= width {
		return st.Render(full)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		st.Render("beautiful reddit threads,"),
		st.Render("live in your terminal"),
	)
}

// signedInChip renders the "signed in as u/name · " prefix shown in footers, or
// "" when not signed in. Matches the input screen's footer styling.
func (m model) signedInChip() string {
	if m.username == "" {
		return ""
	}
	return metaStyle.Render("signed in as ") +
		lipgloss.NewStyle().Foreground(colMuted).Render("u/"+m.username) +
		metaStyle.Render(" · ")
}

// footerBarCentered is the bottom bar with its help centered — used on the
// pages without a fixed header (connect, input, list). The live feed keeps the
// left-metrics / right-help footerBar instead. help must already be styled:
// re-applying a style here would be cancelled by any inner reset (e.g. the
// signed-in chip), leaving the trailing text unstyled (white).
func (m model) footerBarCentered(help string) string {
	w := m.width
	inner := lipgloss.PlaceHorizontal(w-2, lipgloss.Center, help)
	return statusStyle.MaxWidth(w).Width(w).Render(inner)
}

// repoModule is the path users run `go install <path>@latest` to update.
const repoModule = "github.com/renatoworks/oh-my-reddit"

// updateNotice renders the "a newer version is available" line, centered across
// the full width, or "" when no newer release is known (the background check
// sets m.updateLatest only when one exists).
func (m model) updateNotice() string {
	latest := m.updateLatest
	if latest == "" {
		return ""
	}
	// Two centered lines: the version on top, the install command below, so the
	// long command gets its own row instead of overflowing. The command shortens
	// to a `…@latest` form if even a full row can't hold it (very narrow window).
	top := metaStyle.Render("update available · ") +
		lipgloss.NewStyle().Foreground(colAccent).Render(latest)
	cmd := "go install " + repoModule + "@latest"
	if lipgloss.Width(cmd) > m.width-2 {
		cmd = "go install …@latest"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.PlaceHorizontal(m.width, lipgloss.Center, top),
		lipgloss.PlaceHorizontal(m.width, lipgloss.Center, bodyMutedStyle.Render(cmd)),
	)
}

// footerWithNotice pins the centered update line (when present) near the bottom,
// with a blank line of breathing room below it — above the footer bar if there
// is one, otherwise above the screen edge.
func (m model) footerWithNotice(footer string) string {
	n := m.updateNotice()
	if n == "" {
		return footer
	}
	if footer == "" {
		return lipgloss.JoinVertical(lipgloss.Left, n, "")
	}
	return lipgloss.JoinVertical(lipgloss.Left, n, "", footer)
}

// centerPage centers a content block horizontally and vertically in the space
// above a bottom-pinned footer: padded above when short (so it floats in the
// middle), top-aligned and clipped when tall (so the footer is never lost).
func (m model) centerPage(block, footer string) string {
	area := m.height - lipgloss.Height(footer)
	if area < 1 {
		area = 1
	}
	lines := strings.Split(lipgloss.PlaceHorizontal(m.width, lipgloss.Center, block), "\n")
	if len(lines) >= area {
		lines = lines[:area]
	} else {
		top := (area - len(lines)) / 2
		lines = append(make([]string, top), lines...)
		for len(lines) < area {
			lines = append(lines, "")
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), footer)
}

// footerBar draws the shared bottom bar (top border + padding) at the column
// width: pre-rendered left content, then raw help right-aligned, dropping the
// help if it won't fit. Used by the live feed so the chrome is consistent.
func (m model) footerBar(left, help string) string {
	w := m.width   // full width, like the header bar — content between is centered
	avail := w - 2 // statusStyle pads 1 col each side
	h := metaStyle.Render(help)
	if lipgloss.Width(left)+lipgloss.Width(h) > avail {
		h = "" // doesn't fit — drop the help rather than wrap
	}
	gap := avail - lipgloss.Width(left) - lipgloss.Width(h)
	if gap < 1 {
		gap = 1
	}
	return statusStyle.MaxWidth(w).Width(w).Render(left + strings.Repeat(" ", gap) + h)
}

// fillHeight forces a view to be exactly the terminal height: it pads short
// views with blank lines (so no row is left unpainted and old content can't
// ghost through on resize) and clips tall ones (so content can't overflow and
// scroll the screen). A no-op for views that already fill the height.
func (m model) fillHeight(s string) string {
	if m.height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	// Hard-truncate every line to the terminal width. A line wider than m.width
	// is soft-wrapped by the terminal onto an extra physical row, which throws
	// off Bubble Tea's renderer (it counts one row per \n) and leaves duplicated
	// or ghosted rows when scrolling/loading fast. Clamping here guarantees one
	// logical line == one screen row regardless of any upstream sizing slip.
	if m.width > 0 {
		for i, line := range lines {
			if lipgloss.Width(line) > m.width {
				lines[i] = ansi.Truncate(line, m.width, "")
			}
		}
	}
	switch {
	case len(lines) > m.height:
		return strings.Join(lines[:m.height], "\n")
	case len(lines) < m.height:
		return strings.Join(lines, "\n") + strings.Repeat("\n", m.height-len(lines))
	}
	return strings.Join(lines, "\n")
}
