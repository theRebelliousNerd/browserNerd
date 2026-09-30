package mcp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/layout"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// browser-observe mode "layout": the responsive layout audit
// (docs/specs/responsive-layout-audit.md).

const (
	layoutDefaultItems      = 20
	layoutMultiDefaultItems = 10
	layoutMaxViewports      = 6
	layoutShotMaxWidth      = 800
	layoutNavigateTimeoutMs = 15000
)

var layoutThresholdKeys = []string{"min_overlap_px", "tap_target_px", "min_column_px", "collapse_below", "max_findings"}

// layoutOptions reads checks and thresholds, refusing names it does not know.
func layoutOptions(args map[string]interface{}) (layout.Options, error) {
	var opts layout.Options
	if raw, ok := args["checks"]; ok {
		list, ok := raw.([]interface{})
		if !ok {
			return opts, fmt.Errorf("checks must be an array of finding types")
		}
		opts.Checks = map[string]bool{}
		for _, item := range list {
			name, _ := item.(string)
			name = strings.ToLower(strings.TrimSpace(name))
			known := false
			for _, t := range layout.AllTypes {
				if t == name {
					known = true
				}
			}
			if !known {
				msg := fmt.Sprintf("unknown check %q; use %s", name, strings.Join(layout.AllTypes, ", "))
				if s := closestName(name, layout.AllTypes); s != "" {
					msg = fmt.Sprintf("unknown check %q; did you mean %q?", name, s)
				}
				return opts, fmt.Errorf("%s", msg)
			}
			opts.Checks[name] = true
		}
	}
	if raw, ok := args["thresholds"]; ok {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return opts, fmt.Errorf("thresholds must be an object with %s", strings.Join(layoutThresholdKeys, ", "))
		}
		for k, v := range m {
			n, ok := numberValue(v)
			if !ok || n <= 0 {
				return opts, fmt.Errorf("threshold %s must be a positive number", k)
			}
			switch k {
			case "min_overlap_px":
				opts.MinOverlapPx = n
			case "tap_target_px":
				opts.TapTargetPx = n
			case "min_column_px":
				opts.MinColumnPx = n
			case "collapse_below":
				opts.CollapseBelow = n
			case "max_findings":
				opts.MaxFindings = int(n)
			default:
				msg := fmt.Sprintf("unknown threshold %q; use %s", k, strings.Join(layoutThresholdKeys, ", "))
				if s := closestName(k, layoutThresholdKeys); s != "" {
					msg = fmt.Sprintf("unknown threshold %q; did you mean %q?", k, s)
				}
				return opts, fmt.Errorf("%s", msg)
			}
		}
	}
	return opts.WithDefaults(), nil
}

// layoutViewports reads the viewports argument: preset names or viewport
// objects, each resolved against the configured default.
func layoutViewports(raw interface{}, base browser.Viewport) ([]browser.Viewport, error) {
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("viewports must be an array of preset names or viewport objects")
	}
	if len(list) > layoutMaxViewports {
		return nil, fmt.Errorf("at most %d viewports per call", layoutMaxViewports)
	}
	out := make([]browser.Viewport, 0, len(list))
	for k, item := range list {
		var req browser.ViewportRequest
		switch v := item.(type) {
		case string:
			req.Preset = v
		case map[string]interface{}:
			for key := range v {
				allowed := false
				for _, f := range viewportFields {
					if f == key {
						allowed = true
					}
				}
				if !allowed {
					return nil, fmt.Errorf("viewports[%d]: unknown field %q; use %s", k, key, strings.Join(viewportFields, ", "))
				}
			}
			req = viewportRequestFromOp(v)
		default:
			return nil, fmt.Errorf("viewports[%d] must be a preset name or an object", k)
		}
		vp, err := browser.ResolveViewport(req, base)
		if err != nil {
			return nil, fmt.Errorf("viewports[%d]: %v", k, err)
		}
		out = append(out, vp)
	}
	return out, nil
}

// observeLayout is browser-observe mode "layout".
func (t *BrowserObserveTool) observeLayout(ctx context.Context, sessionID string, args map[string]interface{}, view string) (map[string]interface{}, error) {
	fail := func(msg string) map[string]interface{} {
		return map[string]interface{}{"success": false, "status": "error", "mode": "layout", "error": msg}
	}
	opts, err := layoutOptions(args)
	if err != nil {
		return fail(err.Error()), nil
	}
	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return fail(fmt.Sprintf("session not found: %s", sessionID)), nil
	}
	url := getStringArg(args, "url")
	fullPage := getBoolArg(args, "full_page", false)

	var viewports []browser.Viewport
	if raw, ok := args["viewports"]; ok {
		viewports, err = layoutViewports(raw, t.sessions.DefaultViewport())
		if err != nil {
			return fail(err.Error()), nil
		}
	}
	maxItems := getIntArg(args, "max_items", layoutDefaultItems)
	if !argHasInt(args, "max_items") && len(viewports) > 1 {
		maxItems = layoutMultiDefaultItems
	}
	if maxItems <= 0 {
		maxItems = layoutDefaultItems
	}

	var blocks []*layoutBlock
	if len(viewports) == 0 {
		if url != "" {
			if navErr := t.layoutNavigate(ctx, sessionID, url); navErr != "" {
				return fail(navErr), nil
			}
		}
		b, err := t.runLayoutAudit(ctx, sessionID, page, t.sessions.SessionViewport(sessionID), opts, fullPage)
		if err != nil {
			return fail(err.Error()), nil
		}
		blocks = append(blocks, b)
	} else {
		hadOverride := t.sessions.HasViewportOverride(sessionID)
		prior := t.sessions.SessionViewport(sessionID)
		defer func() {
			restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if hadOverride {
				_, _ = t.sessions.SetViewport(restoreCtx, sessionID, prior)
			} else {
				_, _ = t.sessions.ResetViewport(restoreCtx, sessionID)
			}
		}()
		for _, vp := range viewports {
			applied, err := t.sessions.SetViewport(ctx, sessionID, vp)
			if err != nil {
				return fail(fmt.Sprintf("set viewport %s: %v", vp, err)), nil
			}
			navNote := ""
			if url != "" {
				navNote = t.layoutNavigate(ctx, sessionID, url)
			}
			b, err := t.runLayoutAudit(ctx, sessionID, page, applied, opts, fullPage)
			if err != nil {
				return fail(fmt.Sprintf("audit at %s: %v", vp, err)), nil
			}
			b.navigateError = navNote
			blocks = append(blocks, b)
		}
	}
	return t.layoutResponse(ctx, sessionID, blocks, view, maxItems), nil
}

// layoutNavigate loads url in the session and waits for it to go quiet; it
// returns why it failed, or "".
func (t *BrowserObserveTool) layoutNavigate(ctx context.Context, sessionID, url string) string {
	res, err := (&NavigateURLTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
		"session_id": sessionID, "url": url, "wait_until": "networkidle", "timeout_ms": layoutNavigateTimeoutMs,
	})
	if err != nil {
		return fmt.Sprintf("navigate %s: %v", url, err)
	}
	if m := asMap(res); m["success"] == false {
		return fmt.Sprintf("navigate %s: %v", url, m["error"])
	}
	return ""
}

// layoutBlock is one viewport's audit.
type layoutBlock struct {
	viewport      browser.Viewport
	audit         *layout.Audit
	shotPath      string
	shotW, shotH  int
	annotated     int
	anchorsNote   string
	navigateError string
	elapsed       time.Duration
}

// runLayoutAudit captures the page's geometry and pixels at its current size
// and runs the detectors and the CV pass.
func (t *BrowserObserveTool) runLayoutAudit(ctx context.Context, sessionID string, page *rod.Page, vp browser.Viewport, opts layout.Options, fullPage bool) (*layoutBlock, error) {
	start := time.Now()
	p := page.Context(ctx)
	browser.SettleLayout(ctx, page)
	lm, err := proto.PageGetLayoutMetrics{}.Call(p)
	if err != nil {
		return nil, fmt.Errorf("layout metrics: %w", err)
	}
	snap, err := proto.DOMSnapshotCaptureSnapshot{ComputedStyles: layout.StyleProperties, IncludeDOMRects: true, IncludePaintOrder: true}.Call(p)
	if err != nil {
		return nil, fmt.Errorf("DOM snapshot: %w", err)
	}
	lvp := layout.Viewport{Scale: vp.DeviceScaleFactor, Mobile: vp.Mobile, Touch: vp.Touch, ToolbarHeight: float64(vp.ToolbarHeight), Preset: vp.Preset}
	if lm.CSSLayoutViewport != nil {
		lvp.Width, lvp.Height = float64(lm.CSSLayoutViewport.ClientWidth), float64(lm.CSSLayoutViewport.ClientHeight)
		lvp.ScrollX, lvp.ScrollY = float64(lm.CSSLayoutViewport.PageX), float64(lm.CSSLayoutViewport.PageY)
	} else {
		lvp.Width, lvp.Height = float64(vp.Width), float64(vp.Height)
	}
	if lm.CSSContentSize != nil {
		lvp.DocWidth, lvp.DocHeight = lm.CSSContentSize.Width, lm.CSSContentSize.Height
	}
	doc, err := layout.FromSnapshot(snap, lvp)
	if err != nil {
		return nil, err
	}
	layoutW := lvp.Width
	if icbW, icbH, ok := doc.InitialContainingBlock(); ok && icbW+1 < lvp.Width {
		// A mobile page wider than the device: Chrome widens the layout viewport
		// to the content and shows it zoomed out. Measure against the device.
		doc.Viewport.LayoutWidth = lvp.Width
		doc.Viewport.Width, doc.Viewport.Height = icbW, icbH
	}
	block := &layoutBlock{viewport: vp}
	if cands := doc.AnchorCandidates(); len(cands) > 0 {
		anchors, probeErr := probeAnchors(p, doc, cands)
		doc.Anchors = anchors
		if probeErr != nil {
			doc.AnchorsErr = probeErr.Error()
			block.anchorsNote = "toolbar_cutoff not checked: " + probeErr.Error()
		}
	}

	audit := layout.Run(doc, opts)
	block.audit = audit

	// Pixels: the viewport, or the whole page.
	if err := activateWithin(page, screenshotCaptureTimeout); err != nil {
		return nil, fmt.Errorf("could not bring the tab to the front: %v", err)
	}
	capture := page.Context(ctx).Timeout(screenshotCaptureTimeout)
	raw, err := capture.Screenshot(fullPage, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	originX, originY := lvp.ScrollX, lvp.ScrollY
	cssWidth := layoutW
	if fullPage {
		originX, originY = 0, 0
		cssWidth = math.Max(lvp.DocWidth, lvp.Width)
	}
	scale := float64(img.Bounds().Dx()) / math.Max(cssWidth, 1)
	shot := layout.NewShot(img, originX, originY, scale)
	audit.ApplyCV(shot)
	audit.Finish()
	t.attachRefs(page, audit, lvp)

	annotated, factor := layout.Downscale(layout.ToRGBA(img), layoutShotMaxWidth)
	if factor == 1 {
		annotated = cloneRGBA(annotated)
	}
	block.annotated = audit.Annotate(annotated, originX, originY, scale*factor)
	var buf bytes.Buffer
	if err := png.Encode(&buf, annotated); err != nil {
		return nil, fmt.Errorf("encode annotated screenshot: %w", err)
	}
	path, err := t.saveLayoutShot(sessionID, vp, buf.Bytes())
	if err != nil {
		return nil, err
	}
	block.shotPath, block.shotW, block.shotH = path, annotated.Bounds().Dx(), annotated.Bounds().Dy()
	block.elapsed = time.Since(start)
	t.emitLayoutFacts(ctx, sessionID, audit, int(doc.Viewport.Width))
	return block, nil
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

func (t *BrowserObserveTool) saveLayoutShot(sessionID string, vp browser.Viewport, data []byte) (string, error) {
	if t.pathPolicy == nil {
		return "", fmt.Errorf("screenshot write path policy is not configured")
	}
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	name := fmt.Sprintf("layout_%s_%dx%d_%d.png", short, vp.Width, vp.Height, time.Now().UnixMilli())
	path, err := t.pathPolicy.ResolveForWrite(name, t.pathPolicy.DefaultRoot(), filepath.Base(name))
	if err != nil {
		return "", err
	}
	if err := security.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return "", fmt.Errorf("create screenshot directory: %w", err)
	}
	if err := security.WritePrivateFile(path, data); err != nil {
		return "", fmt.Errorf("write annotated screenshot: %w", err)
	}
	return path, nil
}

// attachRefs gives each control in a finding the BrowserNERD ref that
// browser-act resolves, matched by its box.
func (t *BrowserObserveTool) attachRefs(page *rod.Page, audit *layout.Audit, vp layout.Viewport) {
	boxes, err := (&ScreenshotTool{}).extractBoundingBoxes(page, false)
	if err != nil || len(boxes) == 0 {
		return
	}
	dpr := vp.Scale
	if dpr <= 0 {
		dpr = 1
	}
	for k := range audit.Findings {
		for e := range audit.Findings[k].Elements {
			el := &audit.Findings[k].Elements[e]
			for _, b := range boxes {
				if math.Abs(b.X/dpr-el.Rect.X) < 1.5 && math.Abs(b.Y/dpr-el.Rect.Y) < 1.5 &&
					math.Abs(b.Width/dpr-el.Rect.W) < 1.5 && math.Abs(b.Height/dpr-el.Rect.H) < 1.5 {
					el.Ref = b.Ref
					break
				}
			}
		}
	}
}

func (t *BrowserObserveTool) emitLayoutFacts(ctx context.Context, sessionID string, audit *layout.Audit, width int) {
	if t.engine == nil || len(audit.Findings) == 0 {
		return
	}
	now := time.Now()
	facts := make([]mangle.Fact, 0, len(audit.Findings))
	for _, f := range audit.Findings {
		sel := ""
		if len(f.Elements) > 0 {
			sel = f.Elements[0].Selector
		}
		facts = append(facts, mangle.Fact{
			Predicate: "layout_finding",
			Args:      []interface{}{sessionID, f.ID, f.Type, f.Severity, sel, width, now.UnixMilli()},
			Timestamp: now,
		})
	}
	_ = t.engine.AddFacts(ctx, facts)
}

// probeAnchors reads the authored anchoring of the candidate fixed elements
// from their matched CSS rules.
func probeAnchors(page *rod.Page, doc *layout.Document, cands []int) (map[int]layout.Anchor, error) {
	restoreDOM := page.EnableDomain(&proto.DOMEnable{})
	defer restoreDOM()
	zero := 0
	if _, err := (proto.DOMGetDocument{Depth: &zero}).Call(page); err != nil {
		return nil, fmt.Errorf("DOM.getDocument: %w", err)
	}
	restoreCSS := page.EnableDomain(&proto.CSSEnable{})
	defer restoreCSS()
	ids := make([]proto.DOMBackendNodeID, len(cands))
	for k, c := range cands {
		ids[k] = proto.DOMBackendNodeID(doc.Nodes[c].BackendID)
	}
	pushed, err := proto.DOMPushNodesByBackendIDsToFrontend{BackendNodeIDs: ids}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("DOM.pushNodesByBackendIdsToFrontend: %w", err)
	}
	out := map[int]layout.Anchor{}
	for k, nodeID := range pushed.NodeIDs {
		if k >= len(cands) || nodeID == 0 {
			continue
		}
		matched, err := proto.CSSGetMatchedStylesForNode{NodeID: nodeID}.Call(page)
		if err != nil {
			return out, fmt.Errorf("CSS.getMatchedStylesForNode: %w", err)
		}
		out[cands[k]] = anchorFromMatched(matched)
	}
	return out, nil
}

// anchorFromMatched resolves the cascade for the properties that anchor and
// size a fixed element: presentational attributes, then matched rules in
// ascending precedence, then the inline style, with !important winning.
func anchorFromMatched(m *proto.CSSGetMatchedStylesForNodeResult) layout.Anchor {
	type decl struct {
		value     string
		important bool
	}
	values := map[string]decl{}
	set := func(name, value string, important bool) {
		if cur, ok := values[name]; ok && cur.important && !important {
			return
		}
		values[name] = decl{value: strings.TrimSpace(value), important: important}
	}
	apply := func(style *proto.CSSCSSStyle) {
		if style == nil {
			return
		}
		for _, p := range style.CSSProperties {
			if p == nil || p.Disabled {
				continue
			}
			name := strings.ToLower(p.Name)
			value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.Value), "!important"))
			important := p.Important
			switch name {
			case "top", "bottom", "height", "min-height", "max-height", "inset-block-start", "inset-block-end":
				switch name {
				case "inset-block-start":
					name = "top"
				case "inset-block-end":
					name = "bottom"
				}
				set(name, value, important)
			case "inset":
				parts := strings.Fields(value)
				if len(parts) > 0 {
					set("top", parts[0], important)
					bottom := parts[0]
					if len(parts) >= 3 {
						bottom = parts[2]
					}
					set("bottom", bottom, important)
				}
			case "inset-block":
				parts := strings.Fields(value)
				if len(parts) > 0 {
					set("top", parts[0], important)
					bottom := parts[0]
					if len(parts) >= 2 {
						bottom = parts[1]
					}
					set("bottom", bottom, important)
				}
			}
		}
	}
	apply(m.AttributesStyle)
	for _, rm := range m.MatchedCSSRules {
		if rm != nil && rm.Rule != nil {
			apply(rm.Rule.Style)
		}
	}
	apply(m.InlineStyle)
	isSet := func(name string) bool {
		v := strings.ToLower(values[name].value)
		return v != "" && v != "auto" && v != "initial" && v != "unset"
	}
	return layout.Anchor{
		TopSet:    isSet("top"),
		BottomSet: isSet("bottom"),
		Height:    values["height"].value,
		MinHeight: values["min-height"].value,
		MaxHeight: values["max-height"].value,
	}
}

// layoutRow is a compact finding.
func layoutRow(f layout.Finding) map[string]interface{} {
	row := map[string]interface{}{
		"n":        f.N,
		"type":     f.Type,
		"severity": f.Severity,
		"message":  f.Message,
	}
	sels := make([]string, 0, len(f.Elements))
	for _, e := range f.Elements {
		sels = append(sels, e.Selector)
		if e.Ref != "" && row["ref"] == nil {
			row["ref"] = e.Ref
		}
	}
	row["elements"] = sels
	if f.Count > 1 {
		row["count"] = f.Count
	}
	switch f.CV {
	case layout.CVNotChecked, "":
	case layout.CVNotInView:
		row["in_view"] = false
	default:
		row["cv"] = f.CV
	}
	return row
}

func (b *layoutBlock) data(view string, maxItems int) map[string]interface{} {
	vp := b.audit.Doc.Viewport
	vpOut := map[string]interface{}{
		"width": vp.Width, "height": vp.Height, "scale": vp.Scale, "mobile": vp.Mobile, "touch": vp.Touch,
		"doc_width": vp.DocWidth, "doc_height": vp.DocHeight,
	}
	if vp.Preset != "" {
		vpOut["preset"] = vp.Preset
	}
	if vp.ToolbarHeight > 0 {
		vpOut["toolbar_height"] = vp.ToolbarHeight
	}
	if vp.LayoutWidth > 0 {
		vpOut["layout_width"] = vp.LayoutWidth
	}
	if vp.ScrollX != 0 || vp.ScrollY != 0 {
		vpOut["scroll"] = []float64{vp.ScrollX, vp.ScrollY}
	}
	out := map[string]interface{}{
		"viewport": vpOut,
		"counts":   b.audit.Counts(),
		"screenshot": map[string]interface{}{
			"file_path": b.shotPath, "width": b.shotW, "height": b.shotH, "annotated": b.annotated,
		},
	}
	if b.anchorsNote != "" {
		out["note"] = b.anchorsNote
	}
	if b.navigateError != "" {
		out["navigate_error"] = b.navigateError
	}
	switch view {
	case "summary":
	case "full":
		out["findings"] = b.audit.Findings
		out["elapsed_ms"] = b.elapsed.Milliseconds()
	default:
		rows := make([]map[string]interface{}, 0, len(b.audit.Findings))
		for _, f := range b.audit.Findings {
			if len(rows) >= maxItems {
				out["truncated"] = true
				break
			}
			rows = append(rows, layoutRow(f))
		}
		out["findings"] = rows
	}
	return out
}

func (b *layoutBlock) summary() string {
	vp := b.audit.Doc.Viewport
	name := fmt.Sprintf("%.0fx%.0f", vp.Width, vp.Height)
	if vp.Preset != "" {
		name += " " + vp.Preset
	}
	if len(b.audit.Findings) == 0 {
		return name + ": no layout findings"
	}
	counts := b.audit.Counts()
	byType := counts["by_type"].(map[string]int)
	bySev := counts["by_severity"].(map[string]int)
	total := 0
	for _, n := range byType {
		total += n
	}
	parts := make([]string, 0, len(byType))
	for _, typ := range layout.AllTypes {
		if n := byType[typ]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, typ))
		}
	}
	return fmt.Sprintf("%s: %d finding(s), %d high: %s", name, total, bySev[layout.High], strings.Join(parts, ", "))
}

func (t *BrowserObserveTool) layoutResponse(ctx context.Context, sessionID string, blocks []*layoutBlock, view string, maxItems int) map[string]interface{} {
	handles := []string{"observe:" + sessionID + ":layout"}
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
	resp := map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"mode":             "layout",
		"view":             view,
		"evidence_handles": handles,
	}
	summaries := make([]string, 0, len(blocks))
	for _, b := range blocks {
		summaries = append(summaries, b.summary())
	}
	resp["summary"] = strings.Join(summaries, "; ")
	if len(blocks) == 1 {
		resp["data"] = blocks[0].data(view, maxItems)
	} else {
		list := make([]map[string]interface{}, 0, len(blocks))
		for _, b := range blocks {
			list = append(list, b.data(view, maxItems))
		}
		data := map[string]interface{}{"viewports": list}
		if rec := recurringFindings(blocks); len(rec) > 0 {
			data["recurring"] = rec
		}
		resp["data"] = data
	}
	for _, b := range blocks {
		for _, f := range b.audit.Findings {
			if f.Severity != layout.High {
				continue
			}
			for _, e := range f.Elements {
				if e.Ref != "" {
					resp["next_step"] = map[string]interface{}{
						"tool":   "browser-observe",
						"args":   map[string]interface{}{"session_id": sessionID, "mode": "screenshot", "ref": e.Ref},
						"reason": fmt.Sprintf("see finding %d (%s) up close", f.N, f.Type),
					}
					return resp
				}
			}
		}
	}
	return resp
}

// recurringFindings names defects seen at more than one viewport width.
func recurringFindings(blocks []*layoutBlock) []map[string]interface{} {
	type key struct{ typ, sel string }
	widths := map[key][]int{}
	var order []key
	for _, b := range blocks {
		seen := map[key]bool{}
		for _, f := range b.audit.Findings {
			if len(f.Elements) == 0 {
				continue
			}
			k := key{f.Type, f.Elements[0].Selector}
			if seen[k] {
				continue
			}
			seen[k] = true
			if _, ok := widths[k]; !ok {
				order = append(order, k)
			}
			widths[k] = append(widths[k], int(b.audit.Doc.Viewport.Width))
		}
	}
	var out []map[string]interface{}
	for _, k := range order {
		if len(widths[k]) < 2 {
			continue
		}
		ws := widths[k]
		sort.Ints(ws)
		out = append(out, map[string]interface{}{"type": k.typ, "selector": k.sel, "widths": ws})
	}
	return out
}
