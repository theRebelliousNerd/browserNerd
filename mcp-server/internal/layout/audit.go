package layout

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Finding types.
const (
	TypeOverlap            = "overlap"
	TypeTextBleed          = "text_bleed"
	TypeTextClipped        = "text_clipped"
	TypeHorizontalOverflow = "horizontal_overflow"
	TypeOffscreen          = "offscreen"
	TypeClipped            = "clipped"
	TypeUnreachable        = "unreachable"
	TypeToolbarCutoff      = "toolbar_cutoff"
	TypeNonCollapsing      = "non_collapsing"
	TypeTapTarget          = "tap_target"
	TypeOcclusion          = "occlusion"
	TypeEdgeCut            = "edge_cut"
)

// AllTypes lists every finding type in report order.
var AllTypes = []string{
	TypeOverlap, TypeTextBleed, TypeTextClipped, TypeHorizontalOverflow, TypeOffscreen,
	TypeClipped, TypeUnreachable, TypeToolbarCutoff, TypeNonCollapsing, TypeTapTarget,
	TypeOcclusion, TypeEdgeCut,
}

// Severities.
const (
	High   = "high"
	Medium = "medium"
	Low    = "low"
)

func severityRank(s string) int {
	switch s {
	case High:
		return 3
	case Medium:
		return 2
	case Low:
		return 1
	}
	return 0
}

// CV verdicts.
const (
	CVNotChecked   = "not_checked"
	CVNotInView    = "not_in_view"
	CVCollision    = "collision"
	CVCovered      = "covered"
	CVNoInk        = "no_ink"
	CVCutVisible   = "cut_visible"
	CVBleedVisible = "bleed_visible"
	CVCleanEdge    = "clean_edge"
	CVOnly         = "cv_only"
)

// Options tune the audit. Zero values take the defaults.
type Options struct {
	// Checks limits the finding types; empty means all.
	Checks map[string]bool
	// MinOverlapPx is the smallest intersection, in both axes, that counts.
	MinOverlapPx float64
	// TapTargetPx is the smallest comfortable control on a touch screen.
	TapTargetPx float64
	// MinColumnPx is the narrowest a side-by-side column may be on a phone.
	MinColumnPx float64
	// CollapseBelow is the width under which columns should have stacked.
	CollapseBelow float64
	// MaxFindings caps the findings kept after grouping.
	MaxFindings int
}

// WithDefaults fills unset options.
func (o Options) WithDefaults() Options {
	if o.MinOverlapPx <= 0 {
		o.MinOverlapPx = 3
	}
	if o.TapTargetPx <= 0 {
		o.TapTargetPx = 44
	}
	if o.MinColumnPx <= 0 {
		o.MinColumnPx = 140
	}
	if o.CollapseBelow <= 0 {
		o.CollapseBelow = 640
	}
	if o.MaxFindings <= 0 {
		o.MaxFindings = 200
	}
	return o
}

func (o Options) enabled(t string) bool { return len(o.Checks) == 0 || o.Checks[t] }

// ElementRef names one element involved in a finding.
type ElementRef struct {
	Node     int    `json:"-"`
	Selector string `json:"selector"`
	Ref      string `json:"ref,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Label    string `json:"label,omitempty"`
	Rect     Rect   `json:"rect"`
}

// Finding is one spatial defect.
type Finding struct {
	ID       string             `json:"id"`
	N        int                `json:"n,omitempty"`
	Type     string             `json:"type"`
	Severity string             `json:"severity"`
	Message  string             `json:"message"`
	Elements []ElementRef       `json:"elements"`
	Rect     Rect               `json:"rect"`
	InView   bool               `json:"in_view"`
	CV       string             `json:"cv"`
	Count    int                `json:"count,omitempty"`
	Members  []string           `json:"members,omitempty"`
	Metrics  map[string]float64 `json:"metrics,omitempty"`
	// units are the content units behind the finding, for the CV pass.
	units []int
	// cutEdge is the edge a cut or bleed happens at: "left", "right", "top" or
	// "bottom", with its coordinate.
	cutEdge string
	cutAt   float64
}

// Audit is one measured page: its findings and the units they were drawn
// from, which the CV pass reads.
type Audit struct {
	Doc      *Document
	Units    []Unit
	Findings []Finding
	Options  Options
}

// Run analyzes the document and runs every enabled detector. The findings are
// not yet grouped or numbered: call Finish after the CV pass.
func Run(doc *Document, opts Options) *Audit {
	opts = opts.WithDefaults()
	doc.Analyze()
	a := &Audit{Doc: doc, Units: doc.buildUnits(), Options: opts}
	grid := newGrid(a.Units, 64)
	if opts.enabled(TypeOverlap) {
		a.detectOverlaps(grid)
	}
	if opts.enabled(TypeTextBleed) || opts.enabled(TypeTextClipped) {
		a.detectText(grid)
	}
	if opts.enabled(TypeHorizontalOverflow) || opts.enabled(TypeOffscreen) {
		a.detectHorizontalOverflow()
	}
	if opts.enabled(TypeClipped) {
		a.detectClipped()
	}
	if opts.enabled(TypeUnreachable) || opts.enabled(TypeToolbarCutoff) {
		a.detectFixedLayers()
	}
	if opts.enabled(TypeNonCollapsing) {
		a.detectNonCollapsing()
	}
	if opts.enabled(TypeTapTarget) {
		a.detectTapTargets()
	}
	if opts.enabled(TypeOcclusion) {
		a.detectOcclusion()
	}
	vr := doc.Viewport.Rect()
	for k := range a.Findings {
		f := &a.Findings[k]
		f.InView = !f.Rect.Intersect(vr).Empty()
		if f.CV == "" {
			f.CV = CVNotChecked
		}
	}
	return a
}

// ref builds an ElementRef for a node.
func (a *Audit) ref(i int) ElementRef {
	doc := a.Doc
	i = doc.element(i)
	if i < 0 {
		return ElementRef{Node: -1}
	}
	n := doc.Nodes[i]
	return ElementRef{Node: i, Selector: doc.Selector(i), Tag: n.Tag, Label: doc.Label(i), Rect: n.Bounds.Round()}
}

func (a *Audit) add(f Finding) {
	f.Rect = f.Rect.Round()
	a.Findings = append(a.Findings, f)
}

// Finish drops findings the CV pass downgraded below any use, groups repeats of
// one pattern, ranks by severity and size, caps the list, numbers the rows,
// and stamps each finding's stable id.
func (a *Audit) Finish() {
	type key struct{ typ, pattern, sev string }
	groups := map[key]int{}
	var out []Finding
	for _, f := range a.Findings {
		var pats []string
		for _, e := range f.Elements {
			pats = append(pats, patternOf(e.Selector))
		}
		k := key{f.Type, strings.Join(pats, "|"), f.Severity}
		if idx, ok := groups[k]; ok && f.Type != TypeHorizontalOverflow {
			g := &out[idx]
			if g.Count == 0 {
				g.Count = 1
			}
			g.Count++
			if len(g.Members) < 5 && len(f.Elements) > 0 {
				g.Members = append(g.Members, f.Elements[0].Selector)
			}
			if !g.InView && f.InView {
				// Show the member the screenshot can prove.
				members, count := g.Members, g.Count
				*g = f
				g.Members, g.Count = members, count
			}
			continue
		}
		groups[k] = len(out)
		out = append(out, f)
	}
	for k := range out {
		if out[k].Count > 1 {
			out[k].Message = fmt.Sprintf("%s (%d alike)", out[k].Message, out[k].Count)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if si != sj {
			return si > sj
		}
		if out[i].InView != out[j].InView {
			return out[i].InView
		}
		return out[i].Rect.Area()*float64(maxI(out[i].Count, 1)) > out[j].Rect.Area()*float64(maxI(out[j].Count, 1))
	})
	if len(out) > a.Options.MaxFindings {
		out = out[:a.Options.MaxFindings]
	}
	for k := range out {
		out[k].N = k + 1
		out[k].ID = findingID(out[k], a.Doc.Viewport.Width)
	}
	a.Findings = out
}

func findingID(f Finding, width float64) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%.0f", f.Type, width)
	for _, e := range f.Elements {
		fmt.Fprintf(h, "|%s", patternOf(e.Selector))
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// Counts tallies findings by type and severity, groups counted by members.
func (a *Audit) Counts() map[string]interface{} {
	byType := map[string]int{}
	bySev := map[string]int{}
	for _, f := range a.Findings {
		n := maxI(f.Count, 1)
		byType[f.Type] += n
		bySev[f.Severity] += n
	}
	return map[string]interface{}{"by_type": byType, "by_severity": bySev}
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fmtPx(v float64) string {
	return fmt.Sprintf("%.0fpx", math.Round(v))
}

// fmtSize writes a width and height as "189x15px".
func fmtSize(w, h float64) string {
	return fmt.Sprintf("%.0fx%.0fpx", math.Round(w), math.Round(h))
}

func quote(label string) string {
	if label == "" {
		return ""
	}
	return fmt.Sprintf("%q", label)
}

// describe names an element for a message: its label if it has one, else its
// tag.
func (a *Audit) describe(i int) string {
	e := a.Doc.element(i)
	if e < 0 {
		return "an element"
	}
	if l := a.Doc.Label(e); l != "" {
		return fmt.Sprintf("%s %q", a.Doc.Nodes[e].Tag, l)
	}
	return a.Doc.Nodes[e].Tag
}

// grid is a uniform spatial index over unit rects.
type grid struct {
	cell  float64
	cells map[[2]int][]int
}

func newGrid(units []Unit, cell float64) *grid {
	g := &grid{cell: cell, cells: map[[2]int][]int{}}
	for _, u := range units {
		r := u.Rect.Union(u.Vis)
		for _, t := range u.Texts {
			r = r.Union(t)
		}
		g.insert(u.ID, r)
	}
	return g
}

func (g *grid) span(r Rect) (int, int, int, int) {
	return int(math.Floor(r.X / g.cell)), int(math.Floor(r.Y / g.cell)), int(math.Floor(r.Right() / g.cell)), int(math.Floor(r.Bottom() / g.cell))
}

func (g *grid) insert(id int, r Rect) {
	if r.Empty() {
		return
	}
	x1, y1, x2, y2 := g.span(r)
	for x := x1; x <= x2; x++ {
		for y := y1; y <= y2; y++ {
			g.cells[[2]int{x, y}] = append(g.cells[[2]int{x, y}], id)
		}
	}
}

// query returns the ids of units whose indexed box may meet r, each once.
func (g *grid) query(r Rect) []int {
	if r.Empty() {
		return nil
	}
	x1, y1, x2, y2 := g.span(r)
	seen := map[int]bool{}
	var out []int
	for x := x1; x <= x2; x++ {
		for y := y1; y <= y2; y++ {
			for _, id := range g.cells[[2]int{x, y}] {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	sort.Ints(out)
	return out
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}
