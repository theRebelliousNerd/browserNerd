package layout

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// builder constructs a document the way a DOMSnapshot would describe it:
// nodes in document order, parents first.
type builder struct{ doc *Document }

func newDoc(vw, vh float64) *builder {
	doc := &Document{Viewport: Viewport{Width: vw, Height: vh, Scale: 1, DocWidth: vw, DocHeight: vh}}
	b := &builder{doc: doc}
	b.add(&Node{Parent: -1, Tag: "#document", Style: DefaultStyle()})
	b.add(&Node{Parent: 0, Tag: "html", Layout: true, Bounds: Rect{0, 0, vw, vh}, Style: DefaultStyle(), HasClient: true, Client: Rect{0, 0, vw, vh}})
	b.add(&Node{Parent: 1, Tag: "body", Layout: true, Bounds: Rect{0, 0, vw, vh}, Style: DefaultStyle()})
	return b
}

func (b *builder) add(n *Node) int {
	if n.Paint == 0 {
		n.Paint = len(b.doc.Nodes)
	}
	b.doc.Nodes = append(b.doc.Nodes, n)
	return len(b.doc.Nodes) - 1
}

type mod func(*Node)

func style(f func(*Style)) mod { return func(n *Node) { f(&n.Style) } }
func attr(k, v string) mod     { return func(n *Node) { n.Attrs[k] = v } }
func client(r Rect) mod        { return func(n *Node) { n.HasClient, n.Client = true, r } }
func scroll(w, h, top float64) mod {
	return func(n *Node) { n.ScrollW, n.ScrollH, n.ScrollTop = w, h, top }
}
func paint(p int) mod          { return func(n *Node) { n.Paint = p } }
func position(p string) mod    { return style(func(s *Style) { s.Position = p }) }
func overflow(x, y string) mod { return style(func(s *Style) { s.OverflowX, s.OverflowY = x, y }) }
func display(d string) mod     { return style(func(s *Style) { s.Display = d }) }
func background(c string) mod  { return style(func(s *Style) { s.BackgroundColor = c }) }

func (b *builder) el(parent int, tag string, r Rect, mods ...mod) int {
	n := &Node{Parent: parent, Tag: tag, Layout: true, Bounds: r, Style: DefaultStyle(), Attrs: map[string]string{}}
	for _, m := range mods {
		m(n)
	}
	return b.add(n)
}

func (b *builder) text(parent int, s string, boxes ...Rect) int {
	return b.add(&Node{Parent: parent, Tag: "#text", Text: s, Layout: true, Bounds: unionAll(boxes), Style: b.doc.Nodes[parent].Style, TextBoxes: boxes})
}

func unionAll(rs []Rect) Rect {
	var u Rect
	for _, r := range rs {
		u = u.Union(r)
	}
	return u
}

func run(b *builder, opts Options) *Audit {
	a := Run(b.doc, opts)
	a.Finish()
	return a
}

func only(t *testing.T, a *Audit, typ string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range a.Findings {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

func TestRectGeometry(t *testing.T) {
	a := Rect{0, 0, 10, 10}
	b := Rect{5, 5, 10, 10}
	if got := a.Intersect(b); got != (Rect{5, 5, 5, 5}) {
		t.Errorf("Intersect = %v", got)
	}
	if got := a.Intersect(Rect{20, 20, 1, 1}); !got.Empty() {
		t.Errorf("disjoint Intersect = %v", got)
	}
	if got := a.Union(b); got != (Rect{0, 0, 15, 15}) {
		t.Errorf("Union = %v", got)
	}
	parts := outsideParts(Rect{0, 0, 20, 10}, Rect{0, 0, 15, 10})
	if len(parts) != 1 || parts[0] != (Rect{15, 0, 5, 10}) {
		t.Errorf("outsideParts = %v", parts)
	}
	if got := clipAxis(infiniteRect, Rect{10, 10, 50, 50}, true, false); got.X != 10 || got.W != 50 || got.H != infiniteRect.H {
		t.Errorf("clipAxis x-only = %v", got)
	}
}

func TestClipChainHonoursPositioning(t *testing.T) {
	b := newDoc(400, 800)
	box := b.el(2, "div", Rect{0, 0, 100, 100}, overflow("hidden", "hidden"), client(Rect{0, 0, 100, 100}))
	static := b.el(box, "span", Rect{50, 0, 100, 20})
	abs := b.el(box, "span", Rect{50, 30, 100, 20}, position("absolute"))
	fixed := b.el(box, "span", Rect{50, 60, 100, 20}, position("fixed"))
	b.doc.Analyze()
	if got := b.doc.VisibleRect(static, b.doc.Nodes[static].Bounds); got.W != 50 {
		t.Errorf("static child should be clipped to 50px wide, got %v", got)
	}
	if got := b.doc.VisibleRect(abs, b.doc.Nodes[abs].Bounds); got.W != 100 {
		t.Errorf("absolute child with no positioned ancestor escapes the clip, got %v", got)
	}
	if got := b.doc.VisibleRect(fixed, b.doc.Nodes[fixed].Bounds); got.W != 100 {
		t.Errorf("fixed child escapes the clip, got %v", got)
	}
	// A positioned clipping box is the containing block: it clips its
	// absolute child.
	b2 := newDoc(400, 800)
	pbox := b2.el(2, "div", Rect{0, 0, 100, 100}, overflow("hidden", "hidden"), position("relative"), client(Rect{0, 0, 100, 100}))
	abs2 := b2.el(pbox, "span", Rect{50, 30, 100, 20}, position("absolute"))
	b2.doc.Analyze()
	if got := b2.doc.VisibleRect(abs2, b2.doc.Nodes[abs2].Bounds); got.W != 50 {
		t.Errorf("absolute child of a positioned clip should be clipped, got %v", got)
	}
}

func TestOverlapFindsCollidingBadgesAndIgnoresIntentionalLayers(t *testing.T) {
	b := newDoc(390, 844)
	card := b.el(2, "div", Rect{16, 16, 358, 90}, position("relative"))
	b1 := b.el(card, "span", Rect{250, 28, 90, 20}, position("absolute"), background("rgb(29, 78, 216)"), display("inline-block"), attr("class", "badge"))
	b.text(b1, "Proprietary", Rect{258, 30, 74, 16})
	b2 := b.el(card, "span", Rect{220, 30, 90, 20}, position("absolute"), background("rgb(29, 78, 216)"), display("inline-block"))
	b.text(b2, "Proprietary", Rect{228, 32, 74, 16})
	// A count badge on an avatar.
	av := b.el(2, "div", Rect{16, 200, 56, 56}, position("relative"))
	b.el(av, "img", Rect{16, 200, 56, 56})
	cnt := b.el(av, "span", Rect{60, 196, 16, 16}, position("absolute"), background("rgb(0, 0, 0)"))
	b.text(cnt, "3", Rect{64, 196, 8, 16})
	// A menu over a paragraph.
	p := b.el(2, "p", Rect{16, 300, 358, 40})
	b.text(p, "Paragraph under the menu", Rect{16, 300, 300, 18})
	menu := b.el(2, "div", Rect{40, 296, 180, 96}, position("absolute"), attr("role", "menu"), background("rgb(255, 255, 255)"))
	mi := b.el(menu, "div", Rect{40, 296, 180, 48}, attr("role", "menuitem"))
	b.text(mi, "Rename", Rect{52, 311, 60, 18})
	a := run(b, Options{})
	got := only(t, a, TypeOverlap)
	if len(got) != 1 {
		t.Fatalf("want exactly the badge collision, got %d: %+v", len(got), got)
	}
	if got[0].Severity != High || !strings.Contains(got[0].Message, "Proprietary") {
		t.Errorf("badge collision should be high and name the badge: %+v", got[0])
	}
}

func TestTightLeadingIsNotAnOverlap(t *testing.T) {
	b := newDoc(390, 844)
	h := b.el(2, "h3", Rect{16, 16, 358, 16})
	b.text(h, "Heading", Rect{16, 14, 80, 19})
	p := b.el(2, "p", Rect{16, 32, 358, 16})
	b.text(p, "Body line", Rect{16, 30, 80, 19})
	if got := only(t, run(b, Options{}), TypeOverlap); len(got) != 0 {
		t.Errorf("stacked lines touching by 3px are not an overlap: %+v", got)
	}
}

func TestTextBleedClipAndIntentionalTruncation(t *testing.T) {
	b := newDoc(390, 844)
	// A tab whose label runs past its box.
	tab := b.el(2, "button", Rect{16, 16, 90, 48}, attr("role", "tab"))
	b.text(tab, "Opportunities (12)", Rect{22, 31, 124, 18})
	// Cut with no ellipsis.
	cut := b.el(2, "div", Rect{16, 100, 150, 26}, overflow("hidden", "hidden"), client(Rect{17, 101, 148, 24}))
	b.text(cut, "Northwind Industrial Holdings", Rect{21, 104, 205, 18})
	// Cut with an ellipsis: intentional.
	ell := b.el(2, "div", Rect{16, 140, 150, 26}, overflow("hidden", "hidden"), client(Rect{16, 140, 150, 26}), style(func(s *Style) { s.TextOverflow = "ellipsis" }))
	b.text(ell, "Northwind Industrial Holdings", Rect{16, 144, 205, 18})
	// Cut inside a scroller: reachable.
	sc := b.el(2, "div", Rect{16, 180, 150, 26}, overflow("auto", "hidden"), client(Rect{16, 180, 150, 26}))
	b.text(sc, "Northwind Industrial Holdings", Rect{16, 184, 205, 18})
	// Vertically cut inside a line clamp: intentional.
	lc := b.el(2, "p", Rect{16, 220, 200, 36}, overflow("hidden", "hidden"), client(Rect{16, 220, 200, 36}), style(func(s *Style) { s.LineClamp = "2" }))
	b.text(lc, "Line one line two line three", Rect{16, 220, 190, 18}, Rect{16, 238, 190, 18}, Rect{16, 256, 120, 18})

	a := run(b, Options{})
	bleed := only(t, a, TypeTextBleed)
	if len(bleed) != 1 || bleed[0].Severity != High || bleed[0].Elements[0].Tag != "button" {
		t.Errorf("want one high bleed on the tab, got %+v", bleed)
	}
	clipped := only(t, a, TypeTextClipped)
	if len(clipped) != 1 || !strings.Contains(clipped[0].Message, "Northwind") {
		t.Errorf("want one clip (no ellipsis), got %+v", clipped)
	}
	if clipped[0].Elements[0].Rect.Y != 100 {
		t.Errorf("the clip should name the no-ellipsis box, got %+v", clipped[0].Elements)
	}
}

func TestHorizontalOverflowNamesTheCause(t *testing.T) {
	b := newDoc(390, 844)
	b.doc.Viewport.DocWidth = 616
	wrap := b.el(2, "section", Rect{16, 16, 358, 48})
	banner := b.el(wrap, "div", Rect{16, 16, 600, 48}, attr("id", "banner"))
	b.text(banner, "Quarterly targets banner", Rect{16, 30, 180, 18})
	got := only(t, run(b, Options{}), TypeHorizontalOverflow)
	if len(got) != 1 {
		t.Fatalf("want one horizontal overflow, got %+v", got)
	}
	if got[0].Elements[0].Selector != "#banner" {
		t.Errorf("the cause is the banner, not its parent: %+v", got[0].Elements)
	}
}

func TestUnreachableAndToolbarCutoff(t *testing.T) {
	build := func(height string, bottomSet bool) *builder {
		b := newDoc(390, 844)
		b.doc.Viewport.Mobile, b.doc.Viewport.Touch, b.doc.Viewport.ToolbarHeight = true, true, 86
		nav := b.el(2, "nav", Rect{210, 0, 180, 844}, position("fixed"), overflow("auto", "auto"), client(Rect{210, 0, 180, 844}), scroll(180, 912, 0))
		for i := 0; i < 16; i++ {
			y := float64(i * 57)
			l := b.el(nav, "a", Rect{210, y, 180, 56}, attr("href", "#"))
			b.text(l, "Item", Rect{222, y + 19, 40, 18})
		}
		b.doc.Analyze()
		b.doc.Anchors = map[int]Anchor{nav: {TopSet: true, BottomSet: bottomSet, Height: height}}
		return b
	}
	a := run(build("100vh", false), Options{})
	if got := only(t, a, TypeUnreachable); len(got) != 0 {
		t.Errorf("a scrolling menu is reachable: %+v", got)
	}
	if got := only(t, a, TypeToolbarCutoff); len(got) != 1 {
		t.Errorf("a 100vh top-anchored menu loses its bottom under the toolbar, got %+v", got)
	}
	if got := only(t, run(build("100dvh", false), Options{}), TypeToolbarCutoff); len(got) != 0 {
		t.Errorf("a dvh menu follows the toolbar: %+v", got)
	}
	if got := only(t, run(build("auto", true), Options{}), TypeToolbarCutoff); len(got) != 0 {
		t.Errorf("a bottom-anchored menu follows the toolbar: %+v", got)
	}

	// Without scrolling, the items past the viewport are lost.
	b := newDoc(390, 844)
	nav := b.el(2, "nav", Rect{0, 0, 180, 844}, position("fixed"), overflow("hidden", "hidden"), client(Rect{0, 0, 180, 844}))
	for i := 0; i < 16; i++ {
		y := float64(i * 57)
		l := b.el(nav, "a", Rect{0, y, 180, 56}, attr("href", "#"))
		b.text(l, "Item", Rect{12, y + 19, 40, 18})
	}
	got := only(t, run(b, Options{}), TypeUnreachable)
	if len(got) != 1 || got[0].Metrics["hidden_items"] != 2 {
		t.Errorf("want the last two items unreachable, got %+v", got)
	}
}

func TestNonCollapsingColumns(t *testing.T) {
	build := func(width float64) *builder {
		b := newDoc(width, 844)
		row := b.el(2, "div", Rect{16, 16, width - 32, 200}, display("flex"))
		colW := (width - 48) / 3
		for i := 0; i < 3; i++ {
			x := 16 + float64(i)*(colW+8)
			c := b.el(row, "div", Rect{x, 16, colW, 200})
			var boxes []Rect
			for l := 0; l < 6; l++ {
				boxes = append(boxes, Rect{x + 4, 20 + float64(l)*18, colW - 8, 18})
			}
			b.text(c, "Quarter over quarter revenue grew across every region", boxes...)
		}
		return b
	}
	if got := only(t, run(build(390), Options{}), TypeNonCollapsing); len(got) != 1 || got[0].Severity != High {
		t.Errorf("three squeezed columns on a phone: %+v", got)
	}
	if got := only(t, run(build(1024), Options{}), TypeNonCollapsing); len(got) != 0 {
		t.Errorf("columns on a desktop are fine: %+v", got)
	}
}

func TestTapTargets(t *testing.T) {
	b := newDoc(390, 844)
	b.doc.Viewport.Touch = true
	small := b.el(2, "button", Rect{16, 16, 20, 20}, attr("aria-label", "Close"))
	_ = small
	p := b.el(2, "p", Rect{16, 60, 358, 20})
	b.text(p, "Read the ", Rect{16, 60, 70, 18})
	link := b.el(p, "a", Rect{86, 60, 40, 18}, attr("href", "#terms"), display("inline"))
	b.text(link, "terms", Rect{86, 60, 40, 18})
	b.text(p, " before you continue.", Rect{126, 60, 150, 18})
	label := b.el(2, "label", Rect{16, 100, 358, 48}, display("flex"))
	b.el(label, "input", Rect{16, 116, 13, 13}, attr("type", "checkbox"))
	ok := b.el(2, "button", Rect{16, 160, 120, 48})
	b.text(ok, "Continue", Rect{30, 175, 70, 18})
	got := only(t, run(b, Options{}), TypeTapTarget)
	if len(got) != 1 || !strings.Contains(got[0].Message, "Close") || got[0].Severity != Medium {
		t.Errorf("only the 20px close button is too small: %+v", got)
	}
	b.doc.Viewport.Touch = false
	if got := only(t, run(b, Options{}), TypeTapTarget); len(got) != 0 {
		t.Errorf("no tap-target check without touch: %+v", got)
	}
}

func TestOcclusionOnlyWhenPermanent(t *testing.T) {
	build := func(titleY float64) *builder {
		b := newDoc(390, 844)
		b.doc.Viewport.DocHeight = 2000
		hd := b.el(2, "header", Rect{0, 0, 390, 60}, position("fixed"), background("rgb(18, 52, 86)"), paint(100))
		b.text(hd, "Partner hub", Rect{16, 20, 90, 18})
		h1 := b.el(2, "h1", Rect{0, titleY, 390, 30}, paint(10))
		b.text(h1, "Welcome back", Rect{16, titleY + 2, 160, 28})
		return b
	}
	if got := only(t, run(build(12), Options{}), TypeOcclusion); len(got) != 1 {
		t.Errorf("a title under the fixed header at the top of the page is always covered: %+v", got)
	}
	if got := only(t, run(build(80), Options{}), TypeOcclusion); len(got) != 0 {
		t.Errorf("a padded page scrolls its title clear: %+v", got)
	}
}

func TestFinishGroupsRepeatsAndNumbers(t *testing.T) {
	b := newDoc(390, 844)
	b.doc.Viewport.Touch = true
	list := b.el(2, "ul", Rect{0, 0, 390, 300}, attr("data-testid", "rows"))
	for i := 0; i < 3; i++ {
		li := b.el(list, "li", Rect{0, float64(i) * 40, 390, 40})
		b.el(li, "button", Rect{350, float64(i)*40 + 10, 20, 20})
	}
	a := run(b, Options{})
	got := only(t, a, TypeTapTarget)
	if len(got) != 1 || got[0].Count != 3 || len(got[0].Members) != 2 {
		t.Fatalf("three alike buttons group into one row of 3: %+v", got)
	}
	if got[0].N != 1 || got[0].ID == "" {
		t.Errorf("the row is numbered and has an id: %+v", got[0])
	}
}

func TestSelectors(t *testing.T) {
	b := newDoc(390, 844)
	grid := b.el(2, "div", Rect{0, 0, 390, 100}, attr("data-testid", "services-grid"))
	b.el(grid, "div", Rect{0, 0, 190, 100})
	card := b.el(grid, "div", Rect{200, 0, 190, 100})
	gen := b.el(card, "span", Rect{200, 0, 50, 20}, attr("id", ":r1a:"))
	named := b.el(card, "span", Rect{250, 0, 50, 20}, attr("id", "price"))
	b.doc.Analyze()
	if got := b.doc.Selector(gen); got != `[data-testid="services-grid"] > div:nth-of-type(2) > span:nth-of-type(1)` {
		t.Errorf("generated ids are skipped for a path: %s", got)
	}
	if got := b.doc.Selector(named); got != "#price" {
		t.Errorf("a stable id is the selector: %s", got)
	}
}

func TestAnchorUnits(t *testing.T) {
	cases := []struct {
		a              Anchor
		dynamic, large bool
	}{
		{Anchor{TopSet: true, Height: "100vh"}, false, true},
		{Anchor{TopSet: true, Height: "100dvh"}, true, false},
		{Anchor{TopSet: true, Height: "100vh", MaxHeight: "100dvh"}, true, true},
		{Anchor{TopSet: true, BottomSet: true}, true, false},
		{Anchor{TopSet: true, MinHeight: "calc(100vh - 4rem)"}, false, true},
		{Anchor{TopSet: true, Height: "100%"}, true, false},
	}
	for _, c := range cases {
		if c.a.Dynamic() != c.dynamic || c.a.LargeViewportSized() != c.large {
			t.Errorf("%+v: dynamic=%v large=%v, want %v %v", c.a, c.a.Dynamic(), c.a.LargeViewportSized(), c.dynamic, c.large)
		}
	}
}

// glyphs paints a text-like pattern of short dark strokes into a rect.
func glyphs(img *image.RGBA, r image.Rectangle, phase int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if (x+phase)%4 == 0 || ((y+phase)%5 == 0 && (x/3)%2 == 0) {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
}

func white(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	return img
}

func TestCVOverlapVerdicts(t *testing.T) {
	mk := func() *Audit {
		b := newDoc(200, 100)
		s1 := b.el(2, "span", Rect{0, 0, 100, 20}, position("absolute"))
		b.text(s1, "first", Rect{0, 0, 100, 20})
		s2 := b.el(2, "span", Rect{50, 0, 100, 20}, position("absolute"))
		b.text(s2, "second", Rect{50, 0, 100, 20})
		return Run(b.doc, Options{})
	}
	// Both labels drawn where they meet: a collision.
	img := white(200, 100)
	glyphs(img, image.Rect(0, 0, 100, 20), 0)
	glyphs(img, image.Rect(50, 0, 150, 20), 2)
	a := mk()
	a.ApplyCV(NewShot(img, 0, 0, 1))
	if a.Findings[0].CV != CVCollision {
		t.Errorf("two labels drawn over each other: cv=%s", a.Findings[0].CV)
	}
	// Nothing drawn where the boxes meet: a padding overlap.
	img = white(200, 100)
	glyphs(img, image.Rect(0, 0, 45, 20), 0)
	glyphs(img, image.Rect(105, 0, 150, 20), 2)
	a = mk()
	a.ApplyCV(NewShot(img, 0, 0, 1))
	if a.Findings[0].CV != CVNoInk || a.Findings[0].Severity != Low {
		t.Errorf("an overlap of empty box parts drops to low: cv=%s sev=%s", a.Findings[0].CV, a.Findings[0].Severity)
	}
	// One label drawn, the other hidden under it: covered.
	img = white(200, 100)
	glyphs(img, image.Rect(0, 0, 150, 20), 0)
	a = mk()
	a.ApplyCV(NewShot(img, 0, 0, 1))
	if a.Findings[0].CV != CVCovered {
		t.Errorf("one label over the other: cv=%s", a.Findings[0].CV)
	}
}

func TestCVEdgeScan(t *testing.T) {
	build := func(inScroller bool) *Audit {
		b := newDoc(200, 100)
		parent := 2
		if inScroller {
			parent = b.el(2, "div", Rect{0, 0, 200, 100}, overflow("auto", "hidden"), client(Rect{0, 0, 200, 100}))
		}
		p := b.el(parent, "p", Rect{150, 40, 50, 20})
		b.text(p, "cut words", Rect{150, 40, 50, 20})
		return Run(b.doc, Options{})
	}
	img := white(200, 100)
	glyphs(img, image.Rect(150, 40, 200, 60), 0)
	a := build(false)
	a.ApplyCV(NewShot(img, 0, 0, 1))
	if got := only(t, a, TypeEdgeCut); len(got) != 1 {
		t.Errorf("glyphs sliced at the right edge: %+v", got)
	}
	a = build(true)
	a.ApplyCV(NewShot(img, 0, 0, 1))
	if got := only(t, a, TypeEdgeCut); len(got) != 0 {
		t.Errorf("a sideways scroller is cut on purpose: %+v", got)
	}
}

func TestAnnotateDrawsNumberedFindings(t *testing.T) {
	b := newDoc(200, 100)
	s1 := b.el(2, "span", Rect{0, 0, 100, 20}, position("absolute"))
	b.text(s1, "first", Rect{0, 0, 100, 20})
	s2 := b.el(2, "span", Rect{50, 0, 100, 20}, position("absolute"))
	b.text(s2, "second", Rect{50, 0, 100, 20})
	a := run(b, Options{})
	img := white(200, 100)
	if n := a.Annotate(img, 0, 0, 1); n != 1 {
		t.Fatalf("drew %d findings, want 1", n)
	}
	if c := img.RGBAAt(75, 10); c == (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("the overlap region is not filled")
	}
	small, f := Downscale(white(1170, 2532), 800)
	if f >= 1 || small.Bounds().Dx() != 800 {
		t.Errorf("Downscale: %v %v", small.Bounds(), f)
	}
}
