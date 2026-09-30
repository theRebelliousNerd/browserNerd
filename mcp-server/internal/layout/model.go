package layout

import (
	"strconv"
	"strings"
)

// StyleProperties are the computed styles the audit reads, in the order a
// DOMSnapshot capture must request them.
var StyleProperties = []string{
	"display", "position", "visibility", "opacity", "overflow-x", "overflow-y",
	"z-index", "text-overflow", "white-space", "-webkit-line-clamp", "cursor",
	"pointer-events", "background-color", "background-image", "border-top-width",
	"border-right-width", "border-bottom-width", "border-left-width", "transform",
	"flex-direction", "flex-wrap",
}

// Style is the part of an element's computed style the detectors read.
type Style struct {
	Display         string
	Position        string
	Visibility      string
	Opacity         float64
	OverflowX       string
	OverflowY       string
	ZIndex          string
	TextOverflow    string
	WhiteSpace      string
	LineClamp       string
	Cursor          string
	PointerEvents   string
	BackgroundColor string
	BackgroundImage string
	BorderWidth     float64 // the widest of the four sides
	Transform       string
	FlexDirection   string
	FlexWrap        string
}

// StyleFromValues builds a Style from values listed in StyleProperties order.
func StyleFromValues(values []string) Style {
	get := func(i int) string {
		if i < len(values) {
			return strings.TrimSpace(values[i])
		}
		return ""
	}
	opacity := 1.0
	if v := get(3); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			opacity = f
		}
	}
	border := 0.0
	for i := 14; i <= 17; i++ {
		if f := parsePx(get(i)); f > border {
			border = f
		}
	}
	return Style{
		Display: get(0), Position: get(1), Visibility: get(2), Opacity: opacity,
		OverflowX: get(4), OverflowY: get(5), ZIndex: get(6), TextOverflow: get(7),
		WhiteSpace: get(8), LineClamp: get(9), Cursor: get(10), PointerEvents: get(11),
		BackgroundColor: get(12), BackgroundImage: get(13), BorderWidth: border,
		Transform: get(18), FlexDirection: get(19), FlexWrap: get(20),
	}
}

// DefaultStyle is the computed style of a plain, visible, static block.
func DefaultStyle() Style {
	return Style{Display: "block", Position: "static", Visibility: "visible", Opacity: 1,
		OverflowX: "visible", OverflowY: "visible", TextOverflow: "clip", WhiteSpace: "normal",
		LineClamp: "none", Cursor: "auto", PointerEvents: "auto", BackgroundColor: "rgba(0, 0, 0, 0)",
		BackgroundImage: "none", Transform: "none", FlexDirection: "row", FlexWrap: "nowrap"}
}

func parsePx(v string) float64 {
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

// clips reports whether an overflow value clips content on its axis.
func clips(overflow string) bool {
	switch overflow {
	case "hidden", "clip", "auto", "scroll", "overlay":
		return true
	}
	return false
}

// scrolls reports whether an overflow value lets a person scroll the axis.
func scrolls(overflow string) bool {
	switch overflow {
	case "auto", "scroll", "overlay":
		return true
	}
	return false
}

// hasBackground reports a background that is drawn: a colour with alpha, or
// an image.
func (s Style) hasBackground() bool {
	if s.BackgroundImage != "" && s.BackgroundImage != "none" {
		return true
	}
	c := strings.ReplaceAll(s.BackgroundColor, " ", "")
	return c != "" && c != "transparent" && !strings.HasPrefix(c, "rgba(0,0,0,0)") && !strings.HasSuffix(c, ",0)")
}

func (s Style) inline() bool { return s.Display == "inline" || s.Display == "contents" }

// Node is one DOM node of the captured document.
type Node struct {
	Parent    int
	Children  []int
	Tag       string // lower-case element name; "#text" for a text node
	Attrs     map[string]string
	Text      string // a text node's value
	Pseudo    string // "before", "after", ... for a pseudo-element
	BackendID int
	Layout    bool // the node has a layout object
	Bounds    Rect
	// Client is the element's client box (padding box without scrollbars) in
	// document coordinates; HasClient is false when the snapshot gave none.
	Client     Rect
	HasClient  bool
	ScrollW    float64
	ScrollH    float64
	ScrollLeft float64
	ScrollTop  float64
	Style      Style
	Paint      int
	TextBoxes  []Rect
}

// Attr returns an attribute value, "" when absent.
func (n *Node) Attr(name string) string {
	if n.Attrs == nil {
		return ""
	}
	return n.Attrs[name]
}

// Viewport is the size a page was measured at.
type Viewport struct {
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	Scale         float64 `json:"scale"`
	Mobile        bool    `json:"mobile"`
	Touch         bool    `json:"touch"`
	ToolbarHeight float64 `json:"toolbar_height,omitempty"`
	Preset        string  `json:"preset,omitempty"`
	ScrollX       float64 `json:"scroll_x"`
	ScrollY       float64 `json:"scroll_y"`
	DocWidth      float64 `json:"doc_width"`
	DocHeight     float64 `json:"doc_height"`
	// LayoutWidth is set when a mobile page is wider than the device and Chrome
	// widened the layout viewport to it (the page shows zoomed out).
	LayoutWidth float64 `json:"layout_width,omitempty"`
}

// InitialContainingBlock is the size of the root element's client box: the
// device width and height a mobile page lays out against.
func (doc *Document) InitialContainingBlock() (float64, float64, bool) {
	for _, n := range doc.Nodes {
		if n.Tag == "html" && n.HasClient && n.Client.W > 0 && n.Client.H > 0 {
			return n.Client.W, n.Client.H, true
		}
	}
	return 0, 0, false
}

// Rect is the visible part of the document, in document coordinates.
func (v Viewport) Rect() Rect { return Rect{X: v.ScrollX, Y: v.ScrollY, W: v.Width, H: v.Height} }

// Anchor is how a fixed element's authored styles anchor and size it, read
// from its matched CSS rules.
type Anchor struct {
	TopSet    bool
	BottomSet bool
	Height    string
	MinHeight string
	MaxHeight string
}

// Dynamic reports an element whose height follows the dynamic viewport: it is
// anchored at the bottom, stretched between top and bottom, or sized or capped
// in dvh, svh or a percentage.
func (a Anchor) Dynamic() bool {
	if a.BottomSet {
		return true
	}
	return dynamicUnit(a.Height) || dynamicUnit(a.MaxHeight)
}

// LargeViewportSized reports a height or min-height in vh or lvh, which a
// phone resolves against the large viewport.
func (a Anchor) LargeViewportSized() bool {
	return largeVHUnit(a.Height) || largeVHUnit(a.MinHeight)
}

func dynamicUnit(v string) bool {
	v = strings.ToLower(v)
	return strings.Contains(v, "dvh") || strings.Contains(v, "svh") || strings.Contains(v, "%") ||
		strings.Contains(v, "-webkit-fill-available") || strings.Contains(v, "stretch")
}

func largeVHUnit(v string) bool {
	v = strings.ToLower(v)
	if strings.Contains(v, "dvh") || strings.Contains(v, "svh") {
		return false
	}
	return strings.Contains(v, "vh")
}

// Document is a captured page: its nodes in document order (a parent always
// precedes its children), the viewport it was measured at, and the authored
// anchoring of the fixed elements that were probed.
type Document struct {
	Nodes    []*Node
	Viewport Viewport
	// Anchors holds authored anchoring by node index for probed elements.
	Anchors map[int]Anchor
	// AnchorsErr says why authored styles could not be read, when they were
	// wanted and could not be.
	AnchorsErr string

	d     []derived
	enter []int
	exit  []int
}

type derived struct {
	visible    bool
	opacity    float64
	clipParent int // -1 none (document flow), -2 the viewport
	clip       Rect
	descClip   Rect
	fixedRoot  int
	stickyRoot int
	layer      int
	ariaHidden bool
	cb         int // containing block for an absolutely or fixed positioned element
}

const (
	clipNone     = -1
	clipViewport = -2
)

// overlayRoles mark a layer that is meant to cover the page.
var overlayRoles = map[string]bool{"dialog": true, "alertdialog": true, "menu": true, "listbox": true, "tooltip": true}

func (doc *Document) isOverlay(n *Node) bool {
	if overlayRoles[n.Attr("role")] {
		return true
	}
	if _, ok := n.Attrs["data-radix-popper-content-wrapper"]; ok {
		return true
	}
	return n.Attr("aria-modal") == "true"
}

// Analyze derives each node's visibility, clip, fixed and sticky root and
// overlay layer. Detectors call it through Audit.
func (doc *Document) Analyze() {
	n := len(doc.Nodes)
	doc.d = make([]derived, n)
	doc.enter = make([]int, n)
	doc.exit = make([]int, n)
	for i := range doc.Nodes {
		doc.Nodes[i].Children = doc.Nodes[i].Children[:0]
	}
	for i, node := range doc.Nodes {
		if node.Parent >= 0 && node.Parent < i {
			p := doc.Nodes[node.Parent]
			p.Children = append(p.Children, i)
		}
	}
	// Euler tour for O(1) ancestor tests.
	clock := 0
	var walk func(i int)
	walk = func(i int) {
		doc.enter[i] = clock
		clock++
		for _, c := range doc.Nodes[i].Children {
			walk(c)
		}
		doc.exit[i] = clock
		clock++
	}
	for i, node := range doc.Nodes {
		if node.Parent < 0 || node.Parent >= i {
			walk(i)
		}
	}

	vr := doc.Viewport.Rect()
	for i, node := range doc.Nodes {
		dv := &doc.d[i]
		p := node.Parent
		hasParent := p >= 0 && p < i
		parentOpacity := 1.0
		dv.fixedRoot, dv.stickyRoot, dv.layer, dv.cb = -1, -1, -1, -1
		if hasParent {
			pd := doc.d[p]
			parentOpacity = pd.opacity
			dv.fixedRoot, dv.stickyRoot, dv.layer, dv.ariaHidden = pd.fixedRoot, pd.stickyRoot, pd.layer, pd.ariaHidden
		}
		op := node.Style.Opacity
		if node.Tag == "#text" {
			op = 1
		}
		dv.opacity = parentOpacity * op
		if node.Attr("aria-hidden") == "true" {
			dv.ariaHidden = true
		}
		dv.visible = node.Layout && node.Style.Visibility != "hidden" && node.Style.Visibility != "collapse" &&
			node.Style.Display != "none" && dv.opacity > 0.02
		if dv.layer == -1 && doc.isOverlay(node) {
			dv.layer = i
		}

		// Clip parent: fixed escapes every clip (unless a transformed ancestor
		// holds it), absolute escapes the clips below its containing block.
		dv.clipParent = clipNone
		if hasParent {
			dv.clipParent = p
		}
		switch node.Style.Position {
		case "fixed":
			if cb := doc.transformedAncestor(i); cb >= 0 {
				dv.cb = cb
				dv.clipParent = cb
			} else {
				dv.clipParent = clipViewport
				if dv.fixedRoot == -1 {
					dv.fixedRoot = i
				}
			}
		case "absolute":
			cb := doc.positionedAncestor(i)
			dv.cb = cb
			dv.clipParent = cb
			if cb < 0 {
				dv.clipParent = clipNone
			}
		case "sticky":
			if dv.stickyRoot == -1 {
				dv.stickyRoot = i
			}
		}
		switch dv.clipParent {
		case clipNone:
			dv.clip = infiniteRect
		case clipViewport:
			dv.clip = vr
		default:
			dv.clip = doc.d[dv.clipParent].descClip
		}
		dv.descClip = dv.clip
		if node.Tag == "html" || node.Tag == "body" {
			// The root's overflow belongs to the viewport, which scrolls; only a
			// hidden horizontal overflow cuts content off at the side.
			if node.Style.OverflowX == "hidden" || node.Style.OverflowX == "clip" {
				dv.descClip = clipAxis(dv.descClip, Rect{X: 0, Y: -1e7, W: doc.Viewport.Width, H: 2e7}, true, false)
			}
			continue
		}
		cx, cy := clips(node.Style.OverflowX), clips(node.Style.OverflowY)
		if (cx || cy) && node.Tag != "#text" {
			box := node.Bounds
			if node.HasClient {
				box = node.Client
			}
			dv.descClip = clipAxis(dv.descClip, box, cx, cy)
		}
	}
}

// positionedAncestor is the containing block of an absolutely positioned
// element: its nearest ancestor that is positioned or transformed.
func (doc *Document) positionedAncestor(i int) int {
	for p := doc.Nodes[i].Parent; p >= 0; p = doc.Nodes[p].Parent {
		s := doc.Nodes[p].Style
		if (s.Position != "" && s.Position != "static") || (s.Transform != "" && s.Transform != "none") {
			return p
		}
	}
	return -1
}

// transformedAncestor is the nearest transformed ancestor, which holds a fixed
// element in place of the viewport.
func (doc *Document) transformedAncestor(i int) int {
	for p := doc.Nodes[i].Parent; p >= 0; p = doc.Nodes[p].Parent {
		if t := doc.Nodes[p].Style.Transform; t != "" && t != "none" {
			return p
		}
	}
	return -1
}

// IsAncestor reports whether a is a strict ancestor of b.
func (doc *Document) IsAncestor(a, b int) bool {
	if a == b || a < 0 || b < 0 {
		return false
	}
	return doc.enter[a] < doc.enter[b] && doc.exit[b] < doc.exit[a]
}

// Visible reports whether a node is rendered and not faded to nothing.
func (doc *Document) Visible(i int) bool { return doc.d[i].visible }

// VisibleRect is the part of a box that its clip chain lets through.
func (doc *Document) VisibleRect(i int, r Rect) Rect { return r.Intersect(doc.d[i].clip) }

// element returns the nearest element at or above a node (a text node's
// parent).
func (doc *Document) element(i int) int {
	for i >= 0 && doc.Nodes[i].Tag == "#text" {
		i = doc.Nodes[i].Parent
	}
	return i
}

// clipAncestors lists, nearest first, the ancestors whose overflow clips node
// i, following the clip-parent chain (so a fixed element has none and an
// absolute one skips the clips below its containing block).
func (doc *Document) clipAncestors(i int) []int {
	var out []int
	for c := doc.d[i].clipParent; c >= 0; c = doc.d[c].clipParent {
		n := doc.Nodes[c]
		if n.Tag == "html" || n.Tag == "body" {
			if n.Style.OverflowX == "hidden" || n.Style.OverflowX == "clip" {
				out = append(out, c)
			}
			continue
		}
		if clips(n.Style.OverflowX) || clips(n.Style.OverflowY) {
			out = append(out, c)
		}
	}
	return out
}

// clientBox is the box an element clips its content to.
func (doc *Document) clientBox(i int) Rect {
	n := doc.Nodes[i]
	if n.Tag == "html" || n.Tag == "body" {
		return Rect{X: 0, Y: -1e7, W: doc.Viewport.Width, H: 2e7}
	}
	if n.HasClient {
		return n.Client
	}
	return n.Bounds
}

// scrollContainerY is the nearest ancestor that scrolls vertically and has
// something to scroll, or -1 when content moves only with the document.
func (doc *Document) scrollContainerY(i int) int {
	for _, c := range doc.clipAncestors(i) {
		n := doc.Nodes[c]
		if n.Tag == "html" || n.Tag == "body" {
			continue
		}
		if scrolls(n.Style.OverflowY) {
			return c
		}
	}
	return -1
}
