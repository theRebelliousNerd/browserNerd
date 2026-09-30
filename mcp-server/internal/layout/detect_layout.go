package layout

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

var nonColumnRoles = map[string]bool{
	"tablist": true, "toolbar": true, "menubar": true, "radiogroup": true, "navigation": true,
	"group": true, "listbox": true, "menu": true, "tree": true,
}

// wrappedLines is the most lines any single text node inside element i wraps
// to.
func (doc *Document) wrappedLines(i int) int {
	best := 0
	end := len(doc.Nodes)
	for j := i; j < end && (j == i || doc.IsAncestor(i, j)); j++ {
		n := doc.Nodes[j]
		if len(n.TextBoxes) < 2 {
			if len(n.TextBoxes) == 1 && best == 0 {
				best = 1
			}
			continue
		}
		ys := make([]float64, 0, len(n.TextBoxes))
		for _, b := range n.TextBoxes {
			ys = append(ys, b.Y)
		}
		sort.Float64s(ys)
		lines := 1
		for k := 1; k < len(ys); k++ {
			if ys[k]-ys[k-1] > 2 {
				lines++
			}
		}
		if lines > best {
			best = lines
		}
	}
	return best
}

// inHorizontalScroller reports an element inside a container that scrolls
// sideways: a chip strip or a carousel is side by side on purpose.
func (doc *Document) inHorizontalScroller(i int) bool {
	for _, c := range doc.clipAncestors(i) {
		if scrolls(doc.Nodes[c].Style.OverflowX) {
			return true
		}
	}
	return scrolls(doc.Nodes[i].Style.OverflowX)
}

// detectNonCollapsing reports flex rows and grids that still put content
// columns side by side below the collapse width, squeezing one until its text
// wraps into a narrow tower.
func (a *Audit) detectNonCollapsing() {
	doc := a.Doc
	if doc.Viewport.Width >= a.Options.CollapseBelow {
		return
	}
	minCol := a.Options.MinColumnPx
	flagged := map[int]bool{}
	for i, n := range doc.Nodes {
		if !n.Layout || !doc.d[i].visible || n.Tag == "#text" {
			continue
		}
		disp := n.Style.Display
		isFlexRow := (disp == "flex" || disp == "inline-flex") && strings.HasPrefix(n.Style.FlexDirection, "row")
		isGrid := disp == "grid" || disp == "inline-grid"
		if !isFlexRow && !isGrid {
			continue
		}
		if nonColumnRoles[n.Attr("role")] || n.Tag == "nav" || n.Tag == "ul" && n.Attr("role") == "tablist" {
			continue
		}
		skip := false
		for p := n.Parent; p >= 0; p = doc.Nodes[p].Parent {
			if flagged[p] {
				skip = true
				break
			}
		}
		if skip || doc.inHorizontalScroller(i) {
			continue
		}
		var kids []int
		allControls := true
		for _, c := range n.Children {
			cn := doc.Nodes[c]
			if cn.Tag == "#text" || !cn.Layout || !doc.d[c].visible || cn.Bounds.W < 1 || cn.Bounds.H < 1 {
				continue
			}
			if cn.Style.Position == "absolute" || cn.Style.Position == "fixed" {
				continue
			}
			kids = append(kids, c)
			if !(doc.isControl(c) && cn.Bounds.H <= 64) && !imageTags[cn.Tag] {
				allControls = false
			}
		}
		if len(kids) < 2 || allControls {
			continue
		}
		// The largest set of children sharing one row band.
		var row []int
		for _, k := range kids {
			kb := doc.Nodes[k].Bounds
			var band []int
			for _, m := range kids {
				mb := doc.Nodes[m].Bounds
				ov := math.Min(kb.Bottom(), mb.Bottom()) - math.Max(kb.Y, mb.Y)
				if ov >= 0.5*math.Min(kb.H, mb.H) {
					band = append(band, m)
				}
			}
			if len(band) > len(row) {
				row = band
			}
		}
		if len(row) < 2 {
			continue
		}
		var squeezed []int
		minW := math.Inf(1)
		for _, k := range row {
			kn := doc.Nodes[k]
			if kn.Bounds.W < minW {
				minW = kn.Bounds.W
			}
			if kn.Bounds.W < minCol && kn.Bounds.H >= 40 && doc.wrappedLines(k) >= 3 {
				squeezed = append(squeezed, k)
			}
		}
		if len(squeezed) == 0 {
			continue
		}
		flagged[i] = true
		sev := Medium
		if minW < 90 || len(row) >= 3 {
			sev = High
		}
		widths := make([]string, 0, len(row))
		for _, k := range row {
			widths = append(widths, fmtPx(doc.Nodes[k].Bounds.W))
		}
		kind := "flex row"
		if isGrid {
			kind = "grid"
		}
		msg := fmt.Sprintf("%s keeps %d columns side by side at %s wide (%s); %s wraps to %d lines - stack it on a phone",
			kind, len(row), fmtPx(doc.Viewport.Width), strings.Join(widths, " | "), a.describe(squeezed[0]), doc.wrappedLines(squeezed[0]))
		elems := []ElementRef{a.ref(i)}
		for k, s := range squeezed {
			if k >= 3 {
				break
			}
			elems = append(elems, a.ref(s))
		}
		a.add(Finding{Type: TypeNonCollapsing, Severity: sev, Message: msg, Elements: elems, Rect: n.Bounds,
			Metrics: map[string]float64{"columns": float64(len(row)), "narrowest_px": math.Round(minW)}})
	}
}

// tapTargetControls lists the outermost enabled controls.
func (doc *Document) tapTargetControls() []int {
	var out []int
	inControl := make([]bool, len(doc.Nodes))
	for i, n := range doc.Nodes {
		if n.Parent >= 0 && n.Parent < i && (inControl[n.Parent] || doc.isControl(n.Parent) && doc.Nodes[n.Parent].Layout) {
			inControl[i] = true
			continue
		}
		if !n.Layout || n.Tag == "#text" || !doc.d[i].visible || !doc.isControl(i) {
			continue
		}
		if _, disabled := n.Attrs["disabled"]; disabled || n.Attr("aria-disabled") == "true" {
			continue
		}
		out = append(out, i)
	}
	return out
}

// inlineLink reports a link inside a sentence, which WCAG 2.5.8 exempts.
func (doc *Document) inlineLink(i int) bool {
	n := doc.Nodes[i]
	if n.Tag != "a" || !n.Style.inline() {
		return false
	}
	p := n.Parent
	if p < 0 {
		return false
	}
	for _, s := range doc.Nodes[p].Children {
		if s != i && doc.Nodes[s].Tag == "#text" && len(strings.TrimSpace(doc.Nodes[s].Text)) >= 3 {
			return true
		}
	}
	return false
}

// detectTapTargets reports controls smaller than a fingertip on a touch
// viewport.
func (a *Audit) detectTapTargets() {
	doc := a.Doc
	if !doc.Viewport.Touch && !doc.Viewport.Mobile {
		return
	}
	min := a.Options.TapTargetPx
	for _, i := range doc.tapTargetControls() {
		n := doc.Nodes[i]
		r := n.Bounds
		// A checkbox or radio inside its label is as big as the label.
		if n.Tag == "input" {
			for p := n.Parent; p >= 0; p = doc.Nodes[p].Parent {
				if doc.Nodes[p].Tag == "label" {
					r = r.Union(doc.Nodes[p].Bounds)
					break
				}
			}
		}
		if r.W >= min && r.H >= min {
			continue
		}
		if r.Area() < 4 || doc.VisibleRect(i, r).Area() < 4 || doc.inlineLink(i) {
			continue
		}
		if r.Right() <= 0 || r.Bottom() <= 0 {
			continue // parked above or left of the document: never on screen
		}
		sev := Low
		if math.Min(r.W, r.H) < 24 {
			sev = Medium
		}
		msg := fmt.Sprintf("%s is %s, under the %s tap target", a.describe(i), fmtSize(r.W, r.H), fmtPx(min))
		a.add(Finding{Type: TypeTapTarget, Severity: sev, Message: msg, Elements: []ElementRef{a.ref(i)}, Rect: r,
			Metrics: map[string]float64{"width": math.Round(r.W), "height": math.Round(r.H)}})
	}
}
