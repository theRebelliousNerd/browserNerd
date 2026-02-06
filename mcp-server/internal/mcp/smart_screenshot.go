package mcp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
)

// ScreenshotTool captures a screenshot with bounding box overlays for interactive elements.
type ScreenshotTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *ScreenshotTool) Name() string { return "screenshot" }
func (t *ScreenshotTool) Description() string {
	return `Capture visual state with numbered interactive element overlays.

TOKEN COST: HIGH (use sparingly, prefer structured tools first)

WHEN TO USE:
- Visual debugging when structured tools show unexpected state
- Verifying layout/positioning issues
- Finding elements that structured extraction missed
- Correlating visual position with element indices

WHEN NOT TO USE (ANTI-PATTERNS):
- Checking if page loaded -> use get-page-state (10x lighter)
- Finding what to click -> use get-interactive-elements (5x lighter)
- Reading text content -> use snapshot-dom or evaluate-js
- Checking URL/title -> use get-page-state
- Routine navigation verification -> unnecessary

WORKFLOW (when screenshot IS needed):
1. get-interactive-elements -> Find element by ref/label
2. screenshot -> Only if step 1 was ambiguous
3. interact(ref) -> Use ref from step 1, not visual guessing

FEATURES:
- Numbered overlays correlate with get-interactive-elements indices
- Color coding: buttons=red, inputs=blue, links=green, selects=orange

OPTIONS:
- element_ref: Screenshot specific element only
- full_page: Capture entire scrollable page (default: viewport only)

Returns: {success, file_path, elements_highlighted, elements[]}`
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
		},
		"required": []string{"session_id"},
	}
}

// BoundingBox represents an element's position and size
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

func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	elementRef := getStringArg(args, "element_ref")
	fullPage := getBoolArg(args, "full_page", false)
	savePath := getStringArg(args, "save_path")
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

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	screenshotFormat := proto.PageCaptureScreenshotFormatPng
	if format == "jpeg" {
		screenshotFormat = proto.PageCaptureScreenshotFormatJpeg
	}

	var imgData []byte
	var err error

	if elementRef != "" {
		element, findErr := findElementByRef(page, elementRef)
		if findErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not found: %s", elementRef)}, nil
		}
		imgData, err = element.Screenshot(screenshotFormat, quality)
	} else if fullPage {
		imgData, err = page.Screenshot(true, &proto.PageCaptureScreenshot{
			Format:  screenshotFormat,
			Quality: &quality,
		})
	} else {
		imgData, err = page.Screenshot(false, &proto.PageCaptureScreenshot{
			Format:  screenshotFormat,
			Quality: &quality,
		})
	}

	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("screenshot failed: %v", err)}, nil
	}

	// Extract bounding boxes using same logic as get-interactive-elements
	boundingBoxes, extractErr := t.extractBoundingBoxes(page)
	if extractErr != nil {
		boundingBoxes = []BoundingBox{}
	}

	// Draw overlays with numbered labels
	imgData, err = drawBoundingBoxOverlays(imgData, boundingBoxes)
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to draw overlays: %v", err)}, nil
	}

	sizeBytes := len(imgData)

	if savePath == "" {
		cwd, _ := os.Getwd()
		screenshotsDir := filepath.Join(cwd, "screenshots")
		filename := fmt.Sprintf("screenshot_%s_%d.png", sessionID, time.Now().Unix())
		savePath = filepath.Join(screenshotsDir, filename)
	}

	dir := filepath.Dir(savePath)
	if dir != "" && dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to create directory: %v", mkdirErr)}, nil
		}
	}

	if writeErr := os.WriteFile(savePath, imgData, 0644); writeErr != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write screenshot: %v", writeErr)}, nil
	}

	// Build element summary for response
	elementSummary := make([]map[string]interface{}, 0, len(boundingBoxes))
	for _, box := range boundingBoxes {
		elementSummary = append(elementSummary, map[string]interface{}{
			"index": box.Index,
			"type":  box.Type,
			"ref":   box.Ref,
			"label": box.Label,
		})
	}

	result := map[string]interface{}{
		"success":              true,
		"format":               format,
		"size_bytes":           sizeBytes,
		"file_path":            savePath,
		"elements_highlighted": len(boundingBoxes),
		"elements":             elementSummary,
		"message":              fmt.Sprintf("Screenshot with %d numbered element overlays saved to %s", len(boundingBoxes), savePath),
	}

	now := time.Now()
	factArgs := []interface{}{sessionID, format, sizeBytes, now.UnixMilli(), savePath, len(boundingBoxes)}
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "screenshot_taken",
		Args:      factArgs,
		Timestamp: now,
	}})

	return result, nil
}

// extractBoundingBoxes uses the SAME selectors as get-interactive-elements for index correlation
func (t *ScreenshotTool) extractBoundingBoxes(page interface {
	Eval(string, ...interface{}) (*proto.RuntimeRemoteObject, error)
}) ([]BoundingBox, error) {
	// This JS matches get-interactive-elements exactly for consistent indexing
	js := `
	() => {
		const elements = [];
		// MUST match get-interactive-elements selectors exactly
		const selectors = {
			buttons: 'button, input[type="submit"], input[type="button"], [role="button"]',
			inputs: 'input:not([type="hidden"]):not([type="submit"]):not([type="button"]), textarea, [contenteditable="true"]',
			links: 'a[href]',
			selects: 'select, [role="combobox"], [role="listbox"]'
		};
		const selector = Object.values(selectors).join(', ');
		const seen = new Set();
		let idx = 0;

		document.querySelectorAll(selector).forEach((el) => {
			const rect = el.getBoundingClientRect();
			const style = getComputedStyle(el);

			// Skip invisible (same logic as get-interactive-elements)
			if (rect.width === 0 || rect.height === 0 ||
			    style.display === 'none' || style.visibility === 'hidden' ||
			    style.opacity === '0') {
				return;
			}

			// Generate ref (same logic as get-interactive-elements)
			const dataTestId = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || '';
			const ariaLabel = el.getAttribute('aria-label') || '';
			const elId = el.id || '';
			const elName = el.name || '';
			const tag = el.tagName.toLowerCase();
			const classes = Array.from(el.classList);

			let ref;
			if (dataTestId) {
				ref = 'testid:' + dataTestId;
			} else if (ariaLabel && ariaLabel.length < 50) {
				ref = 'aria:' + ariaLabel.replace(/[^a-zA-Z0-9_-]/g, '_').substring(0, 40);
			} else if (elId) {
				ref = elId;
			} else if (elName) {
				ref = elName;
			} else {
				const classStr = classes.slice(0, 2).join('.');
				ref = classStr ? tag + '.' + classStr : tag + '[' + idx + ']';
			}

			if (seen.has(ref)) {
				ref = ref + '_' + idx;
			}
			seen.add(ref);

			// Determine type
			const role = el.getAttribute('role');
			let type = 'other';
			if (tag === 'button' || el.type === 'submit' || el.type === 'button' || role === 'button') {
				type = 'button';
			} else if (tag === 'a') {
				type = 'link';
			} else if (tag === 'select' || role === 'combobox' || role === 'listbox') {
				type = 'select';
			} else if (tag === 'input' || tag === 'textarea' || el.contentEditable === 'true') {
				type = 'input';
			}

			// Get label
			let label = el.getAttribute('aria-label') ||
			           el.innerText?.trim()?.substring(0, 50) ||
			           el.placeholder ||
			           el.title ||
			           '';
			label = label.replace(/\s+/g, ' ').trim();
			if (label.length > 50) label = label.substring(0, 47) + '...';

			elements.push({
				index: idx,
				x: rect.x,
				y: rect.y,
				width: rect.width,
				height: rect.height,
				type: type,
				label: label,
				ref: ref
			});
			idx++;
		});

		return elements;
	}
	`

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}

	var boxes []BoundingBox
	if data, ok := result.Value.Val().([]interface{}); ok {
		for _, item := range data {
			if elem, ok := item.(map[string]interface{}); ok {
				box := BoundingBox{
					Index:  int(getFloat64FromMap(elem, "index")),
					X:      getFloat64FromMap(elem, "x"),
					Y:      getFloat64FromMap(elem, "y"),
					Width:  getFloat64FromMap(elem, "width"),
					Height: getFloat64FromMap(elem, "height"),
					Type:   getStringFromMap(elem, "type"),
					Label:  getStringFromMap(elem, "label"),
					Ref:    getStringFromMap(elem, "ref"),
				}
				boxes = append(boxes, box)
			}
		}
	}

	return boxes, nil
}

// drawBoundingBoxOverlays draws colored rectangles with numbered labels
func drawBoundingBoxOverlays(imgData []byte, boxes []BoundingBox) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG: %w", err)
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	colors := map[string]color.RGBA{
		"button": {255, 0, 0, 255},   // Red
		"input":  {0, 100, 255, 255}, // Blue
		"link":   {0, 200, 0, 255},   // Green
		"select": {255, 165, 0, 255}, // Orange
		"other":  {150, 0, 255, 255}, // Purple
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
		drawRect(rgba, x1, y1, x2, y2, c, 2)

		// Draw numbered label badge
		drawNumberBadge(rgba, x1, y1, box.Index, c)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// drawRect draws a rectangle outline
func drawRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA, thickness int) {
	bounds := img.Bounds()
	for t := 0; t < thickness; t++ {
		for x := x1; x < x2; x++ {
			if y1+t >= 0 && y1+t < bounds.Max.Y && x >= 0 && x < bounds.Max.X {
				img.SetRGBA(x, y1+t, c)
			}
			if y2-1-t >= 0 && y2-1-t < bounds.Max.Y && x >= 0 && x < bounds.Max.X {
				img.SetRGBA(x, y2-1-t, c)
			}
		}
		for y := y1; y < y2; y++ {
			if x1+t >= 0 && x1+t < bounds.Max.X && y >= 0 && y < bounds.Max.Y {
				img.SetRGBA(x1+t, y, c)
			}
			if x2-1-t >= 0 && x2-1-t < bounds.Max.X && y >= 0 && y < bounds.Max.Y {
				img.SetRGBA(x2-1-t, y, c)
			}
		}
	}
}

// drawNumberBadge draws a small badge with the element index number
func drawNumberBadge(img *image.RGBA, x, y, num int, boxColor color.RGBA) {
	bounds := img.Bounds()
	numStr := fmt.Sprintf("%d", num)

	// Badge dimensions
	charWidth := 6
	charHeight := 9
	padding := 2
	badgeWidth := len(numStr)*charWidth + padding*2
	badgeHeight := charHeight + padding*2

	// Position badge at top-left of box, offset slightly inside
	badgeX := x
	badgeY := y - badgeHeight // Above the box
	if badgeY < 0 {
		badgeY = y // Inside if no room above
	}

	// Draw badge background (same color as box)
	for by := badgeY; by < badgeY+badgeHeight && by < bounds.Max.Y; by++ {
		for bx := badgeX; bx < badgeX+badgeWidth && bx < bounds.Max.X; bx++ {
			if bx >= 0 && by >= 0 {
				img.SetRGBA(bx, by, boxColor)
			}
		}
	}

	// Draw number in white
	textX := badgeX + padding
	textY := badgeY + padding
	white := color.RGBA{255, 255, 255, 255}

	for i, ch := range numStr {
		drawDigit(img, textX+i*charWidth, textY, int(ch-'0'), white)
	}
}

// 5x7 bitmap font for digits 0-9
var digitPatterns = [10][7]uint8{
	{0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E}, // 0
	{0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E}, // 1
	{0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F}, // 2
	{0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E}, // 3
	{0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02}, // 4
	{0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E}, // 5
	{0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E}, // 6
	{0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08}, // 7
	{0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E}, // 8
	{0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C}, // 9
}

func drawDigit(img *image.RGBA, x, y, digit int, c color.RGBA) {
	if digit < 0 || digit > 9 {
		return
	}
	bounds := img.Bounds()
	pattern := digitPatterns[digit]

	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			if pattern[row]&(1<<(4-col)) != 0 {
				px := x + col
				py := y + row
				if px >= 0 && px < bounds.Max.X && py >= 0 && py < bounds.Max.Y {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
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
