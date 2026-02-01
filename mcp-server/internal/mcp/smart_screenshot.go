package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
)

// SmartScreenshotTool captures a screenshot with smart return logic (base64 for small, file for large).
// It replaces the standard ScreenshotTool with better defaults and WebP support.
type SmartScreenshotTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *SmartScreenshotTool) Name() string { return "screenshot" }
func (t *SmartScreenshotTool) Description() string {
	return `Capture visual state of the page or a specific element.

IMPROVED VERSION:
- Defaults to saving to disk (avoids context bloat)
- Supports WebP format
- Explicit base64 return only when requested

WHEN TO USE:
- Visual verification of page state
- Debugging layout issues
- Capturing evidence for reports

OPTIONS:
- element_ref: Screenshot specific element (from get-interactive-elements)
- full_page: Capture entire scrollable page (default: viewport only)
- format: "png" (default), "jpeg", or "webp"
- quality: 0-100 for jpeg/webp compression (default: 90)
- save_path: Force save to specific file path. If omitted, saves to ./screenshots/
- return_base64: Force return base64 data (WARNING: can flood context). Default: false

Returns: {success, format, size_bytes, file_path?, data?}`
}

func (t *SmartScreenshotTool) InputSchema() map[string]interface{} {
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
			"quality": map[string]interface{}{
				"type":        "integer",
				"description": "Compression quality 0-100 (jpeg/webp only). Default: 90",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"description": "Image format: 'png', 'jpeg', 'webp'. Default: 'png'",
				"enum":        []string{"png", "jpeg", "webp"},
			},
			"save_path": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Force save to this path. If omitted, defaults to ./screenshots/",
			},
			"return_base64": map[string]interface{}{
				"type":        "boolean",
				"description": "Force include base64 data. Default: false",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *SmartScreenshotTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	elementRef := getStringArg(args, "element_ref")
	fullPage := getBoolArg(args, "full_page", false)
	quality := getIntArg(args, "quality", 90)
	format := getStringArg(args, "format")
	savePath := getStringArg(args, "save_path")
	returnBase64 := getBoolArg(args, "return_base64", false)

	// Backwards compatibility for old "include_data" arg if user/agent uses it
	if val, ok := args["include_data"]; ok {
		if b, ok := val.(bool); ok && b {
			returnBase64 = true
		}
	}

	if format == "" {
		format = "png"
	}

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	// Build screenshot options
	var screenshotFormat proto.PageCaptureScreenshotFormat
	switch format {
	case "jpeg":
		screenshotFormat = proto.PageCaptureScreenshotFormatJpeg
	case "webp":
		screenshotFormat = proto.PageCaptureScreenshotFormatWebp
	default:
		screenshotFormat = proto.PageCaptureScreenshotFormatPng
	}

	// Capture screenshot
	var imgData []byte
	var err error

	if elementRef != "" {
		// Specific element
		element, findErr := findElementByRef(page, elementRef)
		if findErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not found: %s", elementRef)}, nil
		}
		imgData, err = element.Screenshot(screenshotFormat, quality)
	} else if fullPage {
		// Full page
		imgData, err = page.Screenshot(true, &proto.PageCaptureScreenshot{
			Format:  screenshotFormat,
			Quality: &quality,
		})
	} else {
		// Viewport
		imgData, err = page.Screenshot(false, &proto.PageCaptureScreenshot{
			Format:  screenshotFormat,
			Quality: &quality,
		})
	}

	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("screenshot failed: %v", err)}, nil
	}

	sizeBytes := len(imgData)
	result := map[string]interface{}{
		"success":    true,
		"format":     format,
		"size_bytes": sizeBytes,
	}

	// Logic:
	// 1. If return_base64 -> Return data (no file unless save_path explicitly set?)
	//    Actually, let's keep it simple:
	//    - If return_base64 is TRUE: Return data.
	//    - If save_path is SET: Save to that path.
	//    - If save_path is EMPTY AND return_base64 is FALSE: Save to default ./screenshots/ path.
	//    This ensures we always return *something* useful (either data or a file path).

	shouldSave := savePath != ""
	if !returnBase64 && savePath == "" {
		shouldSave = true
		// Generate default path
		cwd, _ := os.Getwd()
		screenshotsDir := filepath.Join(cwd, "screenshots")
		filename := fmt.Sprintf("screenshot_%s_%d.%s", sessionID, time.Now().Unix(), format)
		savePath = filepath.Join(screenshotsDir, filename)
	}

	// Perform Save
	if shouldSave {
		dir := filepath.Dir(savePath)
		if dir != "" && dir != "." {
			if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to create directory: %v", mkdirErr)}, nil
			}
		}

		if writeErr := os.WriteFile(savePath, imgData, 0644); writeErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("failed to write screenshot: %v", writeErr)}, nil
		}

		result["file_path"] = savePath
		result["message"] = fmt.Sprintf("Screenshot saved to %s", savePath)
	}

	// Perform Return Data
	if returnBase64 {
		result["data"] = base64.StdEncoding.EncodeToString(imgData)
		if sizeBytes >= 2*1024*1024 {
			result["warning"] = "Large image data included (>=2MB). This consumes significant context tokens."
		}
	}

	// Emit Mangle fact
	now := time.Now()
	factArgs := []interface{}{sessionID, format, sizeBytes, now.UnixMilli()}
	if savePath != "" {
		factArgs = append(factArgs, savePath)
	}
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "screenshot_taken",
		Args:      factArgs,
		Timestamp: now,
	}})

	return result, nil
}
