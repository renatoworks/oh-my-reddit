package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.sp.Tick, animTickCmd(m.animGen), checkUpdateCmd()} // fast repaints + the update check
	if m.initCmd != nil {                                                  // nil on the auth screen — detection is user-triggered
		cmds = append(cmds, m.initCmd)
	}
	if currentCookie() != "" { // already authed (env/.env) — resolve the username
		cmds = append(cmds, usernameCmd())
	}
	return tea.Batch(cmds...)
}

// checkUpdateCmd asks the Go module proxy, in the background, whether a newer
// release exists — reporting updateMsg only when one does. It's silent on any
// error and for dev builds, so it never blocks startup or nags.
func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		cur := buildVersion()
		if cur == "" {
			return nil
		}
		latest, err := latestVersion()
		if err != nil || latest == "" {
			return nil
		}
		if newerVersion(cur, latest) {
			return updateMsg{latest: latest}
		}
		return nil
	}
}

// --- commands --------------------------------------------------------------

func pollEvery(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return pollMsg{gen} })
}

func releaseEvery(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return releaseMsg{gen} })
}

func pollCmd(url string) tea.Cmd {
	return func() tea.Msg {
		p, cs, err := fetchComments(url)
		if err != nil {
			return errMsg{err}
		}
		return batchMsg{post: p, comments: cs}
	}
}

func fetchThreadsCmd(sub string, sort listSort) tea.Cmd {
	return func() tea.Msg {
		ts, err := fetchThreads(sub, sort)
		if err != nil {
			return errMsg{err}
		}
		return threadsMsg(ts)
	}
}

// summarizeCmd wraps summarizeSentiment as a tea.Cmd, mapping errors to aiErrMsg.
func summarizeCmd(sub, title, opBody string, bodies []string) tea.Cmd {
	return func() tea.Msg {
		s, err := summarizeSentiment(sub, title, opBody, bodies)
		if err != nil {
			return aiErrMsg{err}
		}
		return summaryMsg{s}
	}
}

// askCmd answers a user's question grounded in the original post + recent
// comments, with the user's prior questions for follow-up context.
func askCmd(sub, title, opBody, question string, bodies, prior []string) tea.Cmd {
	return func() tea.Msg {
		a, err := askThread(sub, title, opBody, question, bodies, prior)
		if err != nil {
			return aiErrMsg{err}
		}
		return answerMsg{question: question, text: a}
	}
}

// detectCookieCmd runs the browser-session lookup off the UI thread (it can take
// a few seconds and may pop a Keychain prompt) and reports every account found.
func detectCookieCmd() tea.Cmd {
	return func() tea.Msg {
		accounts, checked, reached := detectRedditAccounts()
		return authResultMsg{accounts: accounts, checked: checked, reached: reached}
	}
}

// usernameCmd resolves the active cookie's reddit username for the UI.
func usernameCmd() tea.Cmd {
	return func() tea.Msg {
		name, reached := fetchUsername()
		return usernameMsg{name: name, reached: reached}
	}
}
