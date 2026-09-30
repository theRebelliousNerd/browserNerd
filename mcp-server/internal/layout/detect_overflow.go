package layout

import (
	"fmt"
	"math"
	"sort"
)

func (doc *Document) rootClipsX() bool {
	for _, n := range doc.Nodes {
		if (n.Tag == "html" || n.Tag == "body") && (n.Style.OverflowX == "hidden" || n.Style.OverflowX == "clip") {
			return true
		}
	}
	return false
}

// innerClipRight is the right edge of the nearest non-root ancestor that clips
// node i horizontally, or +Inf.
func (doc *Document) innerClip(i int) (Rect, bool) {
	for _, c := range doc.clipAncestors(i) {
		n := doc.Nodes[c]
		if n.Tag == "html" || n.Tag == "body" {
			continue
		}
		if clips(n.Style.OverflowX) {
			return doc.clientBox(c), true
		}
	}
	return Rect{}, false
}

type offender struct {
	node   int
	amount float64
	left   bool
}

func (a *Audit) detectHorizontalOverflow() {
	doc := a.Doc
	vw := doc.Viewport.Width
	rootClips := doc.rootClipsX()
	// A mobile page wider than the device widens Chrome's layout viewport and
	// loads zoomed out whatever the root's overflow says.
	widened := doc.Viewport.LayoutWidth > vw+1
	pageScrolls := doc.Viewport.DocWidth > vw+1 && (!rootClips || widened)
	if widened {
		rootClips = false
	}

	overflows := func(i int) (float64, bool) {
		n := doc.Nodes[i]
		if !n.Layout || !doc.d[i].visible || doc.d[i].fixedRoot >= 0 || n.Bounds.Area() <= 2 {
			return 0, false
		}
		if n.Tag == "html" || n.Tag == "body" || n.Tag == "#document" {
			return 0, false
		}
		if n.Tag == "#text" && len(n.TextBoxes) == 0 {
			return 0, false
		}
		r := n.Bounds
		if r.Right() <= vw+1 {
			return 0, false
		}
		if cb, ok := doc.innerClip(i); ok && cb.Right() <= vw+1 {
			return 0, false // cut or scrolled inside its own container
		}
		return r.Right() - vw, true
	}
	var offs []offender
	seenOwner := map[int]bool{}
	for i, n := range doc.Nodes {
		amount, ok := overflows(i)
		if !ok {
			continue
		}
		p := doc.element(n.Parent)
		if n.Tag == "#text" {
			p = doc.element(i)
			if p >= 0 {
				if _, parentOver := overflows(p); parentOver {
					continue
				}
			}
		} else if p >= 0 {
			if _, parentOver := overflows(p); parentOver && n.Bounds.W <= vw {
				continue
			}
		}
		if rootClips && n.Bounds.X >= vw-1 {
			continue // wholly beyond a clipped edge: parked off-canvas, not cut
		}
		owner := doc.element(i)
		if seenOwner[owner] {
			continue
		}
		seenOwner[owner] = true
		offs = append(offs, offender{node: owner, amount: amount})
	}
	// Content straddling the left edge can never be scrolled to either way.
	for i, n := range doc.Nodes {
		if !n.Layout || !doc.d[i].visible || doc.d[i].fixedRoot >= 0 || n.Tag == "#text" || n.Bounds.Area() <= 2 {
			continue
		}
		if n.Tag == "html" || n.Tag == "body" || n.Tag == "#document" {
			continue
		}
		if n.Bounds.X < -1 && n.Bounds.Right() > 1 {
			if cb, ok := doc.innerClip(i); ok && cb.X >= -1 {
				continue
			}
			p := doc.element(n.Parent)
			if p >= 0 && doc.Nodes[p].Bounds.X < -1 {
				continue
			}
			if !seenOwner[i] {
				seenOwner[i] = true
				offs = append(offs, offender{node: i, amount: -n.Bounds.X, left: true})
			}
		}
	}
	if len(offs) == 0 {
		return
	}
	sort.SliceStable(offs, func(x, y int) bool { return offs[x].amount > offs[y].amount })
	var right, left []offender
	for _, o := range offs {
		if o.left {
			left = append(left, o)
		} else {
			right = append(right, o)
		}
	}
	elemsOf := func(list []offender) ([]ElementRef, Rect) {
		var refs []ElementRef
		var region Rect
		for k, o := range list {
			b := doc.Nodes[o.node].Bounds
			part := largest(outsideParts(b, Rect{X: 0, Y: b.Y, W: vw, H: b.H}))
			region = region.Union(part)
			if k < 5 {
				refs = append(refs, a.ref(o.node))
			}
		}
		return refs, region
	}
	if len(right) > 0 {
		refs, region := elemsOf(right)
		if pageScrolls && a.Options.enabled(TypeHorizontalOverflow) {
			effect := "so it scrolls sideways"
			if widened {
				effect = "so a phone loads it zoomed out"
			}
			msg := fmt.Sprintf("the page is %s wide in a %s viewport, %s; widest cause: %s, %s past the edge",
				fmtPx(doc.Viewport.DocWidth), fmtPx(vw), effect, a.describe(right[0].node), fmtPx(right[0].amount))
			a.add(Finding{Type: TypeHorizontalOverflow, Severity: High, Message: msg, Elements: refs, Rect: region,
				Metrics: map[string]float64{"doc_width": math.Round(doc.Viewport.DocWidth), "offenders": float64(len(right))},
				cutEdge: "right", cutAt: vw})
		} else if (rootClips || !pageScrolls) && a.Options.enabled(TypeOffscreen) {
			msg := fmt.Sprintf("%s runs %s past the right edge of the screen and the page cannot scroll sideways to it", a.describe(right[0].node), fmtPx(right[0].amount))
			if len(right) > 1 {
				msg += fmt.Sprintf(" (%d elements)", len(right))
			}
			a.add(Finding{Type: TypeOffscreen, Severity: High, Message: msg, Elements: refs, Rect: region,
				Metrics: map[string]float64{"outside_px": math.Round(right[0].amount)}, cutEdge: "right", cutAt: vw})
		}
	}
	if len(left) > 0 && a.Options.enabled(TypeOffscreen) {
		refs, region := elemsOf(left)
		msg := fmt.Sprintf("%s starts %s left of the screen, where no scroll reaches it", a.describe(left[0].node), fmtPx(left[0].amount))
		a.add(Finding{Type: TypeOffscreen, Severity: High, Message: msg, Elements: refs, Rect: region,
			Metrics: map[string]float64{"outside_px": math.Round(left[0].amount)}, cutEdge: "left", cutAt: 0})
	}
}

// detectClipped reports controls and badges partly cut by an ancestor that
// does not scroll.
func (a *Audit) detectClipped() {
	doc := a.Doc
	for _, u := range a.Units {
		if u.Kind != UnitControl && u.Kind != UnitBadge {
			continue
		}
		d := doc.d[u.Node]
		if d.fixedRoot >= 0 || !d.visible || u.Rect.Area() < 16 {
			continue
		}
		frac := 1 - u.Vis.Area()/u.Rect.Area()
		if frac < 0.25 || frac > 0.98 {
			continue
		}
		clipper := -1
		side := ""
		for _, c := range doc.clipAncestors(u.Node) {
			if s, _ := cutSide(u.Rect, doc.clientBox(c)); s != "" {
				clipper, side = c, s
				break
			}
		}
		if clipper < 0 {
			continue
		}
		cn := doc.Nodes[clipper]
		if cn.Tag == "html" || cn.Tag == "body" {
			continue
		}
		overflow, extent, client := cn.Style.OverflowY, cn.ScrollH, doc.clientBox(clipper).H
		if horizontal(side) {
			overflow, extent, client = cn.Style.OverflowX, cn.ScrollW, doc.clientBox(clipper).W
		}
		if scrolls(overflow) {
			continue
		}
		if client > 0 && extent >= 2*client {
			continue // a carousel or marquee track
		}
		hidden := largest(outsideParts(u.Rect, doc.clientBox(clipper)))
		msg := fmt.Sprintf("%s is %d%% cut off at the %s by %s (overflow %s)", a.describe(u.Node), int(math.Round(frac*100)), side, a.describe(clipper), overflow)
		a.add(Finding{Type: TypeClipped, Severity: Medium, Message: msg, Elements: []ElementRef{a.ref(u.Node), a.ref(clipper)},
			Rect: hidden, Metrics: map[string]float64{"hidden_fraction": math.Round(frac*100) / 100}, units: []int{u.ID}})
	}
}
