package render

import (
	_ "embed"
	"fmt"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed assets/font.otf
var fontData []byte

var parsedFont *opentype.Font

func init() {
	f, err := opentype.Parse(fontData)
	if err != nil {
		panic(fmt.Errorf("embedded font parse: %w", err))
	}
	parsedFont = f
}

// NewFace returns a font.Face at sizePx pixels. 72 DPI makes points == pixels,
// so callers can think in pixel terms directly (matching the Python constants).
func NewFace(sizePx float64) font.Face {
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(fmt.Errorf("font face %.0fpx: %w", sizePx, err))
	}
	return face
}
