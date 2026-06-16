package main

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// narrateStart kicks off (or restarts) the narration loop, returning the first
// tick. A no-op when narration is off, an AI card is speaking, or `say` isn't
// available — callers must bump narrateGen first so any prior loop is killed.
func (m model) narrateStart() tea.Cmd {
	if !m.voice || m.aiSpeaking || runtime.GOOS != "darwin" {
		return nil
	}
	return narrateTick(narrateStartWait, m.narrateGen)
}

// suspendNarrationForAI pauses narration while an AI summary/answer is generated
// and spoken. The voice preference is left intact so narration resumes once the
// AI voice finishes.
func (m *model) suspendNarrationForAI() {
	m.aiSpeaking = true
	m.narrateGen++ // kill the running loop; narrateStart stays a no-op until resume
	m.clearSpeakBand()
	stopSay() // cut off a narration read already in progress so it doesn't overlap
}

// clearSpeakBand snaps the speaking band off (used on interrupts like suspend,
// leave, and quit, and when re-arming the band). Normal end-of-read fades out
// via speakEnd instead.
func (m *model) clearSpeakBand() {
	m.speakingID = ""
	m.speakStart = time.Time{}
	m.speakEnd = time.Time{}
}

// startSpeaking anchors the band to a comment/card at the moment its read begins.
func (m *model) startSpeaking(id string, at time.Time) {
	m.speakingID = id
	m.speakStart = at
	m.speakEnd = time.Time{}
}

// beginBandRelease starts the fade-out; the band lingers, easing to 0 over
// narrateRelease, then the spinner tick retires it.
func (m *model) beginBandRelease() {
	if m.speakingID != "" && m.speakEnd.IsZero() {
		m.speakEnd = time.Now()
	}
}

// speakBandLevel is the current band intensity in [0,1]: a smooth breathing
// pulse anchored to the read's start (so it always begins at 0), multiplied by a
// release ramp so it eases back to 0 when the read ends rather than snapping.
func (m model) speakBandLevel() float64 {
	if m.speakingID == "" || m.speakStart.IsZero() {
		return 0
	}
	elapsed := time.Since(m.speakStart).Seconds()
	// Breathing oscillation lifted onto a floor: mid-read it dips only to
	// narrateBandFloor, never to 0, so the band stays present while speaking.
	osc := 0.5 - 0.5*math.Cos(2*math.Pi*elapsed/narrateBreathSec) // 0..1
	level := narrateBandFloor + (1-narrateBandFloor)*osc          // floor..1
	// Attack: ease in from 0 so the first appearance still starts from nothing.
	if a := elapsed / narrateAttackSec; a < 1 {
		level *= a * a * (3 - 2*a)
	}
	// Release: once the read ends, ease all the way down to 0.
	if !m.speakEnd.IsZero() {
		r := time.Since(m.speakEnd).Seconds() / narrateRelease.Seconds()
		if r >= 1 {
			return 0
		}
		level *= 1 - r*r*(3-2*r)
	}
	return level
}

// speakBand returns the band color for a comment/card id, lerped from the canvas
// background (so a low level blends invisibly into it) up to the given peak by
// the current level — or nil when it isn't the one being read (or the level is
// so low it should be transparent).
func (m model) speakBand(id string, high lipgloss.Color) lipgloss.TerminalColor {
	if id == "" || id != m.speakingID {
		return nil
	}
	if lvl := m.speakBandLevel(); lvl > 0.02 {
		return lipgloss.Color(lerpHex(string(colBg), string(high), lvl))
	}
	return nil
}

// resumeNarration clears the AI-speaking gate and restarts narration if it's on.
func (m *model) resumeNarration() tea.Cmd {
	m.aiSpeaking = false
	m.narrateGen++
	return m.narrateStart()
}

// haltSpeech silences any in-flight `say` and stops the narration loop, leaving
// the user's narration preference intact. Used when leaving a thread or quitting.
func (m *model) haltSpeech() {
	stopSay()
	m.narrateGen++ // kill the loop; stale ticks are dropped
	m.clearSpeakBand()
	m.aiSpeaking = false
}

// narrateTick schedules the next narrateNextMsg after d.
func narrateTick(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return narrateNextMsg{gen} })
}

// Track the `say` processes we spawn so we can cut them off (esc out of a
// thread, quit) instead of letting the audio run on after the user moves on.
var (
	sayMu    sync.Mutex
	sayProcs = map[int]*os.Process{}
)

// runSay reads text aloud with the macOS `say` command, blocking until it
// finishes. A no-op off macOS or when `say` isn't on PATH (both expected, so
// they return nil). It returns an error only when `say` exists but fails to
// start — a real fault worth surfacing, since the user asked for voice and got
// silence. Callers run it inside a tea.Cmd goroutine, so it never blocks the UI.
func runSay(text string) error {
	if runtime.GOOS != "darwin" || strings.TrimSpace(text) == "" {
		return nil
	}
	if _, err := exec.LookPath("say"); err != nil {
		return nil // no `say` on this machine — silently skip
	}
	cmd := exec.Command("say", "--", text) // -- so a comment starting with "-" isn't read as a flag
	// Start and register under the lock so a concurrent stopSay() can't run in
	// the gap between them and miss this process (leaving it playing after an
	// interrupt). The lock is released before Wait() so stopSay() can kill it.
	sayMu.Lock()
	if err := cmd.Start(); err != nil {
		sayMu.Unlock()
		return err
	}
	pid := cmd.Process.Pid
	sayProcs[pid] = cmd.Process
	sayMu.Unlock()
	_ = cmd.Wait()
	sayMu.Lock()
	delete(sayProcs, pid)
	sayMu.Unlock()
	return nil
}

// stopSay kills any in-flight `say` we started, silencing playback immediately.
func stopSay() {
	sayMu.Lock()
	defer sayMu.Unlock()
	for pid, p := range sayProcs {
		_ = p.Kill()
		delete(sayProcs, pid)
	}
}

// narrateSay speaks a comment and reports back when done so the loop can pause
// and pick the next one.
func narrateSay(text string, gen int) tea.Cmd {
	return func() tea.Msg {
		if err := runSay(text); err != nil {
			return voiceErrMsg{err}
		}
		return narrateDoneMsg{gen}
	}
}

// newestNarratable returns the most recently shown real comment (skipping AI
// cards and empties) — the one narration should read next.
func (m model) newestNarratable() (comment, bool) {
	for i := len(m.comments) - 1; i >= 0; i-- {
		c := m.comments[i]
		if c.isSummary || strings.TrimSpace(c.body) == "" {
			continue
		}
		return c, true
	}
	return comment{}, false
}

// narrateText collapses whitespace and caps length so a single read stays short
// enough that narration keeps sampling the live edge of the thread.
func narrateText(body string) string {
	t := strings.Join(strings.Fields(body), " ")
	if r := []rune(t); len(r) > narrateMaxChars {
		t = string(r[:narrateMaxChars])
	}
	return t
}

// aiSpeakCmd reads an AI card aloud (unless voice is off) and always reports
// aiSpokeDoneMsg when finished, so narration can resume afterward.
func (m model) aiSpeakCmd(text string) tea.Cmd {
	on := m.voice
	return func() tea.Msg {
		if on {
			if err := runSay(text); err != nil {
				return voiceErrMsg{err}
			}
		}
		return aiSpokeDoneMsg{}
	}
}
