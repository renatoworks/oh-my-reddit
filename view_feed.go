package main

import (
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m model) renderHeader() string {
	// Three-column top bar on one line: wordmark pinned left, the subreddit +
	// thread title centered between, the username + LIVE/DEMO pinned right.
	// The Padding(0, 1) wrapper consumes one column on each side → fit m.width-2.
	inner := m.width - 2

	mode := "DEMO"
	if m.live {
		mode = "LIVE"
	}
	tag := headerBarStyle.Render(m.sp.View() + mode)
	left := wordmark()

	// Right column: hide the username on smaller terminals so the center keeps
	// room, and drop it entirely if the middle is still tight.
	showUser := m.username != "" && m.width >= 90
	right := func() string {
		if showUser {
			return lipgloss.NewStyle().Foreground(colMuted).Render("u/"+m.username) + "  " + tag
		}
		return tag
	}
	mid := inner - lipgloss.Width(left) - lipgloss.Width(right())
	if mid < 18 && showUser {
		showUser = false
		mid = inner - lipgloss.Width(left) - lipgloss.Width(right())
	}

	// Center column: subreddit + (truncated) title, centered in the middle gap.
	// Reserve a gap on each side so it never butts up against the side columns.
	const colGap = 3
	avail := mid - 2*colGap
	var center string
	if avail > 2 {
		subRaw := m.subLabel
		if lipgloss.Width(subtitleStyle.Render(subRaw)) > avail {
			subRaw = truncate(subRaw, avail)
		}
		c := subtitleStyle.Render(subRaw)
		// Cap the title so it clips on wide terminals instead of stretching across
		// the whole middle gap.
		if budget := min(72, avail-lipgloss.Width(c)-2); m.title != "" && budget >= 6 {
			c += "  " + bodyMutedStyle.Render(truncate(m.title, budget))
		}
		center = lipgloss.PlaceHorizontal(mid, lipgloss.Center, c)
	} else if mid > 0 {
		center = strings.Repeat(" ", mid)
	}

	// A bottom border (same line as the footer's top border) separates the
	// header from the streaming thread below.
	return lipgloss.NewStyle().
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(colBorder).
		Render(left + center + right())
}

// feedContent renders the feed and returns the line index where each comment
// block starts (for per-comment jump navigation).
func (m model) feedContent() (string, []int, int) {
	displayed := m.displayedComments()
	if len(displayed) == 0 {
		// Loading states get the braille spinner as a prefix (same as "loading
		// threads…"); a no-match filter is a settled state, so it stays still.
		var hint string
		switch {
		case strings.TrimSpace(m.feedQuery) != "":
			hint = bodyMutedStyle.Render("no comments match “" + m.feedQuery + "”")
		case m.live:
			hint = m.loaderLine("fetching the thread…")
		default:
			hint = m.loaderLine("waiting for comments…")
		}
		// Center the loader in the body so it sits in the middle of the screen,
		// between the (kept) thread header and the footer.
		return lipgloss.Place(m.vp.Width, m.vp.Height, lipgloss.Center, lipgloss.Center, hint), nil, 0
	}

	innerWidth := m.colW() - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	blocks := make([]string, 0, len(displayed))
	starts := make([]int, 0, len(displayed))
	line := 0
	for _, c := range displayed {
		b := m.renderComment(c, innerWidth)
		starts = append(starts, line)
		blocks = append(blocks, b)
		line += lipgloss.Height(b) + 2 // +2 for the two blank separator lines
	}
	total := line - 2 // last +2 was a separator that doesn't exist after the final block
	// Two blank lines between comments so each gets room to breathe.
	return feedStyle.Render(strings.Join(blocks, "\n\n\n")), starts, total
}

// renderComment fades a freshly released comment in (text brightens from faint
// to full over appearDuration) with a warm gutter that cools to faint over
// fadeDuration.
func (m model) renderComment(c comment, width int) string {
	if c.isSummary {
		return m.renderSummary(c, width)
	}

	gutterHex := string(colFaint)
	// Message takes the soft-bright text color so it leads the eye; the author
	// name sits in the accent red.
	textHex := string(colMessage)
	authorHex := string(colAccent)

	if !c.shownAt.IsZero() {
		el := float64(time.Since(c.shownAt))
		fadeT := el / float64(fadeDuration)
		appearT := el / float64(appearDuration)
		gutterHex = lerpHex(string(colAccent), string(colFaint), fadeT)
		textHex = lerpHex(string(colFaint), string(colMessage), appearT)
		authorHex = lerpHex(string(colFaint), string(colAccent), appearT)
	}
	// Emojis can't be tinted, so blank them out until the text has faded in (they
	// reveal once settled) rather than letting them sit bright on the dark canvas.
	settled := c.shownAt.IsZero() || time.Since(c.shownAt) >= appearDuration

	// The comment being read aloud gets a breathing background band that fades up
	// from 0 and back to 0 (see speakBandLevel), with a steady accent gutter and
	// full-brightness text.
	band, paint, widen := m.bandPainters(c.id, colBandHigh, width)
	if c.id != "" && c.id == m.speakingID {
		gutterHex = string(colAccent)
		textHex = string(colText)
	}

	age := agoString(time.Since(c.postedAt))
	meta := paint(metaStyle).Render("· " + age)
	if c.hasScore {
		score := c.score
		if v, ok := m.scores[c.id]; ok {
			score = v // latest score from the most recent poll
		}
		meta = paint(lipgloss.NewStyle().Foreground(colAccent)).Render(scoreText(score)) +
			paint(metaStyle).Render("  · "+age)
	}
	head := paint(lipgloss.NewStyle().Foreground(lipgloss.Color(authorHex)).Bold(true)).Render(c.author) +
		paint(lipgloss.NewStyle()).Render("  ") + meta

	lines := []string{widen(lipgloss.NewStyle()).Render(head)}
	// Reply reference: show who/what this comment is answering, if we have it.
	if strings.HasPrefix(c.parentID, "t1_") {
		if p, ok := m.byName[c.parentID]; ok {
			ref := "↳ " + p.author + ": " + truncate(p.body, 48)
			lines = append(lines, widen(lipgloss.NewStyle().Foreground(colFaint).Italic(true)).Render(hideEmoji(ref, settled)))
		}
	}
	lines = append(lines, widen(lipgloss.NewStyle().Foreground(lipgloss.Color(textHex))).Render(hideEmoji(c.body, settled)))

	block := lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "▌"}, false, false, false, true).
		BorderForeground(lipgloss.Color(gutterHex)).
		PaddingLeft(1)
	if band != nil {
		block = block.Background(band).BorderBackground(band)
	}
	return block.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderSummary draws the AI sentiment in the same style as a comment, but with
// a purple gutter and "live sentiment" label so it reads as a synthesis.
func (m model) renderSummary(c comment, width int) string {
	appearT := 1.0
	if !c.shownAt.IsZero() {
		appearT = float64(time.Since(c.shownAt)) / float64(appearDuration)
	}
	purpleHex := lerpHex(string(colFaint), string(colAccent2), appearT) // gutter + label stay purple
	textHex := lerpHex(string(colFaint), string(colBody), appearT)

	// Same breathing band as a narrated comment while this card is read aloud,
	// but in the AI's purple to match its identity.
	band, paint, widen := m.bandPainters(c.id, colBandHighA, width)
	if c.id != "" && c.id == m.speakingID {
		textHex = string(colBody) // full-brightness body while spoken
	}

	label := c.aiHeader
	if label == "" {
		label = "live sentiment"
	}
	head := paint(lipgloss.NewStyle().Foreground(lipgloss.Color(purpleHex)).Bold(true)).Render(label) +
		paint(metaStyle).Render("  · "+agoString(time.Since(c.postedAt)))
	body := widen(lipgloss.NewStyle().Foreground(lipgloss.Color(textHex))).Render(c.body)

	block := lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "▌"}, false, false, false, true).
		BorderForeground(lipgloss.Color(purpleHex)).
		PaddingLeft(1)
	if band != nil {
		block = block.Background(band).BorderBackground(band)
	}
	return block.Render(lipgloss.JoinVertical(lipgloss.Left, widen(lipgloss.NewStyle()).Render(head), body))
}

// bandPainters returns the breathing-band color for id (using peak when it's the
// active read) plus paint/widen helpers shared by comments and AI cards. The band
// must be painted on every segment, and widen pads each line to the content width
// in the band color so JoinVertical doesn't leave it ragged with unstyled spaces.
func (m model) bandPainters(id string, peak lipgloss.Color, width int) (
	band lipgloss.TerminalColor,
	paint func(lipgloss.Style) lipgloss.Style,
	widen func(lipgloss.Style) lipgloss.Style,
) {
	band = m.speakBand(id, peak)
	paint = func(s lipgloss.Style) lipgloss.Style {
		if band != nil {
			return s.Background(band)
		}
		return s
	}
	contentW := width - 2 // left border (1) + PaddingLeft (1)
	if contentW < 1 {
		contentW = 1
	}
	widen = func(s lipgloss.Style) lipgloss.Style { return paint(s).Width(contentW) }
	return band, paint, widen
}

// --- OP screen -------------------------------------------------------------

// opDims derives the post reader's inner content width and the body viewport's
// height from the (full) terminal size. Kept in one place so sizing and
// rendering agree.
func (m model) opDims() (contentW, bodyH int) {
	contentW = max(10, m.colW()-6)    // column width minus rounded border (2) + padding (4)
	contentRows := max(3, m.height-4) // full height minus border (2) + vertical padding (2)
	// Non-body rows: header + a blank, then a blank + footer.
	chrome := lipgloss.Height(m.opHeader(contentW)) + 3
	bodyH = max(3, contentRows-chrome)
	return contentW, bodyH
}

// syncOPModal sizes the body viewport and loads the current OP text. Called on
// open, on resize, and when a poll refreshes the OP.
func (m *model) syncOPModal() {
	if m.op == nil {
		return
	}
	contentW, bodyH := m.opDims()
	m.opVP.Width = contentW
	m.opVP.Height = bodyH
	// glamour's width tables undercount newer emoji (1 vs the terminal's 2), so a
	// line with emoji renders one column too wide and spills past the panel
	// border. Squaring each line to the exact width contains that spill.
	m.opVP.SetContent(fitWidth(m.opBody(contentW), contentW))
}

// fitWidth forces every line to exactly width display columns (ANSI-aware):
// short lines are padded, lines that overflow (e.g. emoji glamour under-measured)
// are trimmed back, so nothing punches through the panel border.
func fitWidth(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		switch w := ansi.StringWidth(ln); {
		case w > width:
			lines[i] = ansi.Truncate(ln, width, "")
		case w < width:
			lines[i] = ln + strings.Repeat(" ", width-w)
		}
	}
	return strings.Join(lines, "\n")
}

// scoreText formats a vote score with an up/down arrow, e.g. "▲ 42" or "▼ 3".
func scoreText(score int) string {
	arrow, n := "▲", score
	if n < 0 {
		arrow, n = "▼", -n
	}
	return arrow + " " + strconv.Itoa(n)
}

// opHeader is the reader's title + meta line (author, live score, age).
func (m model) opHeader(width int) string {
	p := m.op
	title := lipgloss.NewStyle().Foreground(colText).Bold(true).Width(width).Render(p.title)
	meta := metaStyle.Render("u/" + p.author)
	if p.hasScore {
		meta += dotStyle + scoreStyle.Render(scoreText(p.score))
	}
	meta += dotStyle + metaStyle.Render(agoString(time.Since(p.postedAt)))
	return lipgloss.JoinVertical(lipgloss.Left, title, meta)
}

// hideEmoji blanks emoji clusters (with equal-width spaces) while shown is false.
// A terminal can't tint an emoji glyph, so during a fade-in we hide them rather
// than let them sit at full brightness on the dark canvas; they reveal once the
// text has settled. Text arrows (↑ ↓ → ↳) aren't emoji, so they fade normally.
func hideEmoji(s string, shown bool) string {
	if shown || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if isEmojiCluster(g.Runes()) {
			b.WriteString(strings.Repeat(" ", g.Width()))
		} else {
			b.WriteString(g.Str())
		}
	}
	return b.String()
}

// cacheOPImage rebuilds the cached half-block render of the OP image at the
// current modal width, so a poll-driven re-sync doesn't rebuild it every time.
func (m *model) cacheOPImage() {
	if m.opImage == nil {
		m.opImageRender, m.opImageRenderW = "", 0
		return
	}
	contentW, _ := m.opDims()
	m.opImageRender = renderImageBlocks(m.opImage, contentW, opImageMaxRows)
	m.opImageRenderW = contentW
}

// opBody is the scrollable content: the rendered self-text, the link, or both.
func (m model) opBody(width int) string {
	p := m.op
	// The URL is rendered as plain styled text: with the mouse uncaptured, the
	// terminal auto-detects it and makes it clickable on its own (the same way it
	// already handles URLs inside comment bodies), with the underline/click scoped
	// to just the URL instead of bleeding across the whole padded row.
	link := lipgloss.NewStyle().Foreground(colAccent2).Width(width).Render("→ " + p.link)
	switch {
	case p.body != "":
		body := renderMarkdown(p.body, width)
		if p.link != "" {
			body += "\n\n" + link
		}
		return body
	case isImageURL(p.link):
		// Image post: the picture as half-blocks above the source link, or a
		// loading/placeholder line until the fetch lands. The render is cached
		// (opImageRender) so a poll-driven re-sync doesn't rebuild it every time.
		switch {
		case m.opImage != nil:
			img := m.opImageRender
			if img == "" || m.opImageRenderW != width {
				img = renderImageBlocks(m.opImage, width, opImageMaxRows) // cache miss (e.g. mid-resize)
			}
			return img + "\n\n" + link
		case m.opImageLoading:
			return bodyMutedStyle.Render("loading image…") + "\n\n" + link
		default:
			return link
		}
	case p.link != "":
		return link
	default:
		return bodyMutedStyle.Render("(no text — link or media post)")
	}
}

// renderMarkdown turns reddit's markdown self-text into styled terminal output
// (headers, tables, emphasis, links) via glamour, wrapped to width. Falls back
// to the raw source if rendering fails.
func renderMarkdown(src string, width int) string {
	// Reserve a 2-cell slot per emoji so glamour's table/wrap math is exact
	// (it otherwise under-measures emoji), then drop the real glyph back in.
	src, picked := reserveEmoji(src)
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return restoreEmoji(src, picked)
	}
	out, err := r.Render(src)
	if err != nil {
		return restoreEmoji(src, picked)
	}
	return restoreEmoji(strings.Trim(out, "\n"), picked)
}

// renderOPScreen draws the OP as a full-screen bordered panel that replaces the
// feed while open. Full width gives wide match-thread tables room to breathe;
// the body scrolls inside its viewport.
func (m model) renderOPScreen() string {
	contentW, _ := m.opDims()
	inner := lipgloss.JoinVertical(lipgloss.Left,
		m.opHeader(contentW),
		"",
		m.opVP.View(),
		"",
		metaStyle.Render("↑↓ scroll · esc / t / q close"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Width(m.colW() - 2).  // border adds 1 col each side → fills the column
		Height(m.height - 2). // border adds 1 row each side
		Render(inner)
}

func (m model) renderStatus() string {
	// The status bar spans the full terminal width (not the capped content
	// column), so size its tiers off m.width — colW() maxes out at 88, which
	// would make the wide tier (sparkline + full help) unreachable.
	wide := m.width >= 96 // room for the sparkline + full help
	mid := m.width >= 68  // room for "updated", queued, and condensed help

	updated := "—"
	if !m.updatedAt.IsZero() {
		updated = agoString(time.Since(m.updatedAt))
	}

	// Default left: metrics, shedding detail as the window narrows.
	left := statusKeyStyle.Render(strconv.Itoa(len(m.comments))) + " shown" +
		dotStyle + statusKeyStyle.Render(strconv.Itoa(m.velocity())) + "/min"
	if wide {
		left += "  " + lipgloss.NewStyle().Foreground(colMuted).Render(m.activitySpark())
	}
	if mid {
		left += dotStyle + "updated " + statusKeyStyle.Render(updated)
	}
	// Reserve a fixed-width slot for the "N queued" indicator so the bar doesn't
	// reflow when it appears/disappears — the space is held even when it's blank.
	queued := ""
	if n := len(m.pending); n > 0 {
		queued = dotStyle + lipgloss.NewStyle().Foreground(colAccent).Render(strconv.Itoa(n)+" queued")
	}
	left += lipgloss.NewStyle().Width(lipgloss.Width(dotStyle) + lipgloss.Width("999 queued")).Render(queued)
	if m.paused {
		left += dotStyle + lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("paused")
	}

	// Active modes replace the metrics with their own (short) prompt.
	switch {
	case m.feedMode == feedSearching:
		left = lipgloss.NewStyle().Foreground(colAccent).Render("search: "+m.feedQuery+"▏") +
			metaStyle.Render("  enter apply · esc clear")
	case m.feedMode == feedAsking:
		left = lipgloss.NewStyle().Foreground(colAccent2).Render("ask: "+m.askQuery+"▏") +
			metaStyle.Render("  enter ask · esc cancel")
	case strings.TrimSpace(m.feedQuery) != "":
		left = lipgloss.NewStyle().Foreground(colAccent).Render("filter: "+m.feedQuery) +
			metaStyle.Render("  "+strconv.Itoa(len(m.displayedComments()))+" matches")
	}
	if m.summarizing {
		// Same braille spinner as the rest of the app, tinted AI-purple.
		frame := spinner.Dot.Frames[m.tick%len(spinner.Dot.Frames)]
		left = lipgloss.NewStyle().Foreground(colAccent2).Render(frame + m.aiLoadLabel)
	}
	if m.err != nil {
		left = lipgloss.NewStyle().Foreground(colAccent).Render("error: " + m.err.Error())
	}

	playPause := "pause"
	if m.paused {
		playPause = "play"
	}
	// Progressive disclosure: help segments in display order, each with a drop
	// priority (lower drops first as the bar narrows). `esc back` never drops;
	// `t post` and `/ find` stay longest, then the AI keys, then open/refresh/
	// pause/voice shed first. Voice only shows where `say` works (macOS); the AI
	// keys only when a key is set.
	type hseg struct {
		text string
		prio int
	}
	segs := []hseg{{"t post", 8}}
	if m.aiEnabled {
		segs = append(segs, hseg{"s summary", 5}, hseg{"a ask", 4})
	}
	segs = append(segs, hseg{"/ find", 6}, hseg{"o open", 3}, hseg{"r refresh", 2}, hseg{"p " + playPause, 1})
	if runtime.GOOS == "darwin" {
		segs = append(segs, hseg{"v voice " + onOff(m.voice), 0})
	}
	segs = append(segs, hseg{"esc back", 1 << 30}) // essential — never dropped

	build := func() string {
		parts := make([]string, len(segs))
		for i, s := range segs {
			parts[i] = s.text
		}
		return strings.Join(parts, " · ")
	}
	// Shed the lowest-priority segment until the help fits beside the metrics.
	avail := m.width - 4 - lipgloss.Width(left) // -2 bar padding, -2 minimum gap
	for len(segs) > 1 && lipgloss.Width(metaStyle.Render(build())) > avail {
		lo := 0
		for i := 1; i < len(segs); i++ {
			if segs[i].prio < segs[lo].prio {
				lo = i
			}
		}
		segs = append(segs[:lo], segs[lo+1:]...)
	}
	return m.footerBar(left, build())
}
