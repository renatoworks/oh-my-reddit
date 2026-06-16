package main

import (
	"image"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// comment is the single unit of the feed. shownAt is when it was released into
// the visible feed (zero until then) and drives the fade-in / settle.
type comment struct {
	id        string
	author    string
	body      string
	score     int
	hasScore  bool   // false for RSS/demo (no score available)
	parentID  string // "t1_…" reply target, "t3_…" if top-level
	isSummary bool   // an AI card (sentiment or answer), not a real comment
	aiHeader  string // label for an AI card ("live sentiment" / "asked: …")
	postedAt  time.Time
	shownAt   time.Time
}

// thread is one selectable post in a subreddit listing.
type thread struct {
	id          string
	title       string
	permalink   string
	author      string
	numComments int
	stickied    bool
}

// post is the OP — the thread's own submission, shown in the OP modal. body is
// the self-text (empty for link posts); link is the external destination (empty
// for self posts).
type post struct {
	title    string
	author   string
	body     string
	score    int
	hasScore bool
	link     string
	postedAt time.Time
}

type screen int

const (
	screenInput screen = iota
	screenList
	screenFeed
	screenAuth
)

// authMode is the active sub-state of the connect screen. Exactly one is active
// at a time, so it's an enum rather than a set of mutually-exclusive bools.
type authMode int

const (
	authIdle      authMode = iota // the connect menu
	authSearching                 // browser detection in flight
	authPicking                   // choosing among found accounts
	authPasting                   // typing a cookie by hand
)

// feedMode is the active input mode on the feed screen. Searching and asking are
// mutually exclusive (you can't type a filter and a question at once), so it's an
// enum rather than parallel bools. The summarizing flag is separate (a request
// can be in flight while the input is idle).
type feedMode int

const (
	feedIdle      feedMode = iota // normal feed navigation
	feedSearching                 // typing into the in-thread search box
	feedAsking                    // typing a question to the AI
)

const (
	livePollInterval = 10 * time.Second       // how often we hit Reddit
	demoPollInterval = 3 * time.Second        // demo: synthesize a small burst
	releaseSlow      = 750 * time.Millisecond // graceful trickle when queue is small
	releaseFast      = 180 * time.Millisecond // drain a backlog (e.g. first load)
	fadeDuration     = 2200 * time.Millisecond
	appearDuration   = 450 * time.Millisecond
	initialCap       = 30 // cap the first batch so launch isn't a minutes-long drain

	// Narration: read comments aloud as they arrive. We can't keep up with a
	// fast thread, so it samples — after finishing one it pauses, then reads the
	// newest comment at that moment, skipping whatever scrolled by in between.
	narratePause     = 1500 * time.Millisecond // break after finishing a read
	narratePoll      = 700 * time.Millisecond  // re-check cadence when nothing new
	narrateStartWait = 850 * time.Millisecond  // settle before a read (also > narrateRelease)
	narrateMaxChars  = 220                     // keep a single read snappy
	narrateRelease   = 750 * time.Millisecond  // band fade-out when a read ends
	narrateBreathSec = 2.2                     // seconds per band breathing cycle (slower = smoother)
	narrateBandFloor = 0.4                     // trough level mid-read (never fully fades while active)
	narrateAttackSec = 0.7                     // ease-in from 0 at the start of a read
)

// pollMsg / releaseMsg carry a generation so stale timers (after refresh,
// pause, or switching threads) can be ignored — Bubble Tea ticks can't be
// cancelled, so we tag and drop them instead.
type pollMsg struct{ gen int }
type releaseMsg struct{ gen int }

// narrateNextMsg asks the loop to read the newest comment; narrateDoneMsg fires
// when a `say` finishes. Both carry the narration generation so a stale loop
// (after toggling off or switching threads) is dropped.
type narrateNextMsg struct{ gen int }
type narrateDoneMsg struct{ gen int }

// aiSpokeDoneMsg fires when an AI summary/answer finishes being read aloud, so
// narration can resume.
type aiSpokeDoneMsg struct{}
type batchMsg struct {
	post     *post
	comments []comment
}
type threadsMsg []thread
type summaryMsg struct{ text string }
type answerMsg struct{ question, text string }
type errMsg struct{ err error }

// aiErrMsg is a failed summary/ask, kept distinct from errMsg so only an AI
// failure resumes narration — a background poll error mid-speech must not.
type aiErrMsg struct{ err error }

// voiceErrMsg fires when `say` fails to start (e.g. no audio device): the user
// turned voice on but would hear nothing, so we notify and disable it.
type voiceErrMsg struct{ err error }

// updateMsg carries a newer released version found by the background check.
type updateMsg struct{ latest string }

// opImageMsg carries a decoded OP image (img is nil if the fetch/decode failed).
type opImageMsg struct {
	url string
	img image.Image
}
type authResultMsg struct {
	accounts []account
	checked  []string
	reached  bool // false if reddit was unreachable during the scan (vs nobody logged in)
}
type usernameMsg struct {
	name    string
	reached bool // whether reddit actually answered (vs a network blip)
}

type model struct {
	screen screen

	// input screen
	input       textinput.Model
	inputCursor int // -1 = typing; >=0 selects a recent item

	// auth screen: acquire a reddit session from the browser before the first fetch
	authMode      authMode        // active sub-state (idle menu / searching / picking / pasting)
	authTried     bool            // a detection has run (so we can show "couldn't find")
	authExpired   bool            // a live session went stale (403) — show "reconnect"
	authUnreached bool            // last scan found cookies but couldn't reach reddit to verify
	authChecked   []string        // browsers examined (shown on failure)
	authAccounts  []account       // sessions found, for the picker
	authCursor    int             // selected account in the picker
	authInput     textinput.Model // manual-paste fallback field
	username      string          // logged-in reddit username (for the UI)
	startArg      string          // CLI arg to route to once auth resolves
	initCmd       tea.Cmd         // initial command for the (post-auth) destination

	// recents (shown on the input screen)
	recentSubs    []string
	recentThreads []recentThread

	// thread list
	subreddit  string   // canonical bare name (no "r/"); fetches, AI, recents
	subLabel   string   // display label for the feed header ("r/soccer", "live")
	listSort   listSort // listing order for the thread list: hot/new/rising/top
	threads    []thread
	results    []thread // threads after the fuzzy filter (what's navigated)
	filter     string
	cursor     int
	listOffset int  // first visible result row (for scrolling)
	loading    bool // initial subreddit fetch (shows the full-page loader)
	resorting  bool // re-fetching after a sort change (keeps the list in place)

	// OP modal
	op             *post          // the OP submission (nil for demo/RSS until fetched)
	opOpen         bool           // the OP modal is open
	opVP           viewport.Model // scrollable body inside the OP modal
	opImage        image.Image    // decoded OP image (nil until loaded; image posts only)
	opImageURL     string         // the image URL opImage corresponds to (or is loading)
	opImageLoading bool           // an OP image fetch is in flight
	opImageRender  string         // cached half-block render of opImage (rebuilt on load/resize)
	opImageRenderW int            // content width opImageRender was built at

	// live comment feed
	comments      []comment          // visible
	pending       []comment          // fetched, awaiting graceful release
	seen          map[string]bool    // dedup by id
	byName        map[string]comment // fullname -> comment, to resolve reply parents
	scores        map[string]int     // latest score per id, refreshed every poll
	vp            viewport.Model
	ready         bool
	title         string
	live          bool
	url           string
	interval      time.Duration
	demoIndex     int
	demoBeat      int // demo poll counter, drives the bursts-and-lulls rhythm
	updatedAt     time.Time
	paused        bool
	loopGen       int      // current timer generation
	commentStarts []int    // line index where each comment block begins (for jump nav)
	feedLines     int      // total line count of the rendered feed
	feedMode      feedMode // active feed input mode (idle / searching / asking)
	feedQuery     string   // active in-thread filter
	askQuery      string   // the question being typed
	summarizing   bool     // an AI request is in flight
	aiEnabled     bool     // an OPENAI_API_KEY is set; gates the s/a features and their help

	// voice + narration (macOS `say`)
	voice       bool      // master voice switch: narrate comments AND speak AI cards
	narratedID  string    // id of the last comment spoken, so we don't repeat it
	speakingID  string    // id of the comment/card being read (drives the band)
	speakStart  time.Time // when the current read began (anchors the breathing phase)
	speakEnd    time.Time // when it finished; non-zero means the band is fading out
	narrateGen  int       // bumped to invalidate a stale narration loop
	aiSpeaking  bool      // an AI card is generating/speaking; narration is suspended
	aiLoadLabel string    // status text while an AI request runs
	tick        int       // frame counter (advances on each spinner tick)

	// entrance animation
	enteredAt  time.Time // when the current screen was entered (drives entrance fade)
	animScreen screen    // screen the entrance animation is armed for
	animGen    int       // invalidates a stale entrance-repaint loop

	sp  spinner.Model
	err error

	updateLatest string // a newer released version (from the background check); "" = none

	width, height int
}

func newModel() model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	ti := textinput.New()
	ti.Placeholder = "r/soccer   ·   paste a thread URL   ·   or type 'demo'"
	ti.Prompt = lipgloss.NewStyle().Foreground(colAccent).Render("› ")
	ti.Focus()
	ti.CharLimit = 200

	ci := textinput.New() // manual cookie-paste fallback on the auth screen
	ci.Placeholder = "paste your reddit Cookie header"
	ci.Prompt = lipgloss.NewStyle().Foreground(colAccent).Render("› ")
	ci.CharLimit = 0 // cookie headers are long
	ci.EchoMode = textinput.EchoPassword

	subs, threads := loadRecents()
	m := model{
		screen:        screenInput,
		input:         ti,
		authInput:     ci,
		inputCursor:   -1,
		recentSubs:    subs,
		recentThreads: threads,
		seen:          map[string]bool{},
		byName:        map[string]comment{},
		scores:        map[string]int{},
		listSort:      sortHot,
		sp:            sp,
		aiEnabled:     os.Getenv("OPENAI_API_KEY") != "", // .env already loaded in main()
		voice:         loadSettings().Voice,              // off until the user turns it on; remembered across runs
	}

	if len(os.Args) > 1 {
		m.startArg = strings.TrimSpace(os.Args[1])
	}
	// Gate on auth: when no session cookie is known yet, show the (idle) auth
	// screen first so the user can choose to connect — detection is on demand,
	// not automatic. Demo needs no network, so it skips straight through.
	if currentCookie() == "" && m.startArg != "demo" {
		m.screen = screenAuth
	} else {
		m.initCmd = m.routeFromArg(m.startArg)
	}
	// Arm the entrance animation for the first screen, so it fades in on launch
	// (rather than staying invisible until the screen first changes).
	m.animScreen = m.screen
	m.enteredAt = time.Now()
	return m
}

// routeFromArg sets the destination screen for a CLI arg (empty = input screen)
// and returns its initial command.
func (m *model) routeFromArg(arg string) tea.Cmd {
	switch {
	case arg == "demo":
		m.subreddit = cleanSubreddit(sampleSubreddit)
		m.startFeed("", sampleThreadTitle, sampleSubreddit, false)
		return m.feedCmds()
	case isURL(arg):
		m.startFeed(arg, "loading thread…", "live", true)
		return m.feedCmds()
	case arg != "":
		m.subreddit = cleanSubreddit(arg)
		// Reset list state; don't save to recents yet (only after the fetch
		// confirms the subreddit exists, via threadsMsg).
		m.screen = screenList
		m.loading = true
		m.threads, m.results = nil, nil
		m.filter = ""
		m.cursor, m.listOffset = 0, 0
		m.err = nil
		return fetchThreadsCmd(m.subreddit, m.listSort)
	default:
		m.screen = screenInput
		return textinput.Blink
	}
}

// resetToAuthIdle drops any feed timers and shows the connect screen's idle menu.
func (m *model) resetToAuthIdle() {
	m.loopGen++
	m.paused = false
	m.screen = screenAuth
	m.authMode = authIdle
	m.authTried = false
	m.authExpired = false
	m.authUnreached = false
	m.authAccounts = nil
}

// logout drops the active session (held in memory only) and returns to the
// connect screen's idle menu.
func (m *model) logout() {
	setCookie("")
	clearSavedCookie()
	m.username = ""
	m.resetToAuthIdle()
}

// promptReauth drops a dead/anonymous session and returns to the connect screen
// with the "expired" notice, so we never keep browsing without a real session.
func (m *model) promptReauth() {
	m.haltSpeech()
	setCookie("")
	clearSavedCookie()
	m.username = ""
	m.err = nil
	m.resetToAuthIdle()  // stops the loops and shows the idle connect screen
	m.authExpired = true // ...but this path wants the "expired" notice set
}

// backToInput goes to the input screen — or, when there's no session, back to
// the connect screen, so an anonymous user (e.g. leaving the demo) never lands
// on the browse UI.
func (m *model) backToInput() tea.Cmd {
	if currentCookie() == "" {
		m.resetToAuthIdle()
		return nil
	}
	m.screen = screenInput
	return textinput.Blink
}
