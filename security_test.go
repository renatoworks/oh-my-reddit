package main

import (
	"strings"
	"testing"
)

func TestStripControl(t *testing.T) {
	// ESC / BEL / 8-bit C1 (CSI U+009B) are removed; newline and tab survive.
	// Real reddit data is valid UTF-8, so C1 controls arrive as proper runes.
	in := "hi\x1b]2;evil\x07 there ok\nnext\ttab"
	want := "hi]2;evil there ok\nnext\ttab"
	if got := stripControl(in); got != want {
		t.Errorf("stripControl = %q, want %q", got, want)
	}
	if strings.ContainsRune(stripControl("a\x1bb"), 0x1b) {
		t.Error("ESC (0x1b) must be stripped")
	}
	if strings.ContainsRune(stripControl("ab"), 0x9c) {
		t.Error("C1 string-terminator (U+009C) must be stripped")
	}
}
