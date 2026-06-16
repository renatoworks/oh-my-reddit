package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- transitions -----------------------------------------------------------

func (m *model) startFeed(url, title, sub string, live bool) {
	m.screen = screenFeed
	m.url = url
	m.title = title
	m.subLabel = sub // display only; m.subreddit stays the canonical bare name
	m.live = live
	m.narrateGen++ // invalidate any narration loop from a previous thread
	m.narratedID = ""
	m.clearSpeakBand()
	m.interval = demoPollInterval
	if live {
		m.interval = livePollInterval
	}
	m.op = nil
	m.opOpen = false
	m.opImage, m.opImageURL, m.opImageLoading = nil, "", false
	m.opImageRender, m.opImageRenderW = "", 0
	m.comments = nil
	m.pending = nil
	m.seen = map[string]bool{}
	m.byName = map[string]comment{}
	m.scores = map[string]int{}
	m.demoIndex = 0
	m.demoBeat = 0
	m.updatedAt = time.Time{}
	m.err = nil
	m.paused = false
	m.feedMode = feedIdle
	m.feedQuery = ""
	m.askQuery = ""
	m.summarizing = false
	m.loopGen++ // invalidate any timers from a previous thread
	if !live {
		m.op = demoPost(title)                       // so the OP modal is testable offline
		m.enqueue(m.demoEmit(demoChatter, 4, false)) // open on some pre-match chatter
	}
	if m.ready {
		m.refreshFeed()
	}
}

// pollReleaseCmds (re)starts the poll + release timers for the current loop
// generation, plus a live poll when the feed is live. Callers tag m.loopGen
// first and append their own extras (narration, demo chatter).
func (m model) pollReleaseCmds() []tea.Cmd {
	cmds := []tea.Cmd{pollEvery(m.interval, m.loopGen), releaseEvery(releaseFast, m.loopGen)}
	if m.live {
		cmds = append(cmds, pollCmd(m.url))
	}
	return cmds
}

func (m model) feedCmds() tea.Cmd {
	cmds := m.pollReleaseCmds()
	if c := m.narrateStart(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// setFeedQuery updates the in-thread filter and repaints, scrolling to the
// bottom so the newest matches stay in view.
func (m *model) setFeedQuery(q string) {
	m.feedQuery = q
	m.refreshFeed()
	m.vp.GotoBottom()
}

// appendAICard appends a synthesized AI card (sentiment or answer), anchors the
// speaking band on it, scrolls to it, and returns the cmd that reads it aloud
// then resumes narration. summaryMsg and answerMsg differ only by header text.
func (m *model) appendAICard(header, text string) tea.Cmd {
	m.summarizing = false
	now := time.Now()
	id := fmt.Sprintf("ai-%d", now.UnixNano())
	m.comments = append(m.comments, comment{
		id:        id,
		isSummary: true,
		aiHeader:  header,
		body:      text,
		postedAt:  now,
		shownAt:   now,
	})
	m.startSpeaking(id, now)
	m.refreshFeed()
	m.vp.GotoBottom()
	return m.aiSpeakCmd(text)
}

// enqueue adds only unseen comments to the pending queue. The first batch is
// capped so launch fills quickly rather than trickling for minutes.
func (m *model) enqueue(cs []comment) {
	fresh := make([]comment, 0, len(cs))
	for _, c := range cs {
		m.byName[c.id] = c // record for reply-parent lookups, even if already shown
		if c.hasScore {
			m.scores[c.id] = c.score // refresh score live, even for already-shown comments
		}
		if m.seen[c.id] {
			continue
		}
		m.seen[c.id] = true
		fresh = append(fresh, c)
	}
	if len(m.comments) == 0 && len(m.pending) == 0 && len(fresh) > initialCap {
		fresh = fresh[len(fresh)-initialCap:]
	}
	m.pending = append(m.pending, fresh...)
}

// releaseOne moves a single pending comment into the visible feed.
func (m *model) releaseOne() bool {
	if len(m.pending) == 0 {
		return false
	}
	c := m.pending[0]
	m.pending = m.pending[1:]
	c.shownAt = time.Now()
	m.comments = append(m.comments, c)
	m.updatedAt = c.shownAt
	m.refreshFeed()
	return true
}

// releaseDelay drains a backlog fast but trickles gracefully when caught up.
func (m model) releaseDelay() time.Duration {
	if len(m.pending) > 8 {
		return releaseFast
	}
	return releaseSlow
}

// anyFading reports whether the newest comment is still animating, so we only
// repaint on spinner ticks while something is actually moving.
func (m model) anyFading() bool {
	if n := len(m.comments); n > 0 {
		return time.Since(m.comments[n-1].shownAt) < fadeDuration
	}
	return false
}

func (m *model) refreshFeed() {
	if !m.ready {
		return
	}
	content, starts, total := m.feedContent()
	m.commentStarts = starts
	m.feedLines = total
	wasBottom := m.vp.AtBottom()
	m.vp.SetContent(content)
	if wasBottom {
		m.vp.GotoBottom()
	}
}

// feedJump steps to the previous/next comment, scrolling so that comment's
// LAST line sits on the bottom row of the viewport.
func (m *model) feedJump(dir int) {
	n := len(m.commentStarts)
	if !m.ready || n == 0 {
		return
	}
	h := m.vp.Height
	if h < 1 {
		h = 1
	}
	// end(i) is the last own line of comment i (excluding the blank separator).
	end := func(i int) int {
		if i < n-1 {
			return m.commentStarts[i+1] - 2
		}
		return m.feedLines - 1
	}

	// Current comment = the lowest one whose end is at/above the bottom row.
	bottom := m.vp.YOffset + h - 1
	cur := 0
	for i := 0; i < n; i++ {
		if end(i) <= bottom {
			cur = i
		} else {
			break
		}
	}

	target := cur + dir
	if target < 0 {
		target = 0
	}
	if target > n-1 {
		target = n - 1
	}
	desired := end(target) - (h - 1) // put that comment's last line on the bottom row
	if desired < 0 {
		desired = 0
	}
	m.vp.SetYOffset(desired)
}

// --- recents ---------------------------------------------------------------

func (m *model) recordSub(sub string) {
	m.recentSubs = pushSub(m.recentSubs, sub)
	saveRecents(m.recentSubs, m.recentThreads)
}

func (m *model) recordThread(sub, url, title string) {
	if url == "" {
		return
	}
	m.recentThreads = pushThread(m.recentThreads, recentThread{Sub: sub, URL: url, Title: title})
	saveRecents(m.recentSubs, m.recentThreads)
}

// inputItem is one selectable recent on the input screen.
type inputItem struct {
	isSub bool
	sub   string
	url   string
	title string
}

func (m model) inputItems() []inputItem {
	items := make([]inputItem, 0, len(m.recentSubs)+len(m.recentThreads))
	for _, s := range m.recentSubs {
		items = append(items, inputItem{isSub: true, sub: s})
	}
	for _, t := range m.recentThreads {
		items = append(items, inputItem{sub: t.Sub, url: t.URL, title: t.Title})
	}
	return items
}

// opContext returns the OP's self-text (and link) to ground the AI, or "" if
// there's no fetched OP (demo without one, RSS-only thread, link post).
func (m model) opContext() string {
	if m.op == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if b := strings.TrimSpace(m.op.body); b != "" {
		parts = append(parts, b)
	}
	if m.op.link != "" {
		parts = append(parts, "Link: "+m.op.link)
	}
	return strings.Join(parts, "\n")
}

// commentsSinceLastSummary returns the bodies posted after the most recent
// sentiment card (or all of them if there's none yet), capped at 60.
func (m model) commentsSinceLastSummary() []string {
	start := 0
	for i := len(m.comments) - 1; i >= 0; i-- {
		if m.comments[i].isSummary {
			start = i + 1
			break
		}
	}
	bodies := bodiesOf(m.comments[start:])
	if len(bodies) > 60 {
		bodies = bodies[len(bodies)-60:]
	}
	return bodies
}

// bodiesOf returns the bodies of the real comments in cs (AI cards excluded).
func bodiesOf(cs []comment) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if !c.isSummary {
			out = append(out, c.body)
		}
	}
	return out
}

// windowStart is the index of the last-n window of comments (a fixed-size tail),
// distinct from commentsSinceLastSummary which starts at the most recent AI card.
func (m model) windowStart(n int) int {
	if len(m.comments) > n {
		return len(m.comments) - n
	}
	return 0
}

// recentBodies returns the last n real comment bodies (AI cards excluded).
func (m model) recentBodies(n int) []string {
	return bodiesOf(m.comments[m.windowStart(n):])
}

// recentAskedQA returns the user's earlier questions (and answers) still within
// the recent window, for follow-up context. Older ones age out naturally as new
// comments push them past the window.
func (m model) recentAskedQA(n int) []string {
	start := m.windowStart(n)
	out := []string{}
	for _, c := range m.comments[start:] {
		if c.isSummary && strings.HasPrefix(c.aiHeader, "asked: ") {
			q := strings.TrimPrefix(c.aiHeader, "asked: ")
			out = append(out, "you asked: \""+q+"\" → I answered: \""+c.body+"\"")
		}
	}
	return out
}

// displayedComments applies the in-thread search filter.
func (m model) displayedComments() []comment {
	if strings.TrimSpace(m.feedQuery) == "" {
		return m.comments
	}
	q := strings.ToLower(strings.TrimSpace(m.feedQuery))
	out := make([]comment, 0, len(m.comments))
	for _, c := range m.comments {
		if strings.Contains(strings.ToLower(c.author), q) || strings.Contains(strings.ToLower(c.body), q) {
			out = append(out, c)
		}
	}
	return out
}

// recomputeResults applies the current fuzzy filter to the thread list.
func (m *model) recomputeResults() {
	if strings.TrimSpace(m.filter) == "" {
		m.results = m.threads
		return
	}
	m.results = fuzzyFilter(m.threads, m.filter)
}

// cycleListSort advances the listing order by dir (+1/-1), wrapping around.
func (m *model) cycleListSort(dir int) {
	i := 0
	for j, s := range listSorts {
		if s == m.listSort {
			i = j
			break
		}
	}
	n := len(listSorts)
	m.listSort = listSorts[((i+dir)%n+n)%n]
}

func (m *model) clampCursor() {
	if m.cursor >= len(m.results) {
		m.cursor = len(m.results) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+m.listCap() {
		m.listOffset = m.cursor - m.listCap() + 1
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

// activitySpark renders a sparkline of comments-per-bucket using ABSOLUTE time
// buckets, so each bar is a fixed 30s wall-clock slice that never shifts — past
// bars stay put; only the rightmost (current) bar fills, and the window steps
// left once every 30s.
func (m model) activitySpark() string {
	const buckets = 14
	const bucketSec = 30
	nowB := time.Now().Unix() / bucketSec // current absolute bucket index
	counts := make([]int, buckets)
	for _, c := range m.comments {
		if c.isSummary {
			continue
		}
		idx := buckets - 1 - int(nowB-c.postedAt.Unix()/bucketSec) // current bucket at right
		if idx >= 0 && idx < buckets {
			counts[idx]++
		}
	}

	// Scale to the busiest COMPLETED bucket so the still-filling current bucket
	// doesn't retroactively shrink the past. Fall back to full max early on.
	maxC := 0
	for i := 0; i < buckets-1; i++ {
		if counts[i] > maxC {
			maxC = counts[i]
		}
	}
	if maxC == 0 {
		for _, c := range counts {
			if c > maxC {
				maxC = c
			}
		}
	}
	return sparkline(counts, maxC)
}

func sparkline(counts []int, maxC int) string {
	levels := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, c := range counts {
		if maxC <= 0 || c == 0 {
			b.WriteRune('▁')
			continue
		}
		lvl := c * (len(levels) - 1) / maxC
		if lvl > len(levels)-1 {
			lvl = len(levels) - 1
		}
		b.WriteRune(levels[lvl])
	}
	return b.String()
}

// velocity is how active the thread is: comments actually posted to Reddit in
// the trailing minute (by their real timestamp), not how fast we display them.
func (m model) velocity() int {
	cutoff := time.Now().Add(-time.Minute)
	n := 0
	for _, c := range m.comments {
		if c.postedAt.After(cutoff) {
			n++
		}
	}
	return n
}

// demoEmit fabricates n comments drawn from a themed pool (one scene of the
// arc). The running demoIndex rotates through the pool and shifts the starting
// offset each loop, so a scene doesn't read identically every time around.
func (m *model) demoEmit(p *demoPool, n int, hot bool) []comment {
	out := make([]comment, 0, n)
	for k := 0; k < n; k++ {
		out = append(out, demoComment(m.demoIndex, p.next(), hot))
		m.demoIndex++
	}
	return out
}
