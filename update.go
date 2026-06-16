package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- update ----------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// -2 for the blank lines padding the comments off the header and footer.
		vpHeight := msg.Height - lipgloss.Height(m.renderHeader()) - lipgloss.Height(m.renderStatus()) - 2
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.vp = viewport.New(m.colW(), vpHeight)
			m.ready = true
		} else {
			m.vp.Width = m.colW()
			m.vp.Height = vpHeight
		}
		m.refreshFeed()
		m.vp.GotoBottom()
		if m.opOpen {
			m.cacheOPImage() // re-render the image at the new width
			m.syncOPModal()
		}
		return m, nil

	case spinner.TickMsg:
		m.tick++
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		cmds := []tea.Cmd{cmd}
		// Arm the entrance animation (+ fast repaint loop) when the screen changes.
		if m.screen != m.animScreen {
			m.animScreen = m.screen
			cmds = append(cmds, m.armEntrance())
		}
		// Once the band's release fade has fully elapsed, retire the speaking state.
		if m.speakingID != "" && !m.speakEnd.IsZero() && time.Since(m.speakEnd) >= narrateRelease {
			m.clearSpeakBand()
		}
		if m.screen == screenFeed && (m.anyFading() || m.speakingID != "" || len(m.comments) == 0) {
			m.refreshFeed() // animate the fade between releases, the loading dots, and the narration band
		}
		return m, tea.Batch(cmds...)

	case animTickMsg:
		// Keep repainting at ~30fps while the entrance fade runs (View redraws it).
		if msg.gen == m.animGen && time.Since(m.enteredAt) < animWindow {
			return m, animTickCmd(m.animGen)
		}
		return m, nil

	case threadsMsg:
		m.loading = false
		m.resorting = false
		m.threads = []thread(msg)
		m.recomputeResults()
		m.cursor = 0
		m.listOffset = 0
		m.err = nil
		if m.subreddit != "" { // the fetch succeeded, so it's a real subreddit — save it now
			m.recordSub(m.subreddit)
		}
		return m, m.armEntrance() // rows appear now (after the load) — animate from here

	case batchMsg:
		m.err = nil
		if msg.post != nil {
			m.op = msg.post // refreshes the OP score live on each poll
			if m.opOpen {
				m.syncOPModal() // keep the open modal's body current
			}
		}
		m.enqueue(msg.comments)
		m.refreshFeed() // repaint so refreshed scores show even with no new comments
		return m, nil

	case summaryMsg:
		return m, m.appendAICard("live sentiment", msg.text)

	case answerMsg:
		return m, m.appendAICard("asked: "+truncate(msg.question, 60), msg.text)

	case aiSpokeDoneMsg:
		m.beginBandRelease() // fade the card's band out smoothly
		return m, m.resumeNarration()

	case aiErrMsg:
		m.summarizing = false
		m.err = msg.err
		return m, m.resumeNarration() // AI failed (no speech coming); bring narration back

	case voiceErrMsg:
		// `say` couldn't start: tell the user once and turn voice off (in-session,
		// not persisted) so we don't keep reading to silence.
		m.voice = false
		m.haltSpeech()
		m.err = fmt.Errorf("voice unavailable: %v", msg.err)
		m.refreshFeed()
		return m, nil

	case updateMsg:
		m.updateLatest = msg.latest // a newer release exists; the home screen shows the notice
		return m, nil

	case opImageMsg:
		if msg.url == m.opImageURL { // still the OP we're viewing
			m.opImage = msg.img
			m.opImageLoading = false
			m.cacheOPImage() // render the half-blocks once, here
			if m.opOpen {
				m.syncOPModal() // re-render the modal body now that the image is in
			}
		}
		return m, nil

	case errMsg:
		m.loading = false
		m.resorting = false
		m.summarizing = false
		if errors.Is(msg.err, errAuth) {
			// Session expired and couldn't be refreshed — prompt re-auth instead
			// of leaving a loader spinning forever.
			m.promptReauth()
			return m, nil
		}
		m.err = msg.err
		// The fetch failed for a non-auth reason — often a 429 storm, which is how
		// a silently-expired session degrades to anonymous, rate-limited browsing.
		// If we hold a cookie, quietly re-verify it; usernameMsg prompts re-auth
		// only on a definitive "you're logged out", so a busy server won't boot us.
		if currentCookie() != "" {
			return m, usernameCmd()
		}
		return m, nil

	case authResultMsg:
		if m.authMode != authSearching {
			return m, nil // user cancelled (esc) before the result arrived
		}
		m.authMode = authIdle
		m.authTried = true
		m.authChecked = msg.checked
		m.authUnreached = len(msg.accounts) == 0 && !msg.reached
		if len(msg.accounts) > 0 {
			// Always let the user confirm which account (even when there's one).
			m.authMode = authPicking
			m.authAccounts = msg.accounts
			m.authCursor = 0
		}
		return m, nil // 0 found → failure options; >0 → the picker

	case usernameMsg:
		if msg.name != "" {
			m.username = msg.name
			return m, nil
		}
		// Cookie present but reddit says we're not logged in → the cached session
		// is anonymous/expired. Don't browse anonymously — prompt re-auth. (A
		// network blip leaves reached=false, so we don't false-trigger.)
		if msg.reached && currentCookie() != "" && m.screen != screenAuth {
			m.promptReauth()
		}
		return m, nil

	case pollMsg:
		if msg.gen != m.loopGen || m.screen != screenFeed || m.paused {
			return m, nil // stale, off-screen, or paused
		}
		if m.live {
			return m, tea.Batch(pollCmd(m.url), pollEvery(m.interval, m.loopGen))
		}
		sc := demoArc[m.demoBeat%len(demoArc)] // walk the match arc, looping
		m.demoBeat++
		if sc.count > 0 {
			m.enqueue(m.demoEmit(sc.pool, sc.count, sc.hot))
		}
		return m, pollEvery(sc.gap, m.loopGen) // each scene sets its own pace

	case releaseMsg:
		if msg.gen != m.loopGen || m.screen != screenFeed || m.paused {
			return m, nil
		}
		m.releaseOne()
		return m, releaseEvery(m.releaseDelay(), m.loopGen)

	case narrateNextMsg:
		if msg.gen != m.narrateGen || !m.voice || m.screen != screenFeed {
			return m, nil // stale loop, narration off, or left the feed
		}
		c, ok := m.newestNarratable()
		if !ok || c.id == m.narratedID {
			return m, narrateTick(narratePoll, m.narrateGen) // nothing new yet
		}
		m.narratedID = c.id
		m.startSpeaking(c.id, time.Now()) // anchor the band so it breathes up from 0
		m.refreshFeed()
		return m, narrateSay(narrateText(c.body), m.narrateGen)

	case narrateDoneMsg:
		if msg.gen != m.narrateGen || !m.voice || m.screen != screenFeed {
			return m, nil // stale: whatever invalidated the loop already handled the band
		}
		m.beginBandRelease() // ease the band out instead of snapping it off
		m.refreshFeed()
		return m, narrateTick(narratePause, m.narrateGen) // breathe, then read the newest

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	switch m.screen {
	case screenInput:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case screenFeed:
		if m.opOpen { // mouse wheel scrolls the open modal, not the feed behind it
			var cmd tea.Cmd
			m.opVP, cmd = m.opVP.Update(msg)
			return m, cmd
		}
		if m.ready {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// handleAuthKey drives the connect screen: the idle menu, the search/picker
// sub-states, and the manual-paste sub-mode. esc backs out of a sub-state (and
// does nothing on the idle menu — it's a top screen); q quits (so does ctrl+c).
func (m model) handleAuthKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Paste mode captures all keys (you're typing the cookie); esc backs out.
	if m.authMode == authPasting {
		switch msg.String() {
		case "enter":
			if v := strings.TrimSpace(m.authInput.Value()); v != "" {
				setCookie(v)
				saveCookie(v)
				return m, m.routeFromArg(m.startArg)
			}
			m.authMode = authIdle
			m.authInput.Blur()
			return m, nil
		case "esc":
			m.authMode = authIdle
			m.authInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.authInput, cmd = m.authInput.Update(msg)
			return m, cmd
		}
	}

	if msg.String() == "q" {
		stopSay() // silence any in-flight speech on quit
		return m, tea.Quit
	}
	if m.authMode == authSearching {
		if msg.String() == "esc" {
			m.authMode = authIdle // cancel the search, back to the menu
		}
		return m, nil
	}
	if m.authMode == authPicking {
		switch msg.String() {
		case "up", "k":
			m.authCursor = max(0, m.authCursor-1)
		case "down", "j":
			m.authCursor = min(len(m.authAccounts)-1, m.authCursor+1)
		case "enter":
			a := m.authAccounts[m.authCursor]
			setCookie(a.header)
			saveCookie(a.header)
			m.username = a.username
			m.authMode = authIdle
			return m, m.routeFromArg(m.startArg)
		case "esc":
			m.authMode = authIdle // back to the menu
		}
		return m, nil
	}

	// Idle menu — esc does nothing (this is the top screen).
	switch msg.String() {
	case "enter":
		m.authMode = authSearching
		m.authTried = true
		m.authExpired = false       // a fresh attempt clears the expired notice
		m.authUnreached = false     // ...and the "couldn't reach reddit" notice
		return m, detectCookieCmd() // find a session in the browser (on demand)
	case "o":
		_ = openBrowser("https://www.reddit.com/login/") // best-effort; user can open it manually
	case "p":
		m.authMode = authPasting
		m.authInput.Width = max(20, min(64, m.width-10))
		m.authInput.Focus()
		return m, textinput.Blink
	case "d":
		return m, m.routeFromArg("demo") // explore the synthetic feed instead
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		stopSay() // silence any in-flight speech on quit
		return m, tea.Quit
	}
	// ctrl+l logs out from anywhere: drop the cookie and return to the connect
	// screen. Not while typing into a field (it'd be a surprise).
	if msg.String() == "ctrl+l" && m.screen != screenAuth && m.feedMode == feedIdle {
		m.logout()
		return m, nil
	}

	switch m.screen {
	case screenAuth:
		return m.handleAuthKey(msg)

	case screenInput:
		items := m.inputItems()
		switch msg.String() {
		case "enter":
			if m.inputCursor >= 0 && m.inputCursor < len(items) {
				return m.activateRecent(items[m.inputCursor])
			}
			return m.submitInput()
		case "esc":
			return m, nil // top screen — esc does nothing (ctrl+c quits)
		case "ctrl+x":
			m.recentSubs = nil
			m.recentThreads = nil
			saveRecents(nil, nil)
			m.inputCursor = -1
			return m, nil
		case "up":
			if m.inputCursor > -1 {
				m.inputCursor--
			}
			return m, nil
		case "down":
			if m.inputCursor < len(items)-1 {
				m.inputCursor++
			}
			return m, nil
		}
		m.inputCursor = -1 // typing returns focus to the text field
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case screenList:
		switch msg.String() {
		case "up":
			m.cursor--
			m.clampCursor()
			return m, nil
		case "down":
			m.cursor++
			m.clampCursor()
			return m, nil
		case "left", "right":
			// Cycle the listing order (hot ⇄ new ⇄ rising ⇄ top) and re-fetch.
			// Match threads rarely crack "hot" — they earn comments, not upvotes —
			// so "new" is how you surface a live one. We keep the current list (and
			// any filter) on screen and show a "sorting by…" indicator rather than
			// tearing down to the full-page loader.
			dir := 1
			if msg.String() == "left" {
				dir = -1
			}
			m.cycleListSort(dir)
			m.resorting = true
			m.err = nil
			return m, fetchThreadsCmd(m.subreddit, m.listSort)
		case "enter":
			if len(m.results) > 0 {
				t := m.results[m.cursor]
				m.recordThread(m.subreddit, t.permalink, t.title)
				m.startFeed(t.permalink, t.title, "r/"+m.subreddit, true)
				return m, m.feedCmds()
			}
			return m, nil
		case "esc":
			if m.filter != "" { // first esc clears the filter, second goes back
				m.filter = ""
				m.recomputeResults()
				m.cursor, m.listOffset = 0, 0
				return m, nil
			}
			m.err = nil
			return m, m.backToInput()
		case "backspace":
			// Only edits the filter; never navigates back (esc does that), so an
			// errant backspace on an empty filter doesn't drop you to the input.
			if m.filter != "" {
				m.filter = dropLastRune(m.filter)
				m.recomputeResults()
				m.cursor, m.listOffset = 0, 0
			}
			return m, nil
		default:
			// Any printable key types into the fuzzy filter.
			switch msg.Type {
			case tea.KeyRunes:
				m.filter += string(msg.Runes)
			case tea.KeySpace:
				m.filter += " "
			default:
				return m, nil
			}
			m.recomputeResults()
			m.cursor, m.listOffset = 0, 0
			return m, nil
		}

	case screenFeed:
		// The OP modal is exclusive: keys scroll its body or close it.
		if m.opOpen {
			switch msg.String() {
			case "esc", "t", "q", "enter":
				m.opOpen = false
				return m, nil
			default:
				var cmd tea.Cmd
				m.opVP, cmd = m.opVP.Update(msg) // ↑/↓/j/k/pgup/pgdn scroll the post
				return m, cmd
			}
		}
		if m.feedMode == feedSearching {
			switch msg.String() {
			case "enter":
				m.feedMode = feedIdle // keep the filter applied
				return m, nil
			case "esc":
				m.feedMode = feedIdle
				m.setFeedQuery("")
				return m, nil
			case "backspace":
				if m.feedQuery != "" {
					m.setFeedQuery(dropLastRune(m.feedQuery))
				}
				return m, nil
			default:
				if q, ok := typedInto(m.feedQuery, msg); ok {
					m.setFeedQuery(q)
				}
				return m, nil
			}
		}
		if m.feedMode == feedAsking {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.askQuery)
				m.feedMode = feedIdle
				if q == "" {
					m.askQuery = ""
					return m, nil
				}
				m.summarizing = true
				m.aiLoadLabel = "thinking…"
				m.suspendNarrationForAI() // pause narration; it resumes after the answer is spoken
				m.refreshFeed()
				return m, askCmd(m.subreddit, m.title, m.opContext(), q, m.recentBodies(120), m.recentAskedQA(120))
			case "esc":
				m.feedMode = feedIdle
				m.askQuery = ""
				return m, nil
			case "backspace":
				if m.askQuery != "" {
					m.askQuery = dropLastRune(m.askQuery)
				}
				return m, nil
			default:
				if q, ok := typedInto(m.askQuery, msg); ok {
					m.askQuery = q
				}
				return m, nil
			}
		}
		switch msg.String() {
		case "esc":
			if m.feedQuery != "" { // first esc clears the in-thread filter
				m.setFeedQuery("")
				return m, nil
			}
			m.haltSpeech() // stop any narration/summary audio when leaving the thread
			if len(m.threads) > 0 {
				m.screen = screenList
				m.err = nil
				return m, nil
			}
			m.err = nil
			return m, m.backToInput()
		case "/":
			m.feedMode = feedSearching
			return m, nil
		case "o":
			if err := openBrowser(m.url); err != nil {
				m.err = err
			}
			return m, nil
		case "t":
			if m.op == nil {
				return m, nil // no OP to show (RSS-only thread, or not fetched yet)
			}
			m.opOpen = true
			m.opVP = viewport.New(0, 0)
			// Kick off the image fetch for an image post we haven't loaded yet.
			var cmd tea.Cmd
			if isImageURL(m.op.link) && m.opImageURL != m.op.link {
				m.opImageURL = m.op.link
				m.opImage = nil
				m.opImageLoading = true
				cmd = fetchOPImageCmd(m.op.link)
			}
			m.cacheOPImage() // (re)render at the current width; no-op while loading
			m.syncOPModal()
			m.opVP.GotoTop()
			return m, cmd
		case "s":
			bodies := m.commentsSinceLastSummary()
			if !m.aiEnabled || m.summarizing || len(bodies) == 0 {
				return m, nil // no key, already running, or nothing new since last summary
			}
			m.summarizing = true
			m.aiLoadLabel = "reading the room…"
			m.suspendNarrationForAI() // pause narration; it resumes after the summary is spoken
			m.refreshFeed()
			return m, summarizeCmd(m.subreddit, m.title, m.opContext(), bodies)
		case "a":
			if !m.aiEnabled || m.summarizing {
				return m, nil
			}
			m.feedMode = feedAsking
			m.askQuery = ""
			return m, nil
		case "up", "k":
			m.feedJump(-1)
			return m, nil
		case "down", "j":
			m.feedJump(1)
			return m, nil
		case "r":
			// Refresh now and restart the poll cycle from zero.
			m.loopGen++
			m.paused = false
			cmds := m.pollReleaseCmds()
			if !m.live {
				m.enqueue(m.demoEmit(demoChatter, 4, false))
			}
			return m, tea.Batch(cmds...)
		case "v":
			m.voice = !m.voice
			saveSettings(settings{Voice: m.voice}) // remember the choice across runs
			if !m.voice {
				m.haltSpeech() // silence narration + any in-flight AI speech
				m.refreshFeed()
				return m, nil
			}
			m.narrateGen++ // turning on: start a fresh narration loop
			m.clearSpeakBand()
			m.refreshFeed()
			return m, m.narrateStart()
		case "p":
			m.paused = !m.paused
			m.loopGen++ // drop outstanding ticks either way
			if m.paused {
				return m, nil
			}
			return m, tea.Batch(m.pollReleaseCmds()...) // catch up on resume
		}
		if m.ready {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// activateRecent jumps to a recent subreddit or thread from the input screen.
func (m model) activateRecent(it inputItem) (tea.Model, tea.Cmd) {
	if it.isSub {
		m.subreddit = it.sub // recorded on fetch success (threadsMsg), not here
		m.screen = screenList
		m.loading = true
		m.threads, m.results = nil, nil
		m.filter = ""
		m.cursor, m.listOffset = 0, 0
		m.err = nil
		return m, fetchThreadsCmd(it.sub, m.listSort)
	}
	m.recordThread(it.sub, it.url, it.title)
	m.startFeed(it.url, it.title, "r/"+it.sub, true)
	return m, m.feedCmds()
}

func (m model) submitInput() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.input.Value())
	if val == "" {
		return m, nil
	}
	// demo / thread URL / subreddit dispatch is identical to a CLI arg.
	return m, m.routeFromArg(val)
}

func isURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http") || strings.Contains(l, "reddit.com")
}
