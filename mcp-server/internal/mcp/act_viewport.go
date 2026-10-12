package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
)

// viewportPresetEnum is the preset names the viewport operation accepts.
func viewportPresetEnum() []string { return browser.ViewportPresetNames() }

// viewportFields are the fields that size a viewport; reset takes none of them.
var viewportFields = []string{"preset", "width", "height", "device_scale_factor", "mobile", "touch", "orientation", "toolbar_height", "user_agent"}

func validateViewportOp(op map[string]interface{}) []string {
	given := 0
	for _, f := range viewportFields {
		if _, ok := op[f]; ok {
			given++
		}
	}
	reset := getBoolArg(op, "reset", false)
	switch {
	case reset && given > 0:
		return []string{"viewport reset takes no other field"}
	case !reset && given == 0:
		return []string{"viewport needs a preset, a width and height, or reset: true"}
	}
	if !reset {
		if _, err := browser.ResolveViewport(viewportRequestFromOp(op), browser.Viewport{Width: 1920, Height: 1080, DeviceScaleFactor: 1}); err != nil {
			return []string{"viewport " + err.Error()}
		}
	}
	return nil
}

// viewportRequestFromOp reads a validated viewport operation (or a viewports
// entry of a layout audit).
func viewportRequestFromOp(op map[string]interface{}) browser.ViewportRequest {
	req := browser.ViewportRequest{
		Preset:      getStringFromMap(op, "preset"),
		Orientation: getStringFromMap(op, "orientation"),
		UserAgent:   getStringFromMap(op, "user_agent"),
	}
	intPtr := func(key string) *int {
		if n, ok := numberValue(op[key]); ok {
			v := int(n)
			return &v
		}
		return nil
	}
	req.Width, req.Height, req.ToolbarHeight = intPtr("width"), intPtr("height"), intPtr("toolbar_height")
	if n, ok := numberValue(op["device_scale_factor"]); ok {
		req.DeviceScaleFactor = &n
	}
	if b, ok := op["mobile"].(bool); ok {
		req.Mobile = &b
	}
	if b, ok := op["touch"].(bool); ok {
		req.Touch = &b
	}
	return req
}

// viewportSummary is how a viewport is reported in results.
func viewportSummary(v browser.Viewport) map[string]interface{} {
	out := map[string]interface{}{
		"width":               v.Width,
		"height":              v.Height,
		"device_scale_factor": v.DeviceScaleFactor,
		"mobile":              v.Mobile,
		"touch":               v.Touch,
		"orientation":         v.Orientation,
	}
	if v.Preset != "" {
		out["preset"] = v.Preset
	}
	if v.ToolbarHeight > 0 {
		out["toolbar_height"] = v.ToolbarHeight
	}
	return out
}

// runViewportOp applies or resets a session's viewport, reads back the size
// the page now lays out at, and records the fact.
func runViewportOp(ctx context.Context, sessions *browser.SessionManager, engine *mangle.Engine, sessionID string, op map[string]interface{}) (map[string]interface{}, error) {
	if sessions == nil || sessionID == "" {
		return nil, fmt.Errorf("viewport needs a session")
	}
	var (
		v   browser.Viewport
		err error
	)
	if getBoolArg(op, "reset", false) {
		v, err = sessions.ResetViewport(ctx, sessionID)
	} else {
		v, err = browser.ResolveViewport(viewportRequestFromOp(op), sessions.SessionViewport(sessionID))
		if err == nil {
			v, err = sessions.SetViewport(ctx, sessionID, v)
		}
	}
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"success": true, "viewport": viewportSummary(v)}
	if page, ok := sessions.Page(sessionID); ok {
		if lm, lmErr := (proto.PageGetLayoutMetrics{}).Call(page.Context(ctx)); lmErr == nil && lm.CSSLayoutViewport != nil {
			result["measured"] = map[string]interface{}{"width": lm.CSSLayoutViewport.ClientWidth, "height": lm.CSSLayoutViewport.ClientHeight}
			if lm.CSSLayoutViewport.ClientWidth != v.Width {
				// A page wider than the viewport (a fixed-width layout on a
				// mobile viewport) zooms out: the audit then sees that width.
				result["note"] = fmt.Sprintf("the page lays out %d px wide in a %d px viewport", lm.CSSLayoutViewport.ClientWidth, v.Width)
			}
		}
	}
	if engine != nil {
		now := time.Now()
		_ = engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "viewport_emulated",
			Args:      []interface{}{sessionID, v.Width, v.Height, v.DeviceScaleFactor, fmt.Sprintf("%t", v.Mobile), now.UnixMilli()},
			Timestamp: now,
		}})
	}
	return result, nil
}
