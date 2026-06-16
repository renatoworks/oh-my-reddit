package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestIsImageURL(t *testing.T) {
	yes := []string{"https://i.redd.it/abc.jpg", "http://x/y.PNG", "https://x/z.gif?width=640"}
	no := []string{"", "https://reddit.com/gallery/abc", "https://x/y.webp", "https://x/page"}
	for _, u := range yes {
		if !isImageURL(u) {
			t.Errorf("isImageURL(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if isImageURL(u) {
			t.Errorf("isImageURL(%q) = true, want false", u)
		}
	}
}

func TestRenderImageBlocks(t *testing.T) {
	// An 8x8 image: red top half, blue bottom half.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		c := color.RGBA{255, 0, 0, 255}
		if y >= 4 {
			c = color.RGBA{0, 0, 255, 255}
		}
		for x := 0; x < 8; x++ {
			img.Set(x, y, c)
		}
	}

	out := renderImageBlocks(img, 8, 40)
	if out == "" {
		t.Fatal("empty render")
	}
	// Square image at 8 cols → rows = 8*8/(8*2) = 4.
	if rows := strings.Count(out, "\n") + 1; rows != 4 {
		t.Errorf("rows = %d, want 4", rows)
	}
	if !strings.Contains(out, "▀") {
		t.Error("expected a half-block character in the output")
	}
	// Each row should be exactly `cols` cells wide.
	for i, line := range strings.Split(out, "\n") {
		if n := strings.Count(line, "▀"); n != 8 {
			t.Errorf("row %d has %d cells, want 8", i, n)
		}
	}
}
