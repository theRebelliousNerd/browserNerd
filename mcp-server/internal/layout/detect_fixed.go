package layout

import (
	"fmt"
	"math"
	"strings"
)

// fixedRoots lists the outermost viewport-fixed elements that are visible in
// the viewport.
func (doc *Document) fixedRoots() []int {
	vr := doc.Viewport.Rect()
	var out []int
	for i := range doc.Nodes {
		if doc.d[i].fixedRoot != i || !doc.d[i].visible {
			continue
		}
		if doc.Nodes[i].Bounds.Intersect(vr).Area() < 4 {
			continue // parked off-screen: a closed drawer
		}
		out = append(out, i)
	}
	return out
}

// AnchorCandidates lists the fixed elements, and the scroll containers inside
// them, whose authored anchoring decides a toolbar_cutoff: those reaching into
// the modeled toolbar strip. Empty unless the viewport is mobile with a
// toolbar height.
func (doc *Document) AnchorCandidates() []int {
	vp := doc.Viewport
	if !vp.Mobile || vp.ToolbarHeight <= 0 {
		return nil
	}
	doc.Analyze()
	band := vp.ScrollY + vp.Height - vp.ToolbarHeight
	seen := map[int]bool{}
	var out []int
	add := func(i int) {
		if i >= 0 && !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for _, f := range doc.fixedRoots() {
		if doc.Nodes[f].Bounds.Bottom() <= band+1 {
			continue
		}
		add(f)
		for i := f + 1; i < len(doc.Nodes) && doc.IsAncestor(f, i); i++ {
			n := doc.Nodes[i]
			if n.Tag != "#text" && scrolls(n.Style.OverflowY) && n.ScrollH > n.Client.H+1 {
				add(i)
			}
		}
	}
	return out
}

// internalWindow is the part of a unit that the non-scrolling clips inside its
// fixed layer let through: a unit inside a collapsed section is hidden on
// purpose, not unreachable. A clip that is the layer itself, or that reaches
// the viewport's edge, is not a deliberate hiding place: content past it is
// lost, and the reachability check reports it.
func (doc *Document) internalWindow(i, root int, r, vr Rect) Rect {
	for _, c := range doc.clipAncestors(i) {
		n := doc.Nodes[c]
		cx := clips(n.Style.OverflowX) && !scrolls(n.Style.OverflowX)
		cy := clips(n.Style.OverflowY) && !scrolls(n.Style.OverflowY)
		if !cx && !cy {
			continue
		}
		box := doc.clientBox(c)
		if c == root || box.Bottom() >= vr.Bottom()-1 || box.Right() >= vr.Right()-1 {
			continue
		}
		r = clipAxis(r, box, cx, cy)
	}
	return r
}

// reachable reports whether a unit can be scrolled wholly into window (a
// document-coordinate rect) by its nearest vertical scroll container, or sits
// in it already.
func (doc *Document) reachable(u Unit, window Rect) bool {
	r := u.Rect
	if window.ContainsTol(r, 2) {
		return true
	}
	if r.Right() > window.Right()+2 || r.X < window.X-2 {
		// Beside the window: only a horizontal scroller could bring it in.
		for _, c := range doc.clipAncestors(u.Node) {
			if scrolls(doc.Nodes[c].Style.OverflowX) {
				return true
			}
		}
		return false
	}
	s := doc.scrollContainerY(u.Node)
	if s < 0 {
		return false
	}
	sn := doc.Nodes[s]
	client := doc.clientBox(s)
	win := client.Intersect(window)
	if win.Empty() {
		return false
	}
	cur := sn.ScrollTop
	maxS := math.Max(0, sn.ScrollH-client.H)
	need := math.Min(r.H, win.H)
	lo := math.Max(r.Y+need-win.Bottom(), -cur)
	hi := math.Min(r.Y-win.Y, maxS-cur)
	if r.H > win.H {
		hi = math.Min(r.Bottom()-need-win.Y, maxS-cur)
	}
	return lo <= hi+1
}

func (a *Audit) fixedUnits(f int) []Unit {
	var out []Unit
	for _, u := range a.Units {
		if a.Doc.d[u.Node].fixedRoot != f || u.Kind == UnitImage {
			continue
		}
		out = append(out, u)
	}
	return out
}

func (a *Audit) labelsOf(units []Unit, max int) string {
	var labels []string
	seen := map[string]bool{}
	for _, u := range units {
		l := a.Doc.Label(u.Node)
		if u.Kind == UnitText && len(u.TextNodes) > 0 {
			l = a.Doc.Label(u.TextNodes[0])
		}
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		labels = append(labels, quote(l))
		if len(labels) >= max {
			break
		}
	}
	return strings.Join(labels, ", ")
}

func (a *Audit) detectFixedLayers() {
	doc := a.Doc
	vp := doc.Viewport
	vr := vp.Rect()
	toolbar := vp.Mobile && vp.ToolbarHeight > 0 && a.Options.enabled(TypeToolbarCutoff)
	shown := Rect{X: vr.X, Y: vr.Y, W: vr.W, H: vr.H - vp.ToolbarHeight}
	for _, f := range doc.fixedRoots() {
		units := a.fixedUnits(f)
		var lost, cut []Unit
		var lostRegion, cutRegion Rect
		for _, u := range units {
			if doc.internalWindow(u.Node, f, u.Rect, vr).Area() < 0.2*u.Rect.Area() {
				continue
			}
			if !doc.reachable(u, vr) {
				lost = append(lost, u)
				lostRegion = lostRegion.Union(u.Rect)
				continue
			}
			if toolbar && !doc.reachable(u, shown) {
				cut = append(cut, u)
				cutRegion = cutRegion.Union(u.Rect.Intersect(Rect{X: vr.X, Y: shown.Bottom(), W: vr.W, H: vp.ToolbarHeight + math.Max(0, u.Rect.Bottom()-vr.Bottom())}))
			}
		}
		fb := doc.Nodes[f].Bounds
		if len(lost) > 0 && a.Options.enabled(TypeUnreachable) {
			below := math.Max(0, lostRegion.Bottom()-vr.Bottom())
			msg := fmt.Sprintf("%s is fixed and its content runs %s past the %s viewport with no scroll to reach it; %d item(s) can never be seen: %s",
				a.describe(f), fmtPx(math.Max(below, lostRegion.Right()-vr.Right())), fmtPx(vr.H), len(lost), a.labelsOf(lost, 4))
			ids := make([]int, len(lost))
			for k, u := range lost {
				ids[k] = u.ID
			}
			region := lostRegion
			if region.Empty() {
				region = fb
			}
			a.add(Finding{Type: TypeUnreachable, Severity: High, Message: msg, Elements: []ElementRef{a.ref(f), a.ref(lost[0].Node)},
				Rect: region, Metrics: map[string]float64{"hidden_items": float64(len(lost)), "overflow_px": math.Round(below)}, units: ids})
		}
		if len(cut) > 0 {
			anchor, known := doc.Anchors[f]
			if !known {
				continue
			}
			s := doc.scrollContainerY(cut[0].Node)
			trap := !anchor.Dynamic()
			why := ""
			switch {
			case trap && anchor.LargeViewportSized():
				why = fmt.Sprintf("sized %s", firstNonEmpty(anchor.Height, anchor.MinHeight))
			case trap:
				why = "anchored to the top with a fixed or content height"
			}
			if !trap && s >= 0 && s != f {
				if sa, ok := doc.Anchors[s]; ok && sa.LargeViewportSized() && !dynamicUnit(sa.MaxHeight) {
					trap = true
					why = fmt.Sprintf("its scroll area %s is sized %s", a.Doc.Selector(s), firstNonEmpty(sa.Height, sa.MinHeight))
				}
			}
			if !trap {
				continue
			}
			ids := make([]int, len(cut))
			for k, u := range cut {
				ids[k] = u.ID
			}
			msg := fmt.Sprintf("%s is %s, so on a phone the browser toolbar (%s) covers its bottom: %s cannot be reached; size it with dvh or anchor it with bottom",
				a.describe(f), why, fmtPx(vp.ToolbarHeight), a.labelsOf(cut, 4))
			a.add(Finding{Type: TypeToolbarCutoff, Severity: High, Message: msg, Elements: []ElementRef{a.ref(f), a.ref(cut[0].Node)},
				Rect: cutRegion, Metrics: map[string]float64{"hidden_items": float64(len(cut)), "toolbar_px": vp.ToolbarHeight}, units: ids})
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// detectOcclusion reports page content that a fixed bar covers at every
// scroll position: it sits under a header at the very top of the document or
// under a footer or floating button at the very bottom.
func (a *Audit) detectOcclusion() {
	doc := a.Doc
	vp := doc.Viewport
	vh := vp.Height
	type bar struct {
		node int
		rect Rect // viewport coordinates
		top  bool
	}
	var bars []bar
	for _, f := range doc.fixedRoots() {
		if doc.d[f].layer >= 0 {
			continue
		}
		n := doc.Nodes[f]
		r := n.Bounds.Intersect(vp.Rect()).Offset(-vp.ScrollX, -vp.ScrollY)
		// A bar hugs the top or bottom edge and leaves most of the height free;
		// a full-height drawer or sheet is a layer, not a bar.
		if r.Area() < 16 || r.H > 0.35*vh || (r.Y > 1 && r.Bottom() < vh-1) {
			continue
		}
		paints := n.Style.hasBackground()
		if !paints {
			for _, u := range a.Units {
				if doc.d[u.Node].fixedRoot == f {
					paints = true
					break
				}
			}
		}
		if !paints {
			continue
		}
		bars = append(bars, bar{node: f, rect: r, top: r.Y+r.H/2 < vh/2})
	}
	if len(bars) == 0 {
		return
	}
	maxScroll := math.Max(0, vp.DocHeight-vh)
	covered := map[int][]Unit{}
	var order []int
	for _, u := range a.Units {
		d := doc.d[u.Node]
		if d.fixedRoot >= 0 || d.stickyRoot >= 0 || d.layer >= 0 || !d.visible || u.Kind == UnitImage {
			continue
		}
		if u.Vis.Area() < 4 || u.Rect.H >= vh/2 || doc.scrollContainerY(u.Node) >= 0 {
			continue
		}
		top, bottom := 0.0, 0.0
		topBar, bottomBar := -1, -1
		for _, b := range bars {
			if b.rect.X >= u.Rect.Right() || b.rect.Right() <= u.Rect.X {
				continue
			}
			if doc.Nodes[b.node].Paint >= 0 && u.Paint >= 0 && doc.Nodes[b.node].Paint < u.Paint {
				continue // the content paints over the bar
			}
			if b.top && b.rect.Bottom() > top {
				top, topBar = b.rect.Bottom(), b.node
			}
			if !b.top && vh-b.rect.Y > bottom {
				bottom, bottomBar = vh-b.rect.Y, b.node
			}
		}
		if topBar < 0 && bottomBar < 0 {
			continue
		}
		lo := math.Max(0, u.Rect.Bottom()-(vh-bottom))
		hi := math.Min(maxScroll, u.Rect.Y-top)
		if lo <= hi+1 {
			continue
		}
		by := bottomBar
		if topBar >= 0 && u.Rect.Y < top {
			by = topBar
		}
		if by < 0 {
			by = topBar
		}
		if _, ok := covered[by]; !ok {
			order = append(order, by)
		}
		covered[by] = append(covered[by], u)
	}
	for _, by := range order {
		units := covered[by]
		var region Rect
		ids := make([]int, len(units))
		for k, u := range units {
			region = region.Union(u.Rect)
			ids[k] = u.ID
		}
		msg := fmt.Sprintf("%s covers %s at every scroll position", a.describe(by), a.labelsOf(units, 4))
		elems := []ElementRef{a.ref(by)}
		named := map[int]bool{}
		for _, u := range units {
			if len(elems) >= 4 {
				break
			}
			if !named[u.Node] {
				named[u.Node] = true
				elems = append(elems, a.ref(u.Node))
			}
		}
		a.add(Finding{Type: TypeOcclusion, Severity: High, Message: msg, Elements: elems,
			Rect: region, Metrics: map[string]float64{"covered_items": float64(len(units))}, units: ids})
	}
}
