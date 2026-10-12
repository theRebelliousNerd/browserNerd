package layout

import (
	"fmt"
	"math"
	"sort"
)

// textGroup is text held by one box: a control's or badge's label, or the
// inline text of a block.
type textGroup struct {
	box   int // the element whose box should hold the text
	kind  UnitKind
	texts []Rect
	nodes []int // the text node of each rect
	units []int
}

// blockContainer is the nearest element at or above i whose box holds inline
// text: the first one that is not itself inline.
func (doc *Document) blockContainer(i int) int {
	for cur := doc.element(i); cur >= 0; cur = doc.Nodes[cur].Parent {
		n := doc.Nodes[cur]
		if n.Layout && !n.Style.inline() {
			return cur
		}
	}
	return -1
}

func (a *Audit) textGroups() []*textGroup {
	doc := a.Doc
	byBox := map[int]*textGroup{}
	var order []int
	get := func(box int, kind UnitKind) *textGroup {
		g, ok := byBox[box]
		if !ok {
			g = &textGroup{box: box, kind: kind}
			byBox[box] = g
			order = append(order, box)
		}
		return g
	}
	for _, u := range a.Units {
		switch u.Kind {
		case UnitControl, UnitBadge:
			if len(u.Texts) == 0 {
				continue
			}
			g := get(u.Node, u.Kind)
			g.units = append(g.units, u.ID)
			k := 0
			for _, tn := range u.TextNodes {
				for range doc.Nodes[tn].TextBoxes {
					if k < len(u.Texts) {
						g.texts = append(g.texts, u.Texts[k])
						g.nodes = append(g.nodes, tn)
					}
					k++
				}
			}
		case UnitText:
			box := doc.blockContainer(u.TextNodes[0])
			if box < 0 {
				continue
			}
			g := get(box, UnitText)
			g.units = append(g.units, u.ID)
			g.texts = append(g.texts, u.Rect)
			g.nodes = append(g.nodes, u.TextNodes[0])
		}
	}
	out := make([]*textGroup, 0, len(order))
	sort.Ints(order)
	for _, b := range order {
		out = append(out, byBox[b])
	}
	return out
}

// cutSide names the side of box a rect passes, and by how much, preferring
// the larger excursion.
func cutSide(t, box Rect) (string, float64) {
	side, amount := "", 0.0
	check := func(s string, v, tol float64) {
		if v > tol && v > amount {
			side, amount = s, v
		}
	}
	// A text box is the font's full ascent and descent, which a tight
	// line-height (leading-none) lets overhang its line by a few pixels while
	// the glyphs stay inside, so the vertical slack scales with the text.
	vtol := math.Max(2, 0.3*t.H)
	check("right", t.Right()-box.Right(), 1.5)
	check("left", box.X-t.X, 1.5)
	check("bottom", t.Bottom()-box.Bottom(), vtol)
	check("top", box.Y-t.Y, vtol)
	return side, amount
}

func horizontal(side string) bool { return side == "left" || side == "right" }

// intentionalTruncation reports an ellipsis (for a horizontal cut) or a line
// clamp (for a vertical one) anywhere from the text up to the clipping box.
func (doc *Document) intentionalTruncation(textNode, clipper int, side string) bool {
	for cur := textNode; cur >= 0; cur = doc.Nodes[cur].Parent {
		s := doc.Nodes[cur].Style
		if horizontal(side) && s.TextOverflow == "ellipsis" {
			return true
		}
		if !horizontal(side) && s.LineClamp != "" && s.LineClamp != "none" {
			return true
		}
		if cur == clipper {
			break
		}
	}
	return false
}

func (a *Audit) boxed(i int) bool {
	n := a.Doc.Nodes[i]
	if a.Doc.isControl(i) {
		return true
	}
	switch n.Tag {
	case "td", "th", "button":
		return true
	}
	switch n.Attr("role") {
	case "tab", "cell", "gridcell", "columnheader", "rowheader", "button":
		return true
	}
	return n.Style.hasBackground() || n.Style.BorderWidth > 0
}

func (a *Audit) detectText(g *grid) {
	doc := a.Doc
	vw := doc.Viewport.Width
	for _, grp := range a.textGroups() {
		box := doc.Nodes[grp.box].Bounds
		var bleed, cut Rect
		bleedSide, cutSideName := "", ""
		bleedAmount, cutAmount, hiddenFrac := 0.0, 0.0, 0.0
		bleedEdge, cutEdge := 0.0, 0.0
		clipperOf := -1
		for k, t := range grp.texts {
			side, amount := cutSide(t, box)
			if side == "" {
				continue
			}
			part := largest(outsideParts(t, box))
			if part.Empty() {
				continue
			}
			tn := grp.nodes[k]
			// Which clip, if any, cuts the text on that side?
			clipper := -1
			for _, c := range doc.clipAncestors(tn) {
				if cs, _ := cutSide(t, doc.clientBox(c)); cs == side {
					clipper = c
					break
				}
			}
			if clipper >= 0 {
				cn := doc.Nodes[clipper]
				if cn.Tag == "html" || cn.Tag == "body" {
					continue // the horizontal-overflow detector reports cuts at the page edge
				}
				overflow := cn.Style.OverflowY
				if horizontal(side) {
					overflow = cn.Style.OverflowX
				}
				if scrolls(overflow) || doc.intentionalTruncation(tn, clipper, side) {
					continue
				}
				cb := doc.clientBox(clipper)
				if cb.Area() <= 2 {
					continue // visually hidden text
				}
				visible := t.Intersect(doc.d[tn].clip)
				if visible.Area() < 0.1*t.Area() {
					continue // wholly hidden: collapsed, not cut
				}
				hidden := largest(outsideParts(t, cb))
				cut = cut.Union(hidden)
				if hidden.Area()/t.Area() > hiddenFrac {
					hiddenFrac = hidden.Area() / t.Area()
				}
				if amount > cutAmount {
					cutAmount, cutSideName, clipperOf = amount, side, clipper
					switch side {
					case "right":
						cutEdge = cb.Right()
					case "left":
						cutEdge = cb.X
					case "bottom":
						cutEdge = cb.Bottom()
					case "top":
						cutEdge = cb.Y
					}
				}
				continue
			}
			if t.Intersect(doc.d[tn].clip).Empty() {
				continue
			}
			bleed = bleed.Union(part)
			if amount > bleedAmount {
				bleedAmount, bleedSide = amount, side
				switch side {
				case "right":
					bleedEdge = box.Right()
				case "left":
					bleedEdge = box.X
				case "bottom":
					bleedEdge = box.Bottom()
				case "top":
					bleedEdge = box.Y
				}
			}
		}
		if !bleed.Empty() && a.Options.enabled(TypeTextBleed) {
			sev := Medium
			into := -1
			for _, id := range g.query(bleed) {
				u := a.Units[id]
				if u.Node == grp.box || doc.IsAncestor(u.Node, grp.box) || doc.IsAncestor(grp.box, u.Node) {
					continue
				}
				if !u.Vis.Intersect(bleed).Empty() {
					into = u.Node
					break
				}
			}
			if a.boxed(grp.box) || into >= 0 || bleed.Right() > vw+1 || bleed.X < -1 {
				sev = High
			}
			msg := fmt.Sprintf("text %s runs %s past the %s edge of %s", quote(a.textOf(grp)), fmtPx(bleedAmount), bleedSide, a.describe(grp.box))
			elems := []ElementRef{a.ref(grp.box)}
			if into >= 0 {
				msg += " into " + a.describe(into)
				elems = append(elems, a.ref(into))
			}
			if bleed.Right() > vw+1 {
				msg += " and off the screen"
			}
			a.add(Finding{Type: TypeTextBleed, Severity: sev, Message: msg, Elements: elems, Rect: bleed,
				Metrics: map[string]float64{"outside_px": math.Round(bleedAmount)}, units: grp.units, cutEdge: bleedSide, cutAt: bleedEdge})
		}
		if !cut.Empty() && a.Options.enabled(TypeTextClipped) {
			sev := Medium
			if hiddenFrac >= 0.4 || doc.isControl(grp.box) {
				sev = High
			}
			msg := fmt.Sprintf("text %s is cut off at the %s by %s (overflow %s, no ellipsis): %s hidden",
				quote(a.textOf(grp)), cutSideName, a.describe(clipperOf), a.overflowOf(clipperOf, cutSideName), fmtPx(cutAmount))
			elems := []ElementRef{a.ref(grp.box)}
			if clipperOf != grp.box {
				elems = append(elems, a.ref(clipperOf))
			}
			a.add(Finding{Type: TypeTextClipped, Severity: sev, Message: msg, Elements: elems, Rect: cut,
				Metrics: map[string]float64{"hidden_px": math.Round(cutAmount), "hidden_fraction": math.Round(hiddenFrac*100) / 100},
				units:   grp.units, cutEdge: cutSideName, cutAt: cutEdge})
		}
	}
}

func (a *Audit) overflowOf(i int, side string) string {
	if i < 0 {
		return "hidden"
	}
	if horizontal(side) {
		return a.Doc.Nodes[i].Style.OverflowX
	}
	return a.Doc.Nodes[i].Style.OverflowY
}

// textOf is the text a group holds, shortened.
func (a *Audit) textOf(grp *textGroup) string {
	seen := map[int]bool{}
	s := ""
	for _, tn := range grp.nodes {
		if seen[tn] {
			continue
		}
		seen[tn] = true
		l := a.Doc.Label(tn)
		if l == "" {
			continue
		}
		if s != "" {
			s += " "
		}
		s += l
		if len(s) > 40 {
			break
		}
	}
	return clip40(s)
}
