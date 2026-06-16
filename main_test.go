package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

// opScreenModel returns a feed model with the OP reader open at the given size.
// The demo OP (u/the_gaffer) is loaded unless a test overrides m.op.
func opScreenModel(w, h int) model {
	m := newModel()
	m.startFeed("", "Match Thread: it's all kicking off", sampleSubreddit, false)
	m.width, m.height = w, h
	m.vp = viewport.New(w, max(1, h-4))
	m.ready = true
	m.opOpen = true
	m.opVP = viewport.New(0, 0)
	return m
}

func TestOPScreenRenders(t *testing.T) {
	for _, dim := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {30, 10}} {
		m := opScreenModel(dim[0], dim[1])
		if m.op == nil {
			t.Fatalf("demo should have an OP")
		}
		m.syncOPModal()
		out := m.View()
		if !strings.Contains(ansi.Strip(out), "the_gaffer") {
			t.Errorf("%v: OP author not visible", dim)
		}
		for _, ln := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(ln); w > dim[0] {
				t.Errorf("%v: line width %d exceeds terminal width", dim, w)
				break
			}
		}
	}
}

func TestOPScreenLinkPost(t *testing.T) {
	m := opScreenModel(100, 30)
	m.op = &post{title: "cool link", author: "someone", link: "https://example.com/x", hasScore: true, score: 5}
	m.syncOPModal()
	if !strings.Contains(ansi.Strip(m.View()), "example.com") {
		t.Errorf("link not shown for link post")
	}
}

// A markdown table full of emoji must keep the panel border solid (every row the
// exact terminal width) and still show the real glyphs.
func TestOPScreenEmojiBorderSolid(t *testing.T) {
	m := opScreenModel(90, 26)
	m.op = &post{
		title:  "Match Thread: Ivory Coast vs Ecuador",
		author: "jiraiya",
		body: "## Subs\n\n| Player | Notes |\n| --- | --- |\n" +
			"| Rekik | ⚽ 43' |\n| Valery | 🔄 off 72' |\n| Mejbri | 🟨 54' · 🔄 off 83' |\n",
		hasScore: true, score: 132,
	}
	m.syncOPModal()
	m.opVP.SetYOffset(0)
	out := m.View()

	col := -1
	for _, ln := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(ln); w != m.width {
			t.Errorf("row width %d != terminal width %d", w, m.width)
		}
		s := ansi.Strip(ln)
		if i := strings.LastIndex(s, "│"); i >= 0 {
			if c := ansi.StringWidth(s[:i]); col == -1 {
				col = c
			} else if c != col {
				t.Errorf("right border drifts: col %d != %d", c, col)
			}
		}
	}
	if !strings.ContainsAny(ansi.Strip(out), "⚽🔄🟨") {
		t.Errorf("emoji missing from the rendered reader")
	}
}
