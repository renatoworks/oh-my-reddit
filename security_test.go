package main

import (
	"strings"
	"testing"
)

func TestStripControl(t *testing.T) {
	// ESC / BEL / 8-bit C1 (CSI U+009B) are removed; newline and tab survive.
	// Real reddit data is valid UTF-8, so C1 controls arrive as proper runes.
	in := "hi\x1b]2;evil\x07 there ok\nnext\ttab"
	want := "hi]2;evil there ok\nnext\ttab"
	if got := stripControl(in); got != want {
		t.Errorf("stripControl = %q, want %q", got, want)
	}
	if strings.ContainsRune(stripControl("a\x1bb"), 0x1b) {
		t.Error("ESC (0x1b) must be stripped")
	}
	if strings.ContainsRune(stripControl("ab"), 0x9c) {
		t.Error("C1 string-terminator (0x9c) must be stripped")
	}
}

func TestHyperlinkSafety(t *testing.T) {
	// A clean http(s) URL is wrapped in an OSC 8 escape.
	if out := hyperlink("https://i.redd.it/x.jpg", "L"); !strings.Contains(out, "\x1b]8;;https://i.redd.it/x.jpg") {
		t.Error("a safe http(s) URL should be hyperlinked")
	}
	// Anything risky must render as plain text with NO escape: control bytes, the
	// ST/BEL terminators, C1 controls, and non-http(s) schemes.
	bad := []string{
		"https://x/\x1b\\evil", // ST injection
		"https://x/\x07",       // BEL
		"https://x/\u009c",     // 8-bit ST (C1)
		"javascript:alert(1)",
		"file:///etc/passwd",
		"ftp://x/y",
		"x", "  ", "",
	}
	for _, u := range bad {
		if got := hyperlink(u, "L"); got != "L" {
			t.Errorf("unsafe url %q should render plain, got %q", u, got)
		}
	}
}
