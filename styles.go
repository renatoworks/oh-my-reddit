package main

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// Dark & sleek palette. Muted grayscale surface with a single warm accent,
// plus a two-stop gradient reserved for the wordmark.
var (
	colBg        = lipgloss.Color("#0f0f0f") // forced terminal background (set via OSC 11 at launch)
	colBorder    = lipgloss.Color("#2a2a34")
	colSelBg     = lipgloss.Color("#20202c") // selected row background
	colText      = lipgloss.Color("#e8e8ec")
	colMessage   = lipgloss.Color("#d2d2d8") // comment message: a hair softer than colText
	colBandHigh  = lipgloss.Color("#32251a") // narration band: warm peak (comments, matches accent)
	colBandHighA = lipgloss.Color("#2a1f38") // narration band: purple peak (AI cards, matches accent2)
	colBody      = lipgloss.Color("#9e9eac") // comment body: a touch dimmer than the handle
	colMuted     = lipgloss.Color("#6c6c7a")
	colFaint     = lipgloss.Color("#44444f")
	colAccent    = lipgloss.Color("#ff6a3d") // arrivals, scores
	colAccent2   = lipgloss.Color("#b14eff") // gradient tail
)

var (
	subtitleStyle = lipgloss.NewStyle().
			Foreground(colMuted).
			Italic(true)

	headerBarStyle = lipgloss.NewStyle().
			Foreground(colFaint)

	feedStyle = lipgloss.NewStyle().
			Padding(0, 1)

	bodyMutedStyle = lipgloss.NewStyle().
			Foreground(colMuted)

	scoreStyle = lipgloss.NewStyle().
			Foreground(colAccent)

	metaStyle = lipgloss.NewStyle().
			Foreground(colFaint)

	statusStyle = lipgloss.NewStyle().
			Foreground(colMuted).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	statusKeyStyle = lipgloss.NewStyle().Foreground(colText).Bold(true)

	dotStyle = lipgloss.NewStyle().Foreground(colFaint).Render(" · ")
)

// gradientText paints s with a left-to-right gradient between two hex colors.
// Used only for the wordmark to keep the rest of the UI calm.
func gradientText(s, startHex, endHex string) string {
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return s
	}
	sr, sg, sb := hexToRGB(startHex)
	er, eg, eb := hexToRGB(endHex)

	out := ""
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		cr := int(float64(sr) + (float64(er)-float64(sr))*t)
		cg := int(float64(sg) + (float64(eg)-float64(sg))*t)
		cb := int(float64(sb) + (float64(eb)-float64(sb))*t)
		hex := "#" + twoHex(cr) + twoHex(cg) + twoHex(cb)
		out += lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true).Render(string(r))
	}
	return out
}

// lerpHex blends two hex colors by t in [0,1]. Drives the fade-in / settle.
func lerpHex(aHex, bHex string, t float64) string {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	ar, ag, ab := hexToRGB(aHex)
	br, bg, bb := hexToRGB(bHex)
	lerp := func(a, b int) int { return int(float64(a) + (float64(b)-float64(a))*t) }
	return "#" + twoHex(lerp(ar, br)) + twoHex(lerp(ag, bg)) + twoHex(lerp(ab, bb))
}

func hexToRGB(h string) (int, int, int) {
	if len(h) == 7 && h[0] == '#' {
		r, _ := strconv.ParseInt(h[1:3], 16, 0)
		g, _ := strconv.ParseInt(h[3:5], 16, 0)
		b, _ := strconv.ParseInt(h[5:7], 16, 0)
		return int(r), int(g), int(b)
	}
	return 255, 255, 255
}

func twoHex(v int) string {
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	s := strconv.FormatInt(int64(v), 16)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}
