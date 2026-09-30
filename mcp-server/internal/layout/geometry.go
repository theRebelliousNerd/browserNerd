// Package layout finds spatial defects in a rendered page - overlapping
// content, text that bleeds out of or is cut by its box, horizontal overflow,
// unreachable content in fixed layers, columns that never collapse on a phone,
// small tap targets and permanently covered content - from the renderer's own
// geometry (a CDP DOMSnapshot), and checks what it can against the pixels of a
// screenshot. It holds no browser: the mcp package captures, this package
// measures, so every detector is testable on a constructed document.
package layout

import "math"

// Rect is an axis-aligned box in CSS pixels. Unless a comment says otherwise,
// coordinates are document coordinates: origin at the top-left of the
// document, unaffected by scrolling.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Right is the x coordinate of the right edge.
func (r Rect) Right() float64 { return r.X + r.W }

// Bottom is the y coordinate of the bottom edge.
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Empty reports a box with no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Area is the box's area, zero when empty.
func (r Rect) Area() float64 {
	if r.Empty() {
		return 0
	}
	return r.W * r.H
}

// Intersect is the overlap of two boxes, empty when they do not meet.
func (r Rect) Intersect(o Rect) Rect {
	x1 := math.Max(r.X, o.X)
	y1 := math.Max(r.Y, o.Y)
	x2 := math.Min(r.Right(), o.Right())
	y2 := math.Min(r.Bottom(), o.Bottom())
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// Union is the smallest box holding both; an empty box contributes nothing.
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	x1 := math.Min(r.X, o.X)
	y1 := math.Min(r.Y, o.Y)
	x2 := math.Max(r.Right(), o.Right())
	y2 := math.Max(r.Bottom(), o.Bottom())
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// ContainsTol reports whether o lies inside r, allowing tol pixels of slack on
// every side.
func (r Rect) ContainsTol(o Rect, tol float64) bool {
	return o.X >= r.X-tol && o.Y >= r.Y-tol && o.Right() <= r.Right()+tol && o.Bottom() <= r.Bottom()+tol
}

// Offset moves the box.
func (r Rect) Offset(dx, dy float64) Rect {
	return Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}
}

// Round keeps a box to one decimal place, so reports stay short and stable.
func (r Rect) Round() Rect {
	q := func(v float64) float64 { return math.Round(v*10) / 10 }
	return Rect{X: q(r.X), Y: q(r.Y), W: q(r.W), H: q(r.H)}
}

// infiniteRect is the clip of content nothing clips.
var infiniteRect = Rect{X: -1e7, Y: -1e7, W: 2e7, H: 2e7}

// clipAxis narrows clip to box on the axes that clip. An axis that does not
// clip keeps clip's extent on that axis.
func clipAxis(clip, box Rect, clipX, clipY bool) Rect {
	target := clip
	if clipX {
		x1 := math.Max(clip.X, box.X)
		x2 := math.Min(clip.Right(), box.Right())
		target.X, target.W = x1, x2-x1
	}
	if clipY {
		y1 := math.Max(clip.Y, box.Y)
		y2 := math.Min(clip.Bottom(), box.Bottom())
		target.Y, target.H = y1, y2-y1
	}
	if target.W < 0 {
		target.W = 0
	}
	if target.H < 0 {
		target.H = 0
	}
	return target
}

// outsideParts returns the parts of r that lie outside box: at most the strips
// left of, right of, above and below it.
func outsideParts(r, box Rect) []Rect {
	var parts []Rect
	in := r.Intersect(box)
	if in.Empty() {
		return []Rect{r}
	}
	if r.X < in.X {
		parts = append(parts, Rect{X: r.X, Y: r.Y, W: in.X - r.X, H: r.H})
	}
	if r.Right() > in.Right() {
		parts = append(parts, Rect{X: in.Right(), Y: r.Y, W: r.Right() - in.Right(), H: r.H})
	}
	if r.Y < in.Y {
		parts = append(parts, Rect{X: in.X, Y: r.Y, W: in.W, H: in.Y - r.Y})
	}
	if r.Bottom() > in.Bottom() {
		parts = append(parts, Rect{X: in.X, Y: in.Bottom(), W: in.W, H: r.Bottom() - in.Bottom()})
	}
	return parts
}

// largest returns the largest box of a set, empty when there is none.
func largest(rs []Rect) Rect {
	var best Rect
	for _, r := range rs {
		if r.Area() > best.Area() {
			best = r
		}
	}
	return best
}
