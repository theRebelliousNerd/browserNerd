package layout

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// edgeThreshold is the luminance step (0-255) between neighbouring pixels that
// counts as an edge: glyph strokes, borders and icon outlines clear it on any
// background, flat fills and gentle gradients do not.
const edgeThreshold = 40

// Shot is a screenshot and where it sits in the document: pixel (0,0) is the
// document point (OriginX, OriginY), and Scale image pixels span one CSS pixel.
type Shot struct {
	Img     image.Image
	OriginX float64
	OriginY float64
	Scale   float64

	w, h int
	lum  []uint8
	edge []bool
}

// NewShot prepares a screenshot for the CV pass.
func NewShot(img image.Image, originX, originY, scale float64) *Shot {
	b := img.Bounds()
	s := &Shot{Img: img, OriginX: originX, OriginY: originY, Scale: scale, w: b.Dx(), h: b.Dy()}
	s.lum = make([]uint8, s.w*s.h)
	// Rec. 601 luma on 8-bit channels, integer arithmetic.
	luma := func(r, g, bl uint32) uint8 { return uint8((299*r + 587*g + 114*bl) / 1000) }
	var probe image.Image = img
	if b.Min != (image.Point{}) {
		probe = nil // the fast paths index Pix from the origin
	}
	switch src := probe.(type) {
	case *image.RGBA:
		for y := 0; y < s.h; y++ {
			row := src.Pix[y*src.Stride:]
			for x := 0; x < s.w; x++ {
				s.lum[y*s.w+x] = luma(uint32(row[4*x]), uint32(row[4*x+1]), uint32(row[4*x+2]))
			}
		}
	case *image.NRGBA:
		for y := 0; y < s.h; y++ {
			row := src.Pix[y*src.Stride:]
			for x := 0; x < s.w; x++ {
				s.lum[y*s.w+x] = luma(uint32(row[4*x]), uint32(row[4*x+1]), uint32(row[4*x+2]))
			}
		}
	default:
		for y := 0; y < s.h; y++ {
			for x := 0; x < s.w; x++ {
				r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				s.lum[y*s.w+x] = luma(r>>8, g>>8, bl>>8)
			}
		}
	}
	s.edge = make([]bool, s.w*s.h)
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			l := int(s.lum[y*s.w+x])
			if x+1 < s.w && absI(l-int(s.lum[y*s.w+x+1])) > edgeThreshold {
				s.edge[y*s.w+x] = true
				continue
			}
			if y+1 < s.h && absI(l-int(s.lum[(y+1)*s.w+x])) > edgeThreshold {
				s.edge[y*s.w+x] = true
			}
		}
	}
	return s
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Bounds is the document rect the image covers.
func (s *Shot) Bounds() Rect {
	return Rect{X: s.OriginX, Y: s.OriginY, W: float64(s.w) / s.Scale, H: float64(s.h) / s.Scale}
}

// pixels maps a document rect to image pixels, clamped to the image.
func (s *Shot) pixels(r Rect) image.Rectangle {
	x1 := int(math.Floor((r.X - s.OriginX) * s.Scale))
	y1 := int(math.Floor((r.Y - s.OriginY) * s.Scale))
	x2 := int(math.Ceil((r.Right() - s.OriginX) * s.Scale))
	y2 := int(math.Ceil((r.Bottom() - s.OriginY) * s.Scale))
	return image.Rect(x1, y1, x2, y2).Intersect(image.Rect(0, 0, s.w, s.h))
}

// EdgeDensity is the fraction of edge pixels in a document rect, and false
// when the rect is off the image or too small to judge.
func (s *Shot) EdgeDensity(r Rect) (float64, bool) {
	p := s.pixels(r)
	if p.Dx() < 2 || p.Dy() < 2 {
		return 0, false
	}
	n := 0
	for y := p.Min.Y; y < p.Max.Y; y++ {
		row := y * s.w
		for x := p.Min.X; x < p.Max.X; x++ {
			if s.edge[row+x] {
				n++
			}
		}
	}
	return float64(n) / float64(p.Dx()*p.Dy()), true
}

// lineEdges counts edge pixels on the one-pixel line just inside a cut: the
// column at document x (vertical line) over [y1,y2], or the row at document y.
func (s *Shot) lineEdges(vertical bool, at, from, to float64) (int, int) {
	if vertical {
		x := int(math.Floor((at - s.OriginX) * s.Scale))
		y1 := int(math.Floor((from - s.OriginY) * s.Scale))
		y2 := int(math.Ceil((to - s.OriginY) * s.Scale))
		if x < 0 || x >= s.w {
			return 0, 0
		}
		n, total := 0, 0
		for y := maxI(y1, 0); y < minI(y2, s.h); y++ {
			total++
			if s.edge[y*s.w+x] || (x > 0 && s.edge[y*s.w+x-1]) {
				n++
			}
		}
		return n, total
	}
	y := int(math.Floor((at - s.OriginY) * s.Scale))
	x1 := int(math.Floor((from - s.OriginX) * s.Scale))
	x2 := int(math.Ceil((to - s.OriginX) * s.Scale))
	if y < 0 || y >= s.h {
		return 0, 0
	}
	n, total := 0, 0
	for x := maxI(x1, 0); x < minI(x2, s.w); x++ {
		total++
		if s.edge[y*s.w+x] || (y > 0 && s.edge[(y-1)*s.w+x]) {
			n++
		}
	}
	return n, total
}

// ownPart is the largest part of a unit's text (or box) outside region, where
// its own drawing can be measured alone.
func ownPart(u Unit, region Rect) Rect {
	var parts []Rect
	src := u.Texts
	if len(src) == 0 {
		src = []Rect{u.Rect}
	}
	for _, t := range src {
		parts = append(parts, outsideParts(t.Intersect(u.Vis), region)...)
	}
	return largest(parts)
}

// ApplyCV checks the findings against the screenshot and adds edge_cut
// findings the DOM did not explain. Call it between Run and Finish.
func (a *Audit) ApplyCV(s *Shot) {
	if s == nil {
		return
	}
	img := s.Bounds()
	for k := range a.Findings {
		f := &a.Findings[k]
		// A cut is judged at its edge, which is on screen even when the part
		// cut away is not.
		probe := f.Rect
		switch f.cutEdge {
		case "left", "right":
			probe = Rect{X: f.cutAt - 2, Y: f.Rect.Y, W: 4, H: f.Rect.H}
		case "top", "bottom":
			probe = Rect{X: f.Rect.X, Y: f.cutAt - 2, W: f.Rect.W, H: 4}
		}
		if probe.Intersect(img).Area() < 1 {
			f.CV = CVNotInView
			continue
		}
		switch f.Type {
		case TypeOverlap:
			a.cvOverlap(s, f)
		case TypeTextBleed:
			a.cvBleed(s, f)
		case TypeTextClipped, TypeOffscreen, TypeHorizontalOverflow:
			a.cvCut(s, f)
		}
	}
	if a.Options.enabled(TypeEdgeCut) {
		a.cvEdgeScan(s)
	}
}

func (a *Audit) cvOverlap(s *Shot, f *Finding) {
	dI, ok := s.EdgeDensity(f.Rect)
	if !ok {
		return
	}
	if dI < 0.015 {
		f.CV = CVNoInk
		f.Severity = Low
		f.Message += "; nothing is drawn where they meet"
		return
	}
	// The two units' own densities, measured away from the overlap.
	byNode := map[int]Unit{}
	for _, id := range f.units {
		u := a.Units[id]
		if _, ok := byNode[u.Node]; !ok {
			byNode[u.Node] = u
		}
	}
	best := 0.0
	measured := 0
	for _, u := range byNode {
		part := ownPart(u, f.Rect)
		if part.Area() < 16 {
			continue
		}
		if d, ok := s.EdgeDensity(part); ok {
			measured++
			if d > best {
				best = d
			}
		}
	}
	if measured == 0 || dI >= 1.35*best {
		f.CV = CVCollision
		return
	}
	f.CV = CVCovered
}

func (a *Audit) cvBleed(s *Shot, f *Finding) {
	d, ok := s.EdgeDensity(f.Rect)
	if !ok {
		return
	}
	if d < 0.01 {
		f.CV = CVNoInk
		f.Severity = Low
		f.Message += "; the part outside draws nothing"
		return
	}
	f.CV = CVBleedVisible
}

// cvCut samples the line of pixels just inside the cutting edge over the cut
// region: glyphs sliced there show as edges.
func (a *Audit) cvCut(s *Shot, f *Finding) {
	edge, at := f.cutEdge, f.cutAt
	if edge == "" {
		return
	}
	var n, total int
	switch edge {
	case "right":
		n, total = s.lineEdges(true, at-1, f.Rect.Y, f.Rect.Bottom())
	case "left":
		n, total = s.lineEdges(true, at+1, f.Rect.Y, f.Rect.Bottom())
	case "bottom":
		n, total = s.lineEdges(false, at-1, f.Rect.X, f.Rect.Right())
	case "top":
		n, total = s.lineEdges(false, at+1, f.Rect.X, f.Rect.Right())
	}
	if total == 0 {
		return
	}
	if n >= 2 {
		f.CV = CVCutVisible
		return
	}
	f.CV = CVCleanEdge
	if f.Type == TypeTextClipped {
		f.Severity = Low
		f.Message += "; no glyph is sliced at the edge"
	}
}

// cvEdgeScan looks down the viewport's left and right edges for runs of
// glyph-like edges - many short luminance steps along the edge column - that
// no DOM finding explains.
func (a *Audit) cvEdgeScan(s *Shot) {
	vp := a.Doc.Viewport
	vr := vp.Rect()
	img := s.Bounds()
	if !img.ContainsTol(vr, 1) {
		return // only a viewport capture shows the screen's edges
	}
	scrollbar := 0.0
	for _, n := range a.Doc.Nodes {
		if n.Tag == "html" && n.HasClient {
			scrollbar = math.Max(0, vp.Width-n.Client.W)
		}
	}
	explained := func(r Rect) bool {
		for _, f := range a.Findings {
			if f.Type == TypeEdgeCut {
				continue
			}
			if !f.Rect.Intersect(Rect{X: r.X - 4, Y: r.Y, W: r.W + 8, H: r.H}).Empty() {
				return true
			}
		}
		return false
	}
	for _, side := range []string{"left", "right"} {
		x := vr.X + 0.5
		if side == "right" {
			x = vr.Right() - scrollbar - 1.5
		}
		col := int(math.Floor((x - s.OriginX) * s.Scale))
		if col < 1 || col >= s.w-1 {
			continue
		}
		// Transitions along the column: glyphs alternate ink and gap.
		var runs [][2]int
		start, last := -1, -1
		gap := int(math.Ceil(6 * s.Scale))
		for y := 0; y < s.h; y++ {
			l := int(s.lum[y*s.w+col])
			step := y+1 < s.h && absI(l-int(s.lum[(y+1)*s.w+col])) > edgeThreshold
			if !step {
				continue
			}
			if start >= 0 && y-last <= gap {
				last = y
				continue
			}
			if start >= 0 {
				runs = append(runs, [2]int{start, last})
			}
			start, last = y, y
		}
		if start >= 0 {
			runs = append(runs, [2]int{start, last})
		}
		for _, run := range runs {
			steps := 0
			for y := run[0]; y <= run[1]; y++ {
				l := int(s.lum[y*s.w+col])
				if y+1 < s.h && absI(l-int(s.lum[(y+1)*s.w+col])) > edgeThreshold {
					steps++
				}
			}
			hCSS := float64(run[1]-run[0]+1) / s.Scale
			if steps < 4 || hCSS < 6 || hCSS > 80 {
				continue
			}
			y := s.OriginY + float64(run[0])/s.Scale
			r := Rect{X: x - 3, Y: y, W: 6, H: hCSS}
			if side == "right" {
				r.X = x - 5
			}
			if explained(r) {
				continue
			}
			under := a.unitAt(x, y+hCSS/2)
			if under < 0 {
				continue
			}
			u := a.Units[under]
			if a.Doc.inHorizontalScroller(u.Node) {
				continue // a strip that scrolls sideways is cut at the edge on purpose
			}
			if u.Kind == UnitImage || a.Doc.Nodes[u.Node].Style.BackgroundImage != "" && a.Doc.Nodes[u.Node].Style.BackgroundImage != "none" {
				continue
			}
			msg := fmt.Sprintf("glyphs are sliced by the %s edge of the screen at y=%s, in %s", side, fmtPx(y), a.describe(u.Node))
			a.add(Finding{Type: TypeEdgeCut, Severity: Medium, Message: msg, Elements: []ElementRef{a.ref(u.Node)}, Rect: r,
				CV: CVOnly, InView: true})
		}
	}
}

// unitAt is the topmost unit whose visible box holds the document point.
func (a *Audit) unitAt(x, y float64) int {
	best, bestPaint := -1, -1
	var hits []int
	for _, u := range a.Units {
		v := u.Vis
		if x >= v.X-2 && x <= v.Right()+2 && y >= v.Y && y <= v.Bottom() {
			hits = append(hits, u.ID)
		}
	}
	sort.Ints(hits)
	for _, id := range hits {
		if p := a.Units[id].Paint; p >= bestPaint {
			best, bestPaint = id, p
		}
	}
	return best
}
