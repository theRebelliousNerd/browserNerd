package layout

import (
	"fmt"
	"regexp"
	"strings"
)

// UnitKind names what a content unit is.
type UnitKind string

const (
	UnitText    UnitKind = "text"
	UnitControl UnitKind = "control"
	UnitImage   UnitKind = "image"
	UnitBadge   UnitKind = "badge"
)

// Unit is one piece of content the detectors compare: a control, image or
// badge as a whole (its descendants fold into it), or one inline text box of
// any other element.
type Unit struct {
	ID    int
	Node  int // the owning element
	Kind  UnitKind
	Rect  Rect
	Vis   Rect // Rect after the owner's clip chain
	Paint int
	// Texts are the inline text boxes inside a control, image or badge unit,
	// or the unit's own box for a text unit.
	Texts []Rect
	// TextNodes are the text nodes the Texts came from.
	TextNodes []int
}

// controlRoles are ARIA roles a person operates.
var controlRoles = map[string]bool{
	"button": true, "link": true, "tab": true, "menuitem": true, "menuitemcheckbox": true,
	"menuitemradio": true, "option": true, "checkbox": true, "radio": true, "switch": true,
	"combobox": true, "slider": true, "spinbutton": true, "treeitem": true,
}

// isControl reports an element a person operates, by semantics or because the
// page made it clickable (cursor: pointer where its parent is not).
func (doc *Document) isControl(i int) bool {
	n := doc.Nodes[i]
	switch n.Tag {
	case "button", "select", "textarea", "summary":
		return true
	case "a":
		_, ok := n.Attrs["href"]
		return ok
	case "input":
		return !strings.EqualFold(n.Attr("type"), "hidden")
	}
	if controlRoles[n.Attr("role")] {
		return true
	}
	if n.Style.Cursor == "pointer" && n.Tag != "#text" && !strings.HasPrefix(n.Tag, "::") {
		p := n.Parent
		if p < 0 || doc.Nodes[p].Style.Cursor != "pointer" {
			return true
		}
	}
	return false
}

// maxAtomicControlHeight: a control up to this tall is one unit; a taller one
// (a clickable card or row) is a control for tap targets but its content is
// still inspected piece by piece.
const maxAtomicControlHeight = 80

var imageTags = map[string]bool{"img": true, "svg": true, "video": true, "canvas": true, "iframe": true, "picture": true, "object": true, "embed": true}

// isBadge reports a small boxed element holding its own short text: a badge,
// pill, chip or tag.
func (doc *Document) isBadge(i int) bool {
	n := doc.Nodes[i]
	if n.Tag == "#text" || n.Style.inline() && !n.Style.hasBackground() {
		return false
	}
	if !n.Style.hasBackground() && n.Style.BorderWidth <= 0 {
		return false
	}
	if n.Bounds.H > 40 || n.Bounds.W > 260 || n.Bounds.H < 8 {
		return false
	}
	text := 0
	for _, c := range n.Children {
		cn := doc.Nodes[c]
		if cn.Tag == "#text" && strings.TrimSpace(cn.Text) != "" {
			text += len(strings.TrimSpace(cn.Text))
		}
	}
	return text > 0 && text <= 40
}

// buildUnits cuts the visible content into units.
func (doc *Document) buildUnits() []Unit {
	var units []Unit
	var visit func(i int, owner int)
	add := func(u Unit) int {
		u.ID = len(units)
		units = append(units, u)
		return u.ID
	}
	visit = func(i int, owner int) {
		n := doc.Nodes[i]
		if doc.d[i].opacity <= 0.02 && n.Tag != "#text" {
			// Faded to nothing, with everything inside it. (A visibility: hidden
			// element is walked: a descendant may be visible again.)
			return
		}
		if owner >= 0 {
			// Inside a control, image or badge: gather text into it.
			if len(n.TextBoxes) > 0 && doc.d[i].visible {
				u := &units[owner]
				for _, b := range n.TextBoxes {
					u.Texts = append(u.Texts, b)
				}
				u.TextNodes = append(u.TextNodes, i)
			}
			for _, c := range n.Children {
				visit(c, owner)
			}
			return
		}
		if n.Layout && n.Tag != "#text" && doc.d[i].visible && !n.Bounds.Empty() {
			kind := UnitKind("")
			switch {
			case imageTags[n.Tag] || n.Attr("role") == "img" && len(n.Children) == 0:
				kind = UnitImage
			case doc.isControl(i) && n.Bounds.H <= maxAtomicControlHeight:
				kind = UnitControl
			case doc.isBadge(i):
				kind = UnitBadge
			}
			if kind != "" {
				id := add(Unit{Node: i, Kind: kind, Rect: n.Bounds, Vis: doc.VisibleRect(i, n.Bounds), Paint: n.Paint})
				if len(n.TextBoxes) > 0 {
					units[id].Texts = append(units[id].Texts, n.TextBoxes...)
					units[id].TextNodes = append(units[id].TextNodes, i)
				}
				for _, c := range n.Children {
					visit(c, id)
				}
				return
			}
		}
		if len(n.TextBoxes) > 0 && doc.d[i].visible && (strings.TrimSpace(n.Text) != "" || strings.HasPrefix(n.Tag, "::")) {
			ownerEl := doc.element(i)
			if ownerEl < 0 {
				ownerEl = i
			}
			for _, b := range n.TextBoxes {
				add(Unit{Node: ownerEl, Kind: UnitText, Rect: b, Vis: doc.VisibleRect(i, b), Paint: n.Paint, Texts: []Rect{b}, TextNodes: []int{i}})
			}
		}
		for _, c := range n.Children {
			visit(c, -1)
		}
	}
	for i, n := range doc.Nodes {
		if n.Parent < 0 || n.Parent >= i {
			visit(i, -1)
		}
	}
	return units
}

// Label is a short human name for an element: its accessible label, its text,
// or its alt, title or placeholder.
func (doc *Document) Label(i int) string {
	if i < 0 || i >= len(doc.Nodes) {
		return ""
	}
	n := doc.Nodes[i]
	if n.Tag == "#text" {
		return clip40(strings.Join(strings.Fields(n.Text), " "))
	}
	for _, a := range []string{"aria-label", "alt", "title", "placeholder"} {
		if v := strings.TrimSpace(n.Attr(a)); v != "" {
			return clip40(v)
		}
	}
	var sb strings.Builder
	var collect func(j int)
	collect = func(j int) {
		if sb.Len() > 60 {
			return
		}
		nj := doc.Nodes[j]
		if nj.Tag == "#text" {
			if t := strings.Join(strings.Fields(nj.Text), " "); t != "" {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(t)
			}
			return
		}
		if nj.Tag == "script" || nj.Tag == "style" {
			return
		}
		for _, c := range nj.Children {
			collect(c)
		}
	}
	collect(i)
	if sb.Len() > 0 {
		return clip40(sb.String())
	}
	if v := strings.TrimSpace(n.Attr("value")); v != "" && n.Tag == "input" {
		return clip40(v)
	}
	return ""
}

func clip40(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}

// generatedID matches ids a framework mints per render (React useId, Radix),
// which are no use as a stable selector.
var generatedID = regexp.MustCompile(`^(:r[0-9a-z]*:|radix-|headlessui-|mui-|react-aria)|[0-9a-f]{8,}|«`)

// Selector is a CSS selector that finds the element again: its data-testid,
// its id, its aria-label, or a child path of tag:nth-of-type steps from the
// nearest ancestor that has one of those.
func (doc *Document) Selector(i int) string {
	if i < 0 || i >= len(doc.Nodes) {
		return ""
	}
	i = doc.element(i)
	if i < 0 {
		return ""
	}
	suffix := ""
	if n := doc.Nodes[i]; n.Pseudo != "" {
		suffix = "::" + n.Pseudo
		i = n.Parent
	}
	var steps []string
	for cur := i; cur >= 0; cur = doc.Nodes[cur].Parent {
		n := doc.Nodes[cur]
		if n.Tag == "#document" || strings.HasPrefix(n.Tag, "#") {
			break
		}
		if anchor := doc.anchorSelector(cur); anchor != "" {
			steps = append(steps, anchor)
			break
		}
		if n.Tag == "html" || n.Tag == "body" {
			steps = append(steps, n.Tag)
			break
		}
		steps = append(steps, fmt.Sprintf("%s:nth-of-type(%d)", n.Tag, doc.nthOfType(cur)))
		if len(steps) >= 8 {
			break
		}
	}
	for l, r := 0, len(steps)-1; l < r; l, r = l+1, r-1 {
		steps[l], steps[r] = steps[r], steps[l]
	}
	return strings.Join(steps, " > ") + suffix
}

func (doc *Document) anchorSelector(i int) string {
	n := doc.Nodes[i]
	if v := n.Attr("data-testid"); v != "" {
		return fmt.Sprintf(`[data-testid="%s"]`, cssQuote(v))
	}
	if v := n.Attr("id"); v != "" && !generatedID.MatchString(v) && cssIdent(v) {
		return "#" + v
	}
	if v := n.Attr("aria-label"); v != "" && len(v) <= 40 {
		return fmt.Sprintf(`%s[aria-label="%s"]`, n.Tag, cssQuote(v))
	}
	return ""
}

func (doc *Document) nthOfType(i int) int {
	n := doc.Nodes[i]
	if n.Parent < 0 {
		return 1
	}
	k := 0
	for _, s := range doc.Nodes[n.Parent].Children {
		if doc.Nodes[s].Tag == n.Tag && doc.Nodes[s].Pseudo == "" {
			k++
		}
		if s == i {
			return k
		}
	}
	return 1
}

func cssQuote(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`)
}

func cssIdent(v string) bool {
	if v == "" || (v[0] >= '0' && v[0] <= '9') {
		return false
	}
	for _, r := range v {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// patternOf is a selector with its nth-of-type positions removed, so the same
// defect repeated in every card of a grid groups into one row.
var nthRE = regexp.MustCompile(`:nth-of-type\(\d+\)`)

func patternOf(sel string) string { return nthRE.ReplaceAllString(sel, "") }
