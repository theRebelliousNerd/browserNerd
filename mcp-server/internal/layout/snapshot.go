package layout

import (
	"fmt"
	"strings"

	"github.com/go-rod/rod/lib/proto"
)

// FromSnapshot builds a Document from the main frame of a DOMSnapshot capture
// taken with StyleProperties as its computed styles, with DOM rects and paint
// order. Iframe documents are left out.
func FromSnapshot(res *proto.DOMSnapshotCaptureSnapshotResult, vp Viewport) (*Document, error) {
	if res == nil || len(res.Documents) == 0 {
		return nil, fmt.Errorf("the snapshot holds no document")
	}
	d := res.Documents[0]
	if d.Nodes == nil || d.Layout == nil {
		return nil, fmt.Errorf("the snapshot holds no node or layout tree")
	}
	str := func(i int) string {
		if i < 0 || i >= len(res.Strings) {
			return ""
		}
		return res.Strings[i]
	}
	nodes := d.Nodes
	count := len(nodes.NodeName)
	doc := &Document{Nodes: make([]*Node, count), Viewport: vp}

	pseudo := map[int]string{}
	if nodes.PseudoType != nil {
		for k, idx := range nodes.PseudoType.Index {
			if k < len(nodes.PseudoType.Value) {
				pseudo[idx] = str(int(nodes.PseudoType.Value[k]))
			}
		}
	}
	for i := 0; i < count; i++ {
		parent := -1
		if i < len(nodes.ParentIndex) {
			parent = nodes.ParentIndex[i]
		}
		name := strings.ToLower(str(int(nodes.NodeName[i])))
		n := &Node{Parent: parent, Tag: name, Style: DefaultStyle(), Paint: -1}
		if i < len(nodes.BackendNodeID) {
			n.BackendID = int(nodes.BackendNodeID[i])
		}
		if i < len(nodes.NodeType) && nodes.NodeType[i] == 3 {
			n.Tag = "#text"
			if i < len(nodes.NodeValue) {
				n.Text = str(int(nodes.NodeValue[i]))
			}
		}
		if p, ok := pseudo[i]; ok {
			n.Pseudo = p
			n.Tag = "::" + p
		}
		if i < len(nodes.Attributes) && len(nodes.Attributes[i]) > 0 {
			attrs := nodes.Attributes[i]
			n.Attrs = make(map[string]string, len(attrs)/2)
			for k := 0; k+1 < len(attrs); k += 2 {
				n.Attrs[strings.ToLower(str(int(attrs[k])))] = str(int(attrs[k+1]))
			}
		}
		doc.Nodes[i] = n
	}

	lay := d.Layout
	rect := func(rs []proto.DOMSnapshotRectangle, k int) ([]float64, bool) {
		if k >= len(rs) || len(rs[k]) < 4 {
			return nil, false
		}
		return rs[k], true
	}
	for k, ni := range lay.NodeIndex {
		if ni < 0 || ni >= count {
			continue
		}
		n := doc.Nodes[ni]
		b, ok := rect(lay.Bounds, k)
		if !ok {
			continue
		}
		n.Layout = true
		n.Bounds = Rect{X: b[0], Y: b[1], W: b[2], H: b[3]}
		if k < len(lay.Styles) {
			vals := make([]string, len(lay.Styles[k]))
			for j, s := range lay.Styles[k] {
				vals[j] = str(int(s))
			}
			n.Style = StyleFromValues(vals)
		}
		if c, ok := rect(lay.ClientRects, k); ok {
			n.HasClient = true
			n.Client = Rect{X: n.Bounds.X + c[0], Y: n.Bounds.Y + c[1], W: c[2], H: c[3]}
		}
		if s, ok := rect(lay.ScrollRects, k); ok {
			n.ScrollLeft, n.ScrollTop, n.ScrollW, n.ScrollH = s[0], s[1], s[2], s[3]
		}
		if k < len(lay.PaintOrders) {
			n.Paint = lay.PaintOrders[k]
		}
	}
	if d.TextBoxes != nil {
		layoutToNode := lay.NodeIndex
		for k, li := range d.TextBoxes.LayoutIndex {
			if li < 0 || li >= len(layoutToNode) {
				continue
			}
			b, ok := rect(d.TextBoxes.Bounds, k)
			if !ok || b[2] <= 0 || b[3] <= 0 {
				continue
			}
			ni := layoutToNode[li]
			if ni < 0 || ni >= count {
				continue
			}
			doc.Nodes[ni].TextBoxes = append(doc.Nodes[ni].TextBoxes, Rect{X: b[0], Y: b[1], W: b[2], H: b[3]})
		}
	}
	return doc, nil
}
