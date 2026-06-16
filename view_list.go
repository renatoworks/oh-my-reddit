package main

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

func (m model) renderList() string {
	// Loading and error are minimal: just the message, centered, with a back
	// hint — no wordmark/header to throw the centering off.
	switch {
	case m.err != nil && len(m.threads) == 0:
		// Initial load failed with nothing to show — full-page message. A failed
		// re-sort keeps the list (handled in the header below) instead of this.
		return m.centerPage(lipgloss.NewStyle().Foreground(colAccent).Render(m.err.Error()),
			m.footerBarCentered(metaStyle.Render("esc back · ctrl+c quit")))
	case m.loading:
		loader := m.loaderLine("loading r/" + m.subreddit + "…")
		return m.centerPage(loader, m.footerBarCentered(metaStyle.Render("esc back · ctrl+c quit")))
	case len(m.threads) == 0:
		// Loaded, but nothing came back — show it instead of spinning forever.
		empty := bodyMutedStyle.Render("no threads in r/" + m.subreddit)
		return m.centerPage(empty, m.footerBarCentered(metaStyle.Render("esc back · ctrl+c quit")))
	}

	// Status line under the title: a failed re-sort note, an active filter, or
	// the resting hint. The active sort lives in the tab strip below, and a
	// re-sort in flight takes over the list area with a centered loader.
	var status string
	switch {
	case m.err != nil:
		status = lipgloss.NewStyle().Foreground(colAccent).Render("couldn't load that sort — showing previous")
	case m.filter != "":
		status = lipgloss.NewStyle().Foreground(colAccent).Render("filter: "+m.filter) +
			metaStyle.Render("▏  "+strconv.Itoa(len(m.results))+" matches")
	default:
		status = bodyMutedStyle.Render("pick a thread") + metaStyle.Render("  ·  type to filter")
	}
	head := subtitleStyle.Render("r/"+m.subreddit) + dotStyle + status

	// While a re-sort is in flight, replace the rows with a centered "sorting
	// by …" loader sized to the outgoing list, so the page doesn't jump.
	listArea := m.renderThreadList()
	if m.resorting {
		frame := spinner.Dot.Frames[m.tick%len(spinner.Dot.Frames)]
		msg := lipgloss.NewStyle().Foreground(colAccent).Render(frame) +
			lipgloss.NewStyle().Foreground(colMuted).Render(" sorting by "+string(m.listSort)+"…")
		listW := max(20, m.colW()-4)
		listArea = lipgloss.Place(listW, lipgloss.Height(listArea), lipgloss.Center, lipgloss.Center, msg)
	}

	block := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, wordmark(), head, "", m.renderSortTabs(), "", listArea),
	)
	footer := m.footerBarCentered(m.signedInChip() + metaStyle.Render("↑/↓ move · ←/→ sort · enter open · / filter · esc back · ctrl+c quit"))
	return m.centerPage(block, footer)
}

// renderSortTabs draws the listing-order tabs (hot/new/rising/top) as a
// segmented control, the active sort highlighted. ←/→ moves the highlight.
func (m model) renderSortTabs() string {
	active := lipgloss.NewStyle().Foreground(colAccent).Background(colSelBg).Bold(true).Padding(0, 1)
	idle := lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)
	tabs := make([]string, len(listSorts))
	for i, s := range listSorts {
		if s == m.listSort {
			tabs[i] = active.Render(string(s))
		} else {
			tabs[i] = idle.Render(string(s))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

// listCap is how many thread rows fit, leaving room for chrome.
func (m model) listCap() int {
	c := m.height - 13 // top bar + section labels + footer chrome
	if c < 4 {
		c = 4
	}
	return c
}

// renderThreadList groups stickied posts under "highlights" and the rest under
// the active sort (hot/new/rising/top), scrolls a window around the cursor, and
// labels the count column.
func (m model) renderThreadList() string {
	listW := max(20, m.colW()-4)
	// Each visible row staggers in (content-relative position; only the visible
	// window is rendered, so the cascade stays bounded).
	rowFade := func(pos int) float64 { return m.fade(pos) }
	header := lipgloss.NewStyle().Width(listW).Align(lipgloss.Right).Foreground(fadeColor(colFaint, m.fade(0))).Render("comments")

	results := m.results
	if len(results) == 0 {
		return header + "\n\n" + bodyMutedStyle.Render("  no threads match “"+m.filter+"”")
	}

	start := m.listOffset
	if start < 0 {
		start = 0
	}
	end := start + m.listCap()
	if end > len(results) {
		end = len(results)
	}

	// Sections only make sense for the unfiltered list.
	sectioned := strings.TrimSpace(m.filter) == ""
	firstPinned, firstHot := -1, -1
	if sectioned {
		for i, t := range results {
			if t.stickied && firstPinned == -1 {
				firstPinned = i
			}
			if !t.stickied && firstHot == -1 {
				firstHot = i
			}
		}
	}

	lines := []string{header}
	if start > 0 {
		lines = append(lines, metaStyle.Render("  ↑ "+strconv.Itoa(start)+" more"))
	}
	label := func(s string, pos int) string {
		return lipgloss.NewStyle().Foreground(fadeColor(colFaint, rowFade(pos))).Bold(true).Render(strings.ToUpper(s))
	}
	for i := start; i < end; i++ {
		pos := i - start
		if sectioned {
			if i == firstPinned {
				lines = append(lines, label("highlights", pos))
			}
			if i == firstHot {
				if firstPinned != -1 && firstPinned >= start {
					lines = append(lines, "") // separate highlights from the rest
				}
				lines = append(lines, label(string(m.listSort), pos)) // HOT / NEW / RISING / TOP
			}
		}
		lines = append(lines, m.renderThreadRow(i, results[i], rowFade(pos)))
	}
	if end < len(results) {
		lines = append(lines, metaStyle.Render("  ↓ "+strconv.Itoa(len(results)-end)+" more"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderThreadRow(i int, t thread, fade float64) string {
	selected := i == m.cursor
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(colSelBg)
		}
		return s
	}

	listW := max(20, m.colW()-4)
	countW := 6
	titleW := listW - 4 - countW
	if titleW < 10 {
		titleW = 10
	}

	titleFg := colText
	if t.stickied {
		titleFg = colAccent
	}
	titleStyle := bg(lipgloss.NewStyle().Foreground(fadeColor(titleFg, fade)).Width(titleW))
	if selected {
		titleStyle = titleStyle.Bold(true)
	}

	countStr := ""
	if t.numComments > 0 {
		countStr = formatCount(t.numComments)
	}
	countStyle := bg(lipgloss.NewStyle().Foreground(fadeColor(colMuted, fade)).Width(countW).Align(lipgloss.Right))

	bar := "  "
	if selected {
		bar = "▌ "
	}

	return bg(lipgloss.NewStyle().Foreground(fadeColor(colAccent, fade)).Width(2)).Render(bar) +
		titleStyle.Render(hideEmoji(truncate(t.title, titleW), fade >= 1)) +
		bg(lipgloss.NewStyle().Width(2)).Render("  ") +
		countStyle.Render(countStr)
}

// formatCount shortens large comment counts: 1031 -> "1.0k".
func formatCount(n int) string {
	if n >= 1000 {
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(n)
}
