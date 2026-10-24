package mcp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/layout"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod/lib/proto"
)

// ScreenshotTool captures a screenshot with bounding box overlays for interactive elements.
type ScreenshotTool struct {
	sessions   *browser.SessionManager
	engine     *mangle.Engine
	pathPolicy *security.PathPolicy
}

func (t *ScreenshotTool) Name() string { return "screenshot" }
func (t *ScreenshotTool) Description() string {
	return `Capture the page (or one element) to a file.

TOKEN COST: Low by default - returns only {file_path, format, size_bytes, width, height}.
annotate=true adds numbered boxes on the controls in view and lists them (grouped).

WHEN TO USE:
- Visual layout or styling questions structured tools cannot answer
- Verifying what a user sees after an action

WHEN NOT TO USE:
- Reading text -> browser-observe mode:text
- Finding what to click -> browser-observe mode:interactive
- Checking URL/title -> get-page-state

OPTIONS:
- element_ref: capture only this element (smallest image)
- full_page: capture the whole scrollable page (default: viewport)
- annotate: draw numbered overlays on viewport-visible controls, capped by max_items (default 30);
  the list maps each number to its ref, repeated controls grouped
- format png|jpeg, quality (jpeg), save_path (confined to writable roots)

The tab is brought to the front first, and capture is bounded: a tab that cannot
render fails with an error instead of hanging.`
}

func (t *ScreenshotTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"element_ref": map[string]interface{}{
				"type":        "string",
				"description": "Optional element ref to screenshot. If omitted, captures page.",
			},
			"full_page": map[string]interface{}{
				"type":        "boolean",
				"description": "Capture full scrollable page (default: false, viewport only)",
			},
			"annotate": map[string]interface{}{
				"type":        "boolean",
				"description": "Draw numbered boxes on viewport-visible controls and list them (default: false)",
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "annotate: max numbered controls (default 30)",
			},
			"save_path": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Force save to this path. If omitted, defaults to ./screenshots/",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"description": "Screenshot format: png or jpeg (default: png)",
				"enum":        []string{"png", "jpeg"},
			},
			"quality": map[string]interface{}{
				"type":        "integer",
				"description": "JPEG quality 1-100 (default: 90). Ignored for png.",
			},
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "annotate: filter by control kind ('all', 'buttons', 'inputs', 'links', 'selects', default 'all')",
				"enum":        []string{"all", "buttons", "inputs", "links", "selects"},
			},
		},
		"required": []string{"session_id"},
	}
}

// BoundingBox represents an element's position and size, in image pixels.
type BoundingBox struct {
	Index  int
	X      float64
	Y      float64
	Width  float64
	Height float64
	Type   string
	Label  string
	Ref    string
}

const (
	// screenshotCaptureTimeout bounds tab activation and capture. A tab that cannot
	// produce a frame fails with an error instead of holding the call open.
	screenshotCaptureTimeout = 20 * time.Second
	defaultAnnotateItems     = 30
	maxAnnotateItems         = 200
)

func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	elementRef := getStringArg(args, "element_ref")
	fullPage := getBoolArg(args, "full_page", false)
	savePath := getStringArg(args, "save_path")
	annotate := getBoolArg(args, "annotate", false)
	maxItems := getIntArg(args, "max_items", defaultAnnotateItems)
	if maxItems <= 0 {
		maxItems = defaultAnnotateItems
	}
	if maxItems > maxAnnotateItems {
		maxItems = maxAnnotateItems
	}
	format := strings.ToLower(getStringArg(args, "format"))
	if format == "" {
		format = "png"
	}
	quality := getIntArg(args, "quality", 90)
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	filter, filterErr := normalizeInteractiveFilter(getStringArg(args, "filter"))
	if filterErr != nil {
		return map[string]interface{}{"success": false, "error": filterErr.Error()}, nil
	}

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}
	if t.pathPolicy == nil {
		return map[string]interface{}{"success": false, "error": "screenshot write path policy is not configured"}, nil
	}

	// Focus emulation keeps background rendering active without raising the window.
	capture := page.Context(ctx).Timeout(screenshotCaptureTimeout)

	screenshotFormat := proto.PageCaptureScreenshotFormatPng
	if format == "jpeg" {
		screenshotFormat = proto.PageCaptureScreenshotFormatJpeg
	}

	var imgData []byte
	var err error
	if elementRef != "" {
		element, findErr := findElementByRefWithRegistry(capture, elementRef, t.sessions.Registry(sessionID))
		if findErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not found: %s", elementRef)}, nil
		}
		imgData, err = element.Screenshot(screenshotFormat, quality)
	} else {
		imgData, err = capture.Screenshot(fullPage, &proto.PageCaptureScreenshot{
			Format:  screenshotFormat,
			Quality: &quality,
		})
	}
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("screenshot failed: %v", err)}, nil
	}

	var annotated []BoundingBox
	inView := 0
	if annotate && elementRef == "" {
		boxes, extractErr := t.extractBoundingBoxes(capture, !fullPage, filter)
		if extractErr != nil {
			return map[string]interface{}{"success": false, "error": extractErr.Error()}, nil
		}
		inView = len(boxes)
		annotated = selectAnnotationBoxes(boxes, maxItems)
		imgData, err = drawBoundingBoxOverlays(imgData, annotated, format, quality)
		if err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to draw overlays: %v", err)}, nil
		}
	}

	sizeBytes := len(imgData)
	width, height := 0, 0
	if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(imgData)); cfgErr == nil {
		width, height = cfg.Width, cfg.Height
	}

	if savePath == "" {
		extension := ".png"
		if format == "jpeg" {
			extension = ".jpg"
		}
		savePath = fmt.Sprintf("screenshot_%s_%d%s", sessionID, time.Now().Unix(), extension)
	}
	savePath, err = t.pathPolicy.ResolveForWrite(savePath, t.pathPolicy.DefaultRoot(), filepath.Base(savePath))
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	if mkdirErr := security.EnsurePrivateDir(filepath.Dir(savePath)); mkdirErr != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to create directory: %v", mkdirErr)}, nil
	}
	if writeErr := security.WritePrivateFile(savePath, imgData); writeErr != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write screenshot: %v", writeErr)}, nil
	}

	result := map[string]interface{}{
		"success":    true,
		"file_path":  savePath,
		"format":     format,
		"size_bytes": sizeBytes,
		"width":      width,
		"height":     height,
	}
	if annotate && elementRef == "" {
		result["annotated"] = len(annotated)
		result["elements"] = groupAnnotationBoxes(annotated)
		if inView > len(annotated) {
			result["in_view"] = inView
			result["truncated"] = true
		}
	}

	now := time.Now()
	if t.engine != nil {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "screenshot_taken",
			Args:      []interface{}{sessionID, format, sizeBytes, now.UnixMilli(), savePath, len(annotated)},
			Timestamp: now,
		}})
	}

	return result, nil
}

// boxScript lists the visible interactive elements with their boxes in image pixels.
// It takes {viewportOnly, document, filter} as data: viewportOnly keeps only elements that
// intersect the viewport, and document offsets boxes by the scroll position for a
// full-page image. Refs and types come from elementRefScript, which
// get-interactive-elements and the resolver share, so an overlay ref names the same
// element and resolves back to it.
const boxScript = `(opts) => {` + elementRefScript + `
	const filter = opts.filter || 'all';
	const vw = window.innerWidth;
	const vh = window.innerHeight;
	const dpr = window.devicePixelRatio || 1;
	const sx = opts.document ? window.scrollX : 0;
	const sy = opts.document ? window.scrollY : 0;
	const boxes = [];
	__bnElementRefs().forEach((record) => {
		if (!__bnMatchesFilter(record, filter)) return;
		const el = record.el;
		const rect = el.getBoundingClientRect();
		const style = getComputedStyle(el);
		if (rect.width === 0 || rect.height === 0 ||
		    style.display === 'none' || style.visibility === 'hidden' ||
		    style.opacity === '0') {
			return;
		}
		if (opts.viewportOnly && (rect.bottom <= 0 || rect.right <= 0 || rect.top >= vh || rect.left >= vw)) {
			return;
		}
		const classified = __bnClassify(record);
		if (filter !== 'all') {
			const allowedButtons = new Set(['button', 'clickable', 'tab', 'menuitem', 'option', 'row']);
			const allowedInputs = new Set(['input', 'checkbox', 'radio', 'combobox']);
			const allowedLinks = new Set(['link']);
			const allowedSelects = new Set(['select', 'combobox', 'listbox', 'option']);
			if (filter === 'buttons' && !allowedButtons.has(classified.type)) return;
			if (filter === 'inputs' && !allowedInputs.has(classified.type)) return;
			if (filter === 'links' && !allowedLinks.has(classified.type)) return;
			if (filter === 'selects' && !allowedSelects.has(classified.type)) return;
		}
		boxes.push({
			x: (rect.x + sx) * dpr,
			y: (rect.y + sy) * dpr,
			width: rect.width * dpr,
			height: rect.height * dpr,
			type: classified.type,
			label: __bnAccessibleLabel(el),
			ref: record.ref
		});
	});
	return boxes;
}`

// extractBoundingBoxes returns the visible interactive elements in document order,
// numbered from 0. viewportOnly keeps only those intersecting the viewport; otherwise
// boxes are placed in full-page image coordinates. Optional filter restricts elements by kind.
func (t *ScreenshotTool) extractBoundingBoxes(page interface {
	Eval(string, ...interface{}) (*proto.RuntimeRemoteObject, error)
}, viewportOnly bool, filter ...string) ([]BoundingBox, error) {
	f := "all"
	if len(filter) > 0 && filter[0] != "" {
		f = filter[0]
	}
	result, err := page.Eval(boxScript, map[string]interface{}{
		"viewportOnly": viewportOnly,
		"document":     !viewportOnly,
		"filter":       f,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}

	var boxes []BoundingBox
	if data, ok := result.Value.Val().([]interface{}); ok {
		for i, item := range data {
			if elem, ok := item.(map[string]interface{}); ok {
				boxes = append(boxes, BoundingBox{
					Index:  i,
					X:      getFloat64FromMap(elem, "x"),
					Y:      getFloat64FromMap(elem, "y"),
					Width:  getFloat64FromMap(elem, "width"),
					Height: getFloat64FromMap(elem, "height"),
					Type:   getStringFromMap(elem, "type"),
					Label:  getStringFromMap(elem, "label"),
					Ref:    getStringFromMap(elem, "ref"),
				})
			}
		}
	}
	return boxes, nil
}

func annotationGroupKey(box BoundingBox) string {
	if box.Label == "" {
		return box.Type + "|ref|" + box.Ref
	}
	return box.Type + "|" + strings.ToLower(box.Label)
}

// selectAnnotationBoxes picks at most max boxes to number: one of every distinct control
// first, then further members of repeated groups, so a page of 400 identical buttons
// still numbers its unique controls. The picks keep document order and are renumbered
// from 0 in that order.
func selectAnnotationBoxes(boxes []BoundingBox, max int) []BoundingBox {
	if len(boxes) <= max {
		out := make([]BoundingBox, len(boxes))
		copy(out, boxes)
		for i := range out {
			out[i].Index = i
		}
		return out
	}
	picked := make([]bool, len(boxes))
	count := 0
	seen := map[string]bool{}
	for i, box := range boxes {
		if count >= max {
			break
		}
		key := annotationGroupKey(box)
		if seen[key] {
			continue
		}
		seen[key] = true
		picked[i] = true
		count++
	}
	for i := range boxes {
		if count >= max {
			break
		}
		if !picked[i] {
			picked[i] = true
			count++
		}
	}
	out := make([]BoundingBox, 0, count)
	for i, box := range boxes {
		if picked[i] {
			box.Index = len(out)
			out = append(out, box)
		}
	}
	return out
}

// groupAnnotationBoxes lists numbered boxes for the agent, one row per distinct control:
// n is its number, or the numbers of every repeated member ("12-29"), and refs names the
// other members.
func groupAnnotationBoxes(boxes []BoundingBox) []map[string]interface{} {
	type group struct {
		first   BoundingBox
		numbers []int
		refs    []string
	}
	order := make([]string, 0, len(boxes))
	groups := map[string]*group{}
	for _, box := range boxes {
		key := annotationGroupKey(box)
		g, ok := groups[key]
		if !ok {
			g = &group{first: box}
			groups[key] = g
			order = append(order, key)
		}
		g.numbers = append(g.numbers, box.Index)
		g.refs = append(g.refs, box.Ref)
	}
	rows := make([]map[string]interface{}, 0, len(order))
	for _, key := range order {
		g := groups[key]
		row := map[string]interface{}{
			"ref":  g.first.Ref,
			"type": g.first.Type,
		}
		if g.first.Label != "" {
			row["label"] = g.first.Label
		}
		if len(g.numbers) == 1 {
			row["n"] = g.numbers[0]
		} else {
			row["n"] = numberRanges(g.numbers)
			row["count"] = len(g.numbers)
			row["refs"] = refRunSummary(g.refs)
		}
		rows = append(rows, row)
	}
	return rows
}

// numberRanges writes ascending numbers compactly: [3 4 5 9] -> "3-5,9".
func numberRanges(numbers []int) string {
	parts := make([]string, 0, len(numbers))
	for i := 0; i < len(numbers); {
		j := i
		for j+1 < len(numbers) && numbers[j+1] == numbers[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d-%d", numbers[i], numbers[j]))
		} else {
			parts = append(parts, fmt.Sprintf("%d", numbers[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

// refRunSummary names a group's members after the first: a numbered run of duplicate
// refs as "first..last", anything else as the first few refs and a count of the rest.
// It matches the refs field get-interactive-elements writes for a grouped row.
func refRunSummary(members []string) string {
	if len(members) <= 1 {
		return ""
	}
	parse := func(ref string) (string, int) {
		at := strings.LastIndex(ref, "_")
		if at <= 0 || at == len(ref)-1 {
			return ref, 0
		}
		digits := ref[at+1:]
		for _, r := range digits {
			if r < '0' || r > '9' {
				return ref, 0
			}
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return ref, 0
		}
		return ref[:at], n
	}
	base, first := parse(members[0])
	consecutive := true
	for k, ref := range members {
		b, n := parse(ref)
		if b != base || n != first+k {
			consecutive = false
			break
		}
	}
	rest := members[1:]
	if consecutive && len(rest) > 2 {
		return rest[0] + ".." + rest[len(rest)-1]
	}
	shown := rest
	if len(shown) > 3 {
		shown = shown[:3]
	}
	out := strings.Join(shown, ", ")
	if len(rest) > 3 {
		out += fmt.Sprintf(", +%d more", len(rest)-3)
	}
	return out
}

// drawBoundingBoxOverlays draws colored rectangles with numbered labels
func drawBoundingBoxOverlays(imgData []byte, boxes []BoundingBox, format string, quality int) ([]byte, error) {
	var (
		img image.Image
		err error
	)
	switch format {
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(imgData))
	default:
		img, err = png.Decode(bytes.NewReader(imgData))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", strings.ToUpper(format), err)
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 100, 255, 255}
	green := color.RGBA{0, 200, 0, 255}
	orange := color.RGBA{255, 165, 0, 255}
	colors := map[string]color.RGBA{
		"button":    red,
		"clickable": red,
		"tab":       red,
		"menuitem":  red,
		"row":       red,
		"input":     blue,
		"checkbox":  blue,
		"radio":     blue,
		"link":      green,
		"select":    orange,
		"combobox":  orange,
		"listbox":   orange,
		"option":    orange,
		"other":     {150, 0, 255, 255}, // Purple
	}

	for _, box := range boxes {
		c, ok := colors[box.Type]
		if !ok {
			c = colors["other"]
		}

		x1 := int(box.X)
		y1 := int(box.Y)
		x2 := int(box.X + box.Width)
		y2 := int(box.Y + box.Height)

		// Clamp
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > bounds.Max.X {
			x2 = bounds.Max.X
		}
		if y2 > bounds.Max.Y {
			y2 = bounds.Max.Y
		}

		// Draw rectangle outline
		layout.DrawRect(rgba, x1, y1, x2, y2, c, 2)

		// Draw numbered label badge
		layout.DrawNumberBadge(rgba, x1, y1, box.Index, c)
	}

	var buf bytes.Buffer
	if format == "jpeg" {
		if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("failed to encode JPEG: %w", err)
		}
	} else if err := png.Encode(&buf, rgba); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

func getFloat64FromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}
