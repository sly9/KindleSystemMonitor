package render

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"

	"kindle-dash/internal/metrics"
)

// Layout constants — 1:1 with kindle_dash.py L307-319.
const (
	Width  = 1072
	Height = 1448

	fVal = 104.0
	fLab = 44.0

	margin    = 36
	colW      = 4
	numTop    = 18
	numBandLo = 6
	numBandHi = 136
	labY      = 140
	chartTop  = 205
	chartBot  = 706
	chartX0   = margin
	chartX1   = Width - margin
)

var colCenters = [3]int{Width * 1 / 6, Width * 3 / 6, Width * 5 / 6}

type block struct {
	Key    string
	Y0     int
	LabCol string
	LabMem string
}

var blocks = []block{
	{Key: "cpu", Y0: 20, LabCol: "CPU", LabMem: "RAM"},
	{Key: "gpu", Y0: 744, LabCol: "GPU", LabMem: "VRAM"},
}

// Region is the bounding box of a partial-refresh push (eips -x -y on Kindle).
type Region struct{ X0, Y0, X1, Y1 int }

type Dashboard struct {
	ctx     *gg.Context
	faceVal font.Face
	faceLab font.Face
	sweepX  int
	last    map[string][3]string
}

func NewDashboard() *Dashboard {
	ctx := gg.NewContext(Width, Height)
	ctx.SetRGB(1, 1, 1)
	ctx.Clear()
	d := &Dashboard{
		ctx:     ctx,
		faceVal: NewFace(fVal),
		faceLab: NewFace(fLab),
		sweepX:  chartX0,
		last:    map[string][3]string{},
	}
	d.drawStatic()
	return d
}

func (d *Dashboard) chartRect(y0 int) (x0, cy0, x1, cy1 int) {
	return chartX0, y0 + chartTop, chartX1, y0 + chartBot
}

// ChartRegions returns the chart-area rectangles for every block. Used by the
// run loop to gc16-repaint the charts when sweep wraps around.
func (d *Dashboard) ChartRegions() []Region {
	rs := make([]Region, 0, len(blocks))
	for _, b := range blocks {
		x0, cy0, x1, cy1 := d.chartRect(b.Y0)
		rs = append(rs, Region{x0, cy0, x1, cy1})
	}
	return rs
}

// drawTopCentered draws text horizontally centered at xCenter with the visual
// top of the glyphs at yTop. This matches Pillow's anchor="ma" exactly:
// baseline = yTop + ascent. gg.DrawStringAnchored's `y` is the baseline and
// its `ay` shifts the baseline (not the top), so we can't get top-anchor
// directly from it without knowing the font's ascent fraction.
func (d *Dashboard) drawTopCentered(face font.Face, text string, xCenter, yTop int) {
	d.ctx.SetFontFace(face)
	w, _ := d.ctx.MeasureString(text)
	asc := face.Metrics().Ascent.Ceil()
	d.ctx.DrawString(text, float64(xCenter)-w/2, float64(yTop+asc))
}

func (d *Dashboard) drawStatic() {
	d.ctx.SetRGB(0, 0, 0)
	for _, b := range blocks {
		labels := [3]string{b.LabCol, "TEMP", b.LabMem}
		for i, cx := range colCenters {
			d.drawTopCentered(d.faceLab, labels[i], cx, b.Y0+labY)
		}
		x0, cy0, x1, cy1 := d.chartRect(b.Y0)
		d.ctx.SetLineWidth(2)
		d.ctx.DrawRectangle(float64(x0), float64(cy0), float64(x1-x0), float64(cy1-cy0))
		d.ctx.Stroke()
	}
}

func fmtPct(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.0f%%", *v)
}

func fmtTemp(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.0f°C", *v)
}

func numStringsFor(key string, m metrics.Metrics) [3]string {
	if key == "cpu" {
		return [3]string{fmtPct(m.CPU), fmtTemp(m.CPUTemp), fmtPct(m.Mem)}
	}
	return [3]string{fmtPct(m.GPU), fmtTemp(m.GPUTemp), fmtPct(m.VRAM)}
}

func (d *Dashboard) drawNumbers(y0 int, strs [3]string) Region {
	by0 := y0 + numBandLo
	by1 := y0 + numBandHi
	d.ctx.SetRGB(1, 1, 1)
	d.ctx.DrawRectangle(0, float64(by0), float64(Width), float64(by1-by0))
	d.ctx.Fill()

	d.ctx.SetRGB(0, 0, 0)
	for i, cx := range colCenters {
		d.drawTopCentered(d.faceVal, strs[i], cx, y0+numTop)
	}
	return Region{0, by0, Width, by1}
}

func (d *Dashboard) drawColumn(y0 int, value *float64) Region {
	_, cy0, _, cy1 := d.chartRect(y0)
	cyc := (cy0 + cy1) / 2
	half := (cy1-cy0)/2 - 3
	x := d.sweepX
	if value != nil {
		v := *value
		if v < 0 {
			v = 0
		} else if v > 100 {
			v = 100
		}
		h := int(float64(half) * v / 100.0)
		if h > 0 {
			d.ctx.SetRGB(0, 0, 0)
			d.ctx.DrawRectangle(float64(x), float64(cyc-h), float64(colW), float64(2*h))
			d.ctx.Fill()
		}
	}
	return Region{x, cy0 + 2, x + colW, cy1 - 1}
}

func (d *Dashboard) clearCharts() {
	d.ctx.SetRGB(1, 1, 1)
	for _, b := range blocks {
		x0, cy0, x1, cy1 := d.chartRect(b.Y0)
		d.ctx.DrawRectangle(float64(x0+2), float64(cy0+2), float64(x1-x0-4), float64(cy1-cy0-4))
		d.ctx.Fill()
	}
}

// Compose paints this tick's delta into the framebuffer and returns the
// regions that the transport layer should push.
func (d *Dashboard) Compose(m metrics.Metrics) (cols, nums []Region, wrapped bool) {
	if d.sweepX+colW > chartX1 {
		d.clearCharts()
		d.sweepX = chartX0
		wrapped = true
	}
	for _, b := range blocks {
		var val *float64
		if b.Key == "cpu" {
			val = m.CPU
		} else {
			val = m.GPU
		}
		cols = append(cols, d.drawColumn(b.Y0, val))
		strs := numStringsFor(b.Key, m)
		if d.last[b.Key] != strs {
			nums = append(nums, d.drawNumbers(b.Y0, strs))
			d.last[b.Key] = strs
		}
	}
	d.sweepX += colW
	return cols, nums, wrapped
}

// EncodePNG returns the framebuffer as an 8-bit grayscale PNG (matches the
// Pillow "L" mode that eips expects).
func (d *Dashboard) EncodePNG() ([]byte, error) {
	gray := d.grayImage()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePNG writes the framebuffer to path as an 8-bit grayscale PNG.
func (d *Dashboard) SavePNG(path string) error {
	gray := d.grayImage()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, gray)
}

// CropPNG returns just `r` from the framebuffer as 8-bit grayscale PNG bytes
// (used by the transport layer for partial pushes).
func (d *Dashboard) CropPNG(r Region) ([]byte, error) {
	src := d.ctx.Image()
	rect := image.Rect(r.X0, r.Y0, r.X1, r.Y1)
	gray := image.NewGray(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(gray, gray.Bounds(), src, rect.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (d *Dashboard) grayImage() *image.Gray {
	src := d.ctx.Image()
	b := src.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, src, b.Min, draw.Src)
	return gray
}
