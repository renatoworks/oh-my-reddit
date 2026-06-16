package main

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"  // register decoders
	_ "image/jpeg" // (webp isn't stdlib; those posts fall back to the link)
	_ "image/png"
	"io"
	"net/http"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// opImageMaxRows caps how tall a rendered OP image gets (the viewport scrolls).
const opImageMaxRows = 40

// maxImagePixels rejects decompression bombs: a small file can decode to a huge
// pixel buffer. 40MP is well above any real photo but far below a bomb.
const maxImagePixels = 40_000_000

// isImageURL reports whether u points at an image we can decode (by extension,
// ignoring any query string). webp is excluded — the stdlib can't decode it.
func isImageURL(u string) bool {
	if u == "" {
		return false
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	u = strings.ToLower(u)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif"} {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

// fetchOPImageCmd downloads and decodes an image in the background, reporting it
// back so the OP modal can render it (img is nil on any failure).
func fetchOPImageCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return opImageMsg{url: url, img: fetchImage(url)}
	}
}

func fetchImage(url string) image.Image {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil // only fetch over http(s); never file://, data://, etc.
	}
	req.Header.Set("User-Agent", browserUA)
	// No session cookie is ever attached here: a (link) post can point this URL
	// at any host, and reddit's image hosts serve public images, so there is
	// nothing to authenticate — and so nothing to leak to a third party.
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	// Read the (size-capped) body once, then check the decoded dimensions before
	// the full decode: a small file can decompress to a huge pixel buffer, and the
	// URL isn't always a trusted host (link posts can point anywhere).
	data, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20)) // cap at 25MB on the wire
	if err != nil {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	// int64 multiply so the bomb check can't be bypassed by overflow on 32-bit.
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// renderImageBlocks draws img as a grid of "▀" half-block cells (top pixel = fg,
// bottom pixel = bg), so each terminal cell shows two stacked pixels. cols sets
// the width; the height preserves aspect (two sub-pixels per row) up to maxRows.
// The result is plain styled text, so it composes with the viewport and the
// renderer like any other content.
func renderImageBlocks(img image.Image, cols, maxRows int) string {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 || cols < 1 {
		return ""
	}
	rows := cols * ih / (iw * 2) // half the height: each cell stacks two pixels
	if rows < 1 {
		rows = 1
	}
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
	}
	cell := lipgloss.NewStyle() // reused per cell (lipgloss styles copy on set)
	var sb strings.Builder
	for ry := 0; ry < rows; ry++ {
		for cx := 0; cx < cols; cx++ {
			sx := b.Min.X + cx*iw/cols
			topY := b.Min.Y + (2*ry)*ih/(rows*2)
			botY := b.Min.Y + (2*ry+1)*ih/(rows*2)
			top := hexColor(img.At(sx, topY))
			bot := hexColor(img.At(sx, botY))
			sb.WriteString(cell.Foreground(lipgloss.Color(top)).Background(lipgloss.Color(bot)).Render("▀"))
		}
		if ry < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func hexColor(c color.Color) string {
	r, g, b, _ := c.RGBA() // 16-bit per channel
	return "#" + twoHex(int(r>>8)) + twoHex(int(g>>8)) + twoHex(int(b>>8))
}
