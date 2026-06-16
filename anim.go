package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	animAppearSec = 0.22                  // per-element fade-in duration (seconds)
	animStagger   = 28 * time.Millisecond // delay between successive elements
	animFrame     = 33 * time.Millisecond // ~30fps repaint cadence while animating
	animWindow    = 2 * time.Second       // how long to keep the fast repaints running
)

// animTickMsg drives a brief ~30fps repaint loop so the entrance fade is smooth
// (the spinner's ~10fps tick is too coarse for the stagger).
type animTickMsg struct{ gen int }

func animTickCmd(gen int) tea.Cmd {
	return tea.Tick(animFrame, func(time.Time) tea.Msg { return animTickMsg{gen} })
}

// armEntrance (re)starts the entrance animation: anchors the timeline to now and
// kicks the fast repaint loop, superseding any previous one.
func (m *model) armEntrance() tea.Cmd {
	m.enteredAt = time.Now()
	m.animGen++
	return animTickCmd(m.animGen)
}

// fade returns the entrance opacity [0,1] for the i-th element of the current
// screen: 0 before its staggered start, easing to 1 over animAppearSec. Returns
// 0 on the frame a screen is first entered (the tick handler arms it next frame),
// so content fades in from nothing rather than flashing.
func (m model) fade(i int) float64 {
	if m.screen != m.animScreen || m.enteredAt.IsZero() {
		return 0
	}
	t := (time.Since(m.enteredAt).Seconds() - float64(i)*animStagger.Seconds()) / animAppearSec
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t * t * (3 - 2*t) // smoothstep
}

// fadeColor lerps a target color up from the canvas background by the entrance
// opacity, so at t=0 the text matches the background exactly and reads as
// transparent (terminals have no real glyph alpha) rather than a dark smudge.
func fadeColor(target lipgloss.Color, t float64) lipgloss.Color {
	return lipgloss.Color(lerpHex(string(colBg), string(target), t))
}

// wordmarkFaded renders the wordmark gradient lerped up from the background.
func wordmarkFaded(t float64) string {
	return gradientText("oh-my-reddit",
		lerpHex(string(colBg), "#ff6a3d", t),
		lerpHex(string(colBg), "#b14eff", t))
}
