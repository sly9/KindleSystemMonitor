package render

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"math/rand"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
)

// Frame is a rendered animation frame with push and timing metadata.
type Frame struct {
	PNG      []byte
	Hold     time.Duration
	Waveform string
	Clear    bool
}

// Cockpit chrome layout
const (
	ckBord    = 30   // outer border margin (px)
	ckHdrY    = 110  // y of header/content divider
	ckFtrY    = 1338 // y of content/footer divider
	ckBootTop = 148  // y where first boot line starts
	ckBootLH  = 52   // px per boot log line
	ckSepY    = 392  // y of separator between boot-log and message zone
	ckMsgTop  = 412  // y top of message zone
	ckMsgBot  = 1320 // y bottom of message zone

	ckSzHdr  = 36.0
	ckSzBoot = 30.0
	ckSzMsg  = 62.0
	ckSzFtr  = 26.0
)

var ckBoot = [4]string{
	"  >>  INITIALIZING SENSOR ARRAY  ............  OK",
	"  >>  DISPLAY MATRIX  ....................... OK",
	"  >>  DATA UPLINK  ......................... OK",
	"  >>  ALL SYSTEMS NOMINAL  ................. READY",
}

var ckBootOff = [4]string{
	"  >>  SENSOR ARRAY  ..................... OFFLINE",
	"  >>  DISPLAY MATRIX  .................. STANDBY",
	"  >>  DATA UPLINK  ..................... CLOSED",
	"  >>  ALL SYSTEMS  ..................... SHUTDOWN",
}

const ckGlitchPool = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#@!%&*<>?/|=+-"

type ckFaces struct{ hdr, boot, msg, ftr font.Face }

func newCKFaces() ckFaces {
	return ckFaces{
		hdr:  NewFace(ckSzHdr),
		boot: NewFace(ckSzBoot),
		msg:  NewFace(ckSzMsg),
		ftr:  NewFace(ckSzFtr),
	}
}

// CockpitIntroFrames builds the boot-sequence animation for the welcome screen.
// Frame holds are sized so total animation time ≈ totalDur.
func CockpitIntroFrames(msgLines []string, totalDur time.Duration) ([]Frame, error) {
	f := newCKFaces()

	// Estimated push overhead for 7 frames: 2×gc16 (~1.5s each) + 5×du (~0.5s each) ≈ 5.5s
	const pushOverhead = 5500 * time.Millisecond
	const fixedHolds = (450 + 450 + 450 + 600 + 800) * time.Millisecond
	finalHold := totalDur - pushOverhead - fixedHolds
	if finalHold < 500*time.Millisecond {
		finalHold = 500 * time.Millisecond
	}

	type spec struct {
		boots   int
		showMsg bool
		footer  string
		hold    time.Duration
		wf      string
		clear   bool
	}
	specs := []spec{
		{0, false, "INITIALIZING...   [          ]   0%", 0, "gc16", true},
		{1, false, "LOADING...        [##        ]  25%", 450 * time.Millisecond, "du", false},
		{2, false, "LOADING...        [####      ]  50%", 450 * time.Millisecond, "du", false},
		{3, false, "LOADING...        [######    ]  75%", 450 * time.Millisecond, "du", false},
		{4, false, "LOADING...        [########  ]  90%", 600 * time.Millisecond, "du", false},
		{4, true, "VERIFYING...      [##########]  100%", 800 * time.Millisecond, "du", false},
		{4, true, "STATUS: ONLINE    SYS.MON v1.0", finalHold, "gc16", false},
	}

	var frames []Frame
	for _, s := range specs {
		png, err := renderIntroFrame(s.boots, s.showMsg, msgLines, s.footer, f)
		if err != nil {
			return nil, err
		}
		frames = append(frames, Frame{PNG: png, Hold: s.hold, Waveform: s.wf, Clear: s.clear})
	}
	return frames, nil
}

// CockpitOutroFrames builds the shutdown animation for the farewell screen.
func CockpitOutroFrames(msgLines []string) ([]Frame, error) {
	f := newCKFaces()
	rng := rand.New(rand.NewSource(7331))

	type spec struct {
		reveal float64 // 0=full scramble, 1=clean; -1 = final offline frame
		footer string
		hold   time.Duration
		wf     string
		clear  bool
	}
	specs := []spec{
		{1.0, "STATUS: ONLINE    SYS.MON v1.0", 800 * time.Millisecond, "gc16", true},
		{0.65, "DISCONNECTING...  [######    ]  60%", 500 * time.Millisecond, "du", false},
		{0.25, "SHUTTING DOWN...  [##        ]  20%", 500 * time.Millisecond, "du", false},
		{-1, "STATUS: OFFLINE   SYSTEM SHUTDOWN", 0, "gc16", false},
	}

	var frames []Frame
	for _, s := range specs {
		var (
			pngBytes []byte
			err      error
		)
		if s.reveal < 0 {
			pngBytes, err = renderOutroFinal(msgLines, s.footer, f)
		} else {
			pngBytes, err = renderOutroFrame(s.reveal, msgLines, s.footer, f, rng)
		}
		if err != nil {
			return nil, err
		}
		frames = append(frames, Frame{PNG: pngBytes, Hold: s.hold, Waveform: s.wf, Clear: s.clear})
	}
	return frames, nil
}

func renderIntroFrame(numBoots int, showMsg bool, msgLines []string, footer string, f ckFaces) ([]byte, error) {
	ctx := gg.NewContext(Width, Height)
	ctx.SetRGB(1, 1, 1)
	ctx.Clear()
	ctx.SetRGB(0, 0, 0)

	drawCKChrome(ctx, "<< MOBILE SUIT SYSTEM MONITOR >>", footer, f)

	if numBoots > 0 {
		ctx.SetFontFace(f.boot)
		asc := f.boot.Metrics().Ascent.Ceil()
		for i := 0; i < numBoots && i < len(ckBoot); i++ {
			ctx.DrawString(ckBoot[i], float64(ckBord+40), float64(ckBootTop+i*ckBootLH+asc))
		}
	}

	if showMsg {
		drawCKSep(ctx)
		drawCKMsg(ctx, msgLines, f.msg)
	}

	return encodeCKGray(ctx)
}

func renderOutroFrame(reveal float64, msgLines []string, footer string, f ckFaces, rng *rand.Rand) ([]byte, error) {
	ctx := gg.NewContext(Width, Height)
	ctx.SetRGB(1, 1, 1)
	ctx.Clear()
	ctx.SetRGB(0, 0, 0)

	drawCKChrome(ctx,
		ckGlitch("<< MOBILE SUIT SYSTEM MONITOR >>", reveal, rng),
		ckGlitch(footer, reveal, rng),
		f,
	)

	ctx.SetFontFace(f.boot)
	asc := f.boot.Metrics().Ascent.Ceil()
	for i, line := range ckBoot {
		ctx.DrawString(ckGlitch(line, reveal, rng), float64(ckBord+40), float64(ckBootTop+i*ckBootLH+asc))
	}

	drawCKSep(ctx)
	drawCKMsg(ctx, ckGlitchLines(msgLines, reveal, rng), f.msg)
	return encodeCKGray(ctx)
}

func renderOutroFinal(msgLines []string, footer string, f ckFaces) ([]byte, error) {
	ctx := gg.NewContext(Width, Height)
	ctx.SetRGB(1, 1, 1)
	ctx.Clear()
	ctx.SetRGB(0, 0, 0)

	drawCKChrome(ctx, "<< SYSTEM OFFLINE >>", footer, f)

	ctx.SetFontFace(f.boot)
	asc := f.boot.Metrics().Ascent.Ceil()
	for i, line := range ckBootOff {
		ctx.DrawString(line, float64(ckBord+40), float64(ckBootTop+i*ckBootLH+asc))
	}

	drawCKSep(ctx)
	drawCKMsg(ctx, msgLines, f.msg)
	return encodeCKGray(ctx)
}

// drawCKChrome draws the border, tick marks, dividers, header, and footer.
func drawCKChrome(ctx *gg.Context, header, footer string, f ckFaces) {
	// outer border
	ctx.SetLineWidth(3)
	ctx.DrawRectangle(float64(ckBord), float64(ckBord),
		float64(Width-2*ckBord), float64(Height-2*ckBord))
	ctx.Stroke()

	// corner squares
	for _, cx := range []float64{float64(ckBord), float64(Width - ckBord)} {
		for _, cy := range []float64{float64(ckBord), float64(Height - ckBord)} {
			ctx.DrawRectangle(cx-6, cy-6, 12, 12)
			ctx.Fill()
		}
	}

	// tick marks along top and bottom borders
	ctx.SetLineWidth(2)
	for x := ckBord + 100; x < Width-ckBord; x += 100 {
		ctx.DrawLine(float64(x), float64(ckBord), float64(x), float64(ckBord+9))
		ctx.Stroke()
		ctx.DrawLine(float64(x), float64(Height-ckBord-9), float64(x), float64(Height-ckBord))
		ctx.Stroke()
	}
	// tick marks along left and right borders
	for y := ckBord + 120; y < Height-ckBord; y += 120 {
		ctx.DrawLine(float64(ckBord), float64(y), float64(ckBord+9), float64(y))
		ctx.Stroke()
		ctx.DrawLine(float64(Width-ckBord-9), float64(y), float64(Width-ckBord), float64(y))
		ctx.Stroke()
	}

	// header and footer dividers
	ctx.DrawLine(float64(ckBord), float64(ckHdrY), float64(Width-ckBord), float64(ckHdrY))
	ctx.Stroke()
	ctx.DrawLine(float64(ckBord), float64(ckFtrY), float64(Width-ckBord), float64(ckFtrY))
	ctx.Stroke()

	// header text — vertically centered in [ckBord, ckHdrY]
	drawCKBandText(ctx, f.hdr, header, ckBord, ckHdrY)

	// footer text — vertically centered in [ckFtrY, Height-ckBord]
	drawCKBandText(ctx, f.ftr, footer, ckFtrY, Height-ckBord)
}

// drawCKBandText draws text horizontally and vertically centered in [yTop, yBot].
func drawCKBandText(ctx *gg.Context, face font.Face, text string, yTop, yBot int) {
	ctx.SetFontFace(face)
	metr := face.Metrics()
	asc := metr.Ascent.Ceil()
	desc := metr.Descent.Ceil()
	bandH := yBot - yTop
	blockTop := yTop + (bandH-(asc+desc))/2
	w, _ := ctx.MeasureString(text)
	ctx.DrawString(text, (float64(Width)-w)/2, float64(blockTop+asc))
}

func drawCKSep(ctx *gg.Context) {
	ctx.SetLineWidth(1)
	ctx.DrawLine(float64(ckBord+50), float64(ckSepY), float64(Width-ckBord-50), float64(ckSepY))
	ctx.Stroke()
}

// drawCKMsg draws lines centered horizontally and vertically in the message zone.
func drawCKMsg(ctx *gg.Context, lines []string, face font.Face) {
	ctx.SetFontFace(face)
	metr := face.Metrics()
	lh := (metr.Ascent.Ceil() + metr.Descent.Ceil()) * 13 / 10
	totalH := lh * len(lines)
	avail := ckMsgBot - ckMsgTop
	y := ckMsgTop + (avail-totalH)/2
	asc := metr.Ascent.Ceil()
	for _, line := range lines {
		w, _ := ctx.MeasureString(line)
		ctx.DrawString(line, (float64(Width)-w)/2, float64(y+asc))
		y += lh
	}
}

// ckGlitch randomly replaces characters with glitch chars.
// Non-ASCII runes (e.g. Chinese) are always preserved to avoid width mismatches.
func ckGlitch(s string, reveal float64, rng *rand.Rand) string {
	pool := []byte(ckGlitchPool)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\n':
			b.WriteRune(r)
		case r > 127:
			b.WriteRune(r) // preserve non-ASCII as-is
		default:
			if rng.Float64() < reveal {
				b.WriteRune(r)
			} else {
				b.WriteByte(pool[rng.Intn(len(pool))])
			}
		}
	}
	return b.String()
}

func ckGlitchLines(lines []string, reveal float64, rng *rand.Rand) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ckGlitch(l, reveal, rng)
	}
	return out
}

func encodeCKGray(ctx *gg.Context) ([]byte, error) {
	src := ctx.Image()
	gray := image.NewGray(src.Bounds())
	draw.Draw(gray, gray.Bounds(), src, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
