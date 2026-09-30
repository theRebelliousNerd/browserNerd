package layout

import (
	"fmt"
	"math"
)

type layerKey struct{ layer, fixed, sticky int }

func (a *Audit) layerOf(u Unit) layerKey {
	d := a.Doc.d[u.Node]
	return layerKey{d.layer, d.fixedRoot, d.stickyRoot}
}

// absoluteBelow reports whether node i, or an ancestor of it below stop, is
// absolutely positioned: it was placed on top of its neighbours on purpose.
func (doc *Document) absoluteBelow(i, stop int) bool {
	for cur := i; cur >= 0 && cur != stop; cur = doc.Nodes[cur].Parent {
		if doc.Nodes[cur].Style.Position == "absolute" {
			return true
		}
	}
	return false
}

// commonAncestor is the nearest node both a and b sit under.
func (doc *Document) commonAncestor(a, b int) int {
	for cur := a; cur >= 0; cur = doc.Nodes[cur].Parent {
		if cur == b || doc.IsAncestor(cur, b) {
			return cur
		}
	}
	return -1
}

func paintAbove(u, v Unit) bool {
	if u.Paint >= 0 && v.Paint >= 0 && u.Paint != v.Paint {
		return u.Paint > v.Paint
	}
	return u.Node > v.Node
}

// intentionalOverlap holds the patterns that overlap by design.
func (a *Audit) intentionalOverlap(u, v Unit) bool {
	doc := a.Doc
	if u.Kind == UnitImage && v.Kind == UnitImage {
		// Stacked avatars and layered artwork.
		return true
	}
	for _, pair := range [][2]Unit{{u, v}, {v, u}} {
		img, other := pair[0], pair[1]
		if img.Kind != UnitImage {
			continue
		}
		common := doc.commonAncestor(img.Node, other.Node)
		// A small absolutely placed element over an image: a badge on an avatar.
		if other.Rect.Area() <= 0.6*img.Rect.Area() && doc.absoluteBelow(other.Node, common) {
			return true
		}
		// An image under content it is much larger than, or a decorative one
		// under anything: a background.
		if paintAbove(other, img) && (img.Rect.Area() >= 4*other.Rect.Area() || doc.d[img.Node].ariaHidden || doc.Nodes[img.Node].Style.PointerEvents == "none") {
			return true
		}
	}
	return false
}

type overlapAgg struct {
	a, b      int // owner nodes, a < b
	kindA     UnitKind
	kindB     UnitKind
	region    Rect
	area      float64
	textClash bool
	units     []int
	aAbove    bool
}

func (a *Audit) detectOverlaps(g *grid) {
	doc := a.Doc
	minPx := a.Options.MinOverlapPx
	aggs := map[[2]int]*overlapAgg{}
	var order [][2]int
	for _, u := range a.Units {
		if u.Vis.Area() < 4 || !doc.d[u.Node].visible {
			continue
		}
		for _, id := range g.query(u.Vis) {
			if id <= u.ID {
				continue
			}
			v := a.Units[id]
			if v.Vis.Area() < 4 || u.Node == v.Node || !doc.d[v.Node].visible {
				continue
			}
			if (u.Kind != UnitText && doc.IsAncestor(u.Node, v.Node)) || (v.Kind != UnitText && doc.IsAncestor(v.Node, u.Node)) {
				continue
			}
			if a.layerOf(u) != a.layerOf(v) {
				continue
			}
			in := u.Vis.Intersect(v.Vis)
			if in.W < minPx || in.H < minPx {
				continue
			}
			if u.Kind == UnitText && v.Kind == UnitText {
				// Stacked lines with tight leading touch by a pixel or two.
				if in.H < math.Max(minPx, 0.25*math.Min(u.Rect.H, v.Rect.H)) {
					continue
				}
			}
			if a.intentionalOverlap(u, v) {
				continue
			}
			na, nb := u.Node, v.Node
			ka, kb := u.Kind, v.Kind
			ua, ub := u, v
			if nb < na {
				na, nb, ka, kb, ua, ub = nb, na, kb, ka, ub, ua
			}
			key := [2]int{na, nb}
			agg, ok := aggs[key]
			if !ok {
				agg = &overlapAgg{a: na, b: nb, kindA: ka, kindB: kb, aAbove: paintAbove(ua, ub)}
				aggs[key] = agg
				order = append(order, key)
			}
			agg.region = agg.region.Union(in)
			agg.area += in.Area()
			agg.units = append(agg.units, u.ID, v.ID)
			if !agg.textClash {
				for _, t1 := range u.Texts {
					for _, t2 := range v.Texts {
						ti := t1.Intersect(t2).Intersect(in)
						if ti.W >= minPx && ti.H >= minPx {
							agg.textClash = true
						}
					}
				}
			}
		}
	}
	for _, key := range order {
		agg := aggs[key]
		sev := Medium
		what := "overlaps"
		switch {
		case agg.textClash:
			sev = High
			what = "is drawn over the text of"
		case agg.kindA == UnitControl && agg.kindB == UnitControl:
			sev = High
			what = "overlaps (both are controls)"
		}
		top, bottom := agg.a, agg.b
		if !agg.aAbove {
			top, bottom = agg.b, agg.a
		}
		msg := fmt.Sprintf("%s %s %s by %s", a.describe(top), what, a.describe(bottom), fmtSize(agg.region.W, agg.region.H))
		a.add(Finding{
			Type:     TypeOverlap,
			Severity: sev,
			Message:  msg,
			Elements: []ElementRef{a.ref(top), a.ref(bottom)},
			Rect:     agg.region,
			Metrics:  map[string]float64{"overlap_area": math.Round(agg.area)},
			units:    agg.units,
		})
	}
}
