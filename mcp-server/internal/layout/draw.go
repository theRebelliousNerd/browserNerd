package layout

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

// Overlay colours for annotated screenshots.
var (
	ColorFirst  = color.RGBA{230, 0, 0, 255}   // the first element of a finding
	ColorSecond = color.RGBA{0, 90, 255, 255}  // the second element
	ColorRegion = color.RGBA{255, 0, 200, 255} // the problem region
	ColorLow    = color.RGBA{255, 150, 0, 255} // a low-severity finding's badge
)

// ToRGBA copies an image into a drawable RGBA.
func ToRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// DrawRect draws a rectangle outline of the given thickness, clipped to the
// image.
func DrawRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA, thickness int) {
	bounds := img.Bounds()
	for t := 0; t < thickness; t++ {
		for x := x1; x < x2; x++ {
			if y1+t >= 0 && y1+t < bounds.Max.Y && x >= 0 && x < bounds.Max.X {
				img.SetRGBA(x, y1+t, c)
			}
			if y2-1-t >= 0 && y2-1-t < bounds.Max.Y && x >= 0 && x < bounds.Max.X {
				img.SetRGBA(x, y2-1-t, c)
			}
		}
		for y := y1; y < y2; y++ {
			if x1+t >= 0 && x1+t < bounds.Max.X && y >= 0 && y < bounds.Max.Y {
				img.SetRGBA(x1+t, y, c)
			}
			if x2-1-t >= 0 && x2-1-t < bounds.Max.X && y >= 0 && y < bounds.Max.Y {
				img.SetRGBA(x2-1-t, y, c)
			}
		}
	}
}

// FillRect blends a colour over a rectangle at the given opacity (0-255).
func FillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA, alpha uint8) {
	r := image.Rect(x1, y1, x2, y2).Intersect(img.Bounds())
	a := uint32(alpha)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			o := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((uint32(c.R)*a + uint32(o.R)*(255-a)) / 255),
				G: uint8((uint32(c.G)*a + uint32(o.G)*(255-a)) / 255),
				B: uint8((uint32(c.B)*a + uint32(o.B)*(255-a)) / 255),
				A: 255,
			})
		}
	}
}

// DrawNumberBadge draws a filled badge holding num just above (x, y), or just
// inside when there is no room above.
func DrawNumberBadge(img *image.RGBA, x, y, num int, boxColor color.RGBA) {
	bounds := img.Bounds()
	numStr := fmt.Sprintf("%d", num)
	charWidth, charHeight, padding := 6, 9, 2
	badgeWidth := len(numStr)*charWidth + padding*2
	badgeHeight := charHeight + padding*2
	badgeX := x
	badgeY := y - badgeHeight
	if badgeY < 0 {
		badgeY = y
	}
	if badgeX+badgeWidth > bounds.Max.X {
		badgeX = bounds.Max.X - badgeWidth
	}
	if badgeX < 0 {
		badgeX = 0
	}
	for by := badgeY; by < badgeY+badgeHeight && by < bounds.Max.Y; by++ {
		for bx := badgeX; bx < badgeX+badgeWidth && bx < bounds.Max.X; bx++ {
			if bx >= 0 && by >= 0 {
				img.SetRGBA(bx, by, boxColor)
			}
		}
	}
	white := color.RGBA{255, 255, 255, 255}
	for i, ch := range numStr {
		drawDigit(img, badgeX+padding+i*charWidth, badgeY+padding, int(ch-'0'), white)
	}
}

// 5x7 bitmap font for digits 0-9.
var digitPatterns = [10][7]uint8{
	{0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E}, // 0
	{0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E}, // 1
	{0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F}, // 2
	{0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E}, // 3
	{0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02}, // 4
	{0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E}, // 5
	{0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E}, // 6
	{0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08}, // 7
	{0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E}, // 8
	{0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C}, // 9
}

func drawDigit(img *image.RGBA, x, y, digit int, c color.RGBA) {
	if digit < 0 || digit > 9 {
		return
	}
	bounds := img.Bounds()
	pattern := digitPatterns[digit]
	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			if pattern[row]&(1<<(4-col)) != 0 {
				px, py := x+col, y+row
				if px >= 0 && px < bounds.Max.X && py >= 0 && py < bounds.Max.Y {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
}

// Annotate draws the numbered findings onto out, an image whose pixel (0,0) is
// the document point (originX, originY) at scale image pixels per CSS pixel:
// the problem region filled, the first element outlined red, the second blue,
// and the finding's number in a badge. It returns how many findings it drew.
func (a *Audit) Annotate(out *image.RGBA, originX, originY, scale float64) int {
	s := &Shot{OriginX: originX, OriginY: originY, Scale: scale, w: out.Bounds().Dx(), h: out.Bounds().Dy()}
	px := func(r Rect) (int, int, int, int) {
		p := s.pixels(r)
		return p.Min.X, p.Min.Y, p.Max.X, p.Max.Y
	}
	thick := int(math.Max(2, math.Round(scale)))
	img := s.Bounds()
	drawn := 0
	// Later numbers first, so the most severe findings end on top.
	for k := len(a.Findings) - 1; k >= 0; k-- {
		f := a.Findings[k]
		if f.Rect.Intersect(img).Area() < 1 {
			continue
		}
		drawn++
		x1, y1, x2, y2 := px(f.Rect)
		FillRect(out, x1, y1, x2, y2, ColorRegion, 90)
		for e, el := range f.Elements {
			if e > 1 || el.Rect.Intersect(img).Empty() {
				continue
			}
			c := ColorFirst
			if e == 1 {
				c = ColorSecond
			}
			ex1, ey1, ex2, ey2 := px(el.Rect)
			DrawRect(out, ex1, ey1, ex2, ey2, c, thick)
		}
		badge := ColorFirst
		if f.Severity == Low {
			badge = ColorLow
		}
		DrawNumberBadge(out, x1, y1, f.N, badge)
	}
	return drawn
}

// Downscale shrinks an image by area averaging so its width is at most
// maxWidth, returning the image and the factor applied (1 when unchanged).
func Downscale(img *image.RGBA, maxWidth int) (*image.RGBA, float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= maxWidth || maxWidth <= 0 {
		return img, 1
	}
	f := float64(maxWidth) / float64(w)
	nw, nh := maxWidth, int(math.Round(float64(h)*f))
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	inv := 1 / f
	for y := 0; y < nh; y++ {
		sy1, sy2 := int(float64(y)*inv), int(math.Ceil(float64(y+1)*inv))
		if sy2 > h {
			sy2 = h
		}
		for x := 0; x < nw; x++ {
			sx1, sx2 := int(float64(x)*inv), int(math.Ceil(float64(x+1)*inv))
			if sx2 > w {
				sx2 = w
			}
			var r, g, b, n uint32
			for yy := sy1; yy < sy2; yy++ {
				for xx := sx1; xx < sx2; xx++ {
					c := img.RGBAAt(xx, yy)
					r += uint32(c.R)
					g += uint32(c.G)
					b += uint32(c.B)
					n++
				}
			}
			if n == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
		}
	}
	return out, f
}
