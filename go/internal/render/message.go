package render

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"strings"

	"github.com/fogleman/gg"
)

// RenderMessage renders a full-screen centered message as an 8-bit grayscale PNG.
// Newlines in `text` are explicit paragraph breaks; long lines wrap on spaces.
// 1:1 port of _draw_wrapped_centered in kindle_dash.py.
func RenderMessage(text string) ([]byte, error) {
	const msgFontPx = 72.0
	const sideMargin = 80

	ctx := gg.NewContext(Width, Height)
	ctx.SetRGB(1, 1, 1)
	ctx.Clear()
	ctx.SetRGB(0, 0, 0)
	face := NewFace(msgFontPx)
	ctx.SetFontFace(face)

	maxW := float64(Width - 2*sideMargin)
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Split(paragraph, " ")
		cur := ""
		for _, w := range words {
			trial := strings.TrimSpace(cur + " " + w)
			tw, _ := ctx.MeasureString(trial)
			if tw <= maxW || cur == "" {
				cur = trial
			} else {
				lines = append(lines, cur)
				cur = w
			}
		}
		lines = append(lines, cur)
	}

	metr := face.Metrics()
	// match Python: line height = (ascent + descent) * 1.3
	lh := (metr.Ascent.Ceil() + metr.Descent.Ceil()) * 13 / 10
	totalH := lh * len(lines)
	y := (Height - totalH) / 2
	ascent := metr.Ascent.Ceil()
	for _, line := range lines {
		tw, _ := ctx.MeasureString(line)
		ctx.DrawString(line, (float64(Width)-tw)/2, float64(y+ascent))
		y += lh
	}

	src := ctx.Image()
	gray := image.NewGray(src.Bounds())
	draw.Draw(gray, gray.Bounds(), src, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
