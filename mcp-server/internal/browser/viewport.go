package browser

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Viewport is the device a session's tab is emulating.
type Viewport struct {
	Preset            string  `json:"preset,omitempty"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"device_scale_factor"`
	Mobile            bool    `json:"mobile"`
	Touch             bool    `json:"touch"`
	Orientation       string  `json:"orientation"`
	// ToolbarHeight models the phone browser's toolbars (large minus small
	// viewport height, CSS px); the layout audit treats that strip at the
	// bottom as covered for fixed elements.
	ToolbarHeight int `json:"toolbar_height,omitempty"`
	// UserAgent is set only when the caller gave one; a mobile viewport
	// otherwise gets the browser's own user agent rewritten as mobile.
	UserAgent string `json:"user_agent,omitempty"`
}

// String is a compact description: "390x844@3 mobile touch (phone)".
func (v Viewport) String() string {
	s := fmt.Sprintf("%dx%d@%g", v.Width, v.Height, v.DeviceScaleFactor)
	if v.Mobile {
		s += " mobile"
	}
	if v.Touch {
		s += " touch"
	}
	if v.Preset != "" {
		s += " (" + v.Preset + ")"
	}
	return s
}

// ViewportPresets are the named devices a viewport operation accepts.
var ViewportPresets = map[string]Viewport{
	"phone":       {Preset: "phone", Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true, Touch: true, Orientation: "portrait", ToolbarHeight: 86},
	"phone-small": {Preset: "phone-small", Width: 360, Height: 800, DeviceScaleFactor: 3, Mobile: true, Touch: true, Orientation: "portrait", ToolbarHeight: 56},
	"iphone-14":   {Preset: "iphone-14", Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true, Touch: true, Orientation: "portrait", ToolbarHeight: 86},
	"pixel-7":     {Preset: "pixel-7", Width: 412, Height: 915, DeviceScaleFactor: 2.625, Mobile: true, Touch: true, Orientation: "portrait", ToolbarHeight: 56},
	"tablet":      {Preset: "tablet", Width: 768, Height: 1024, DeviceScaleFactor: 2, Mobile: true, Touch: true, Orientation: "portrait", ToolbarHeight: 50},
	"laptop":      {Preset: "laptop", Width: 1366, Height: 768, DeviceScaleFactor: 1, Orientation: "landscape"},
	"desktop":     {Preset: "desktop", Width: 1920, Height: 1080, DeviceScaleFactor: 1, Orientation: "landscape"},
}

// ViewportPresetNames lists the presets in a stable order.
func ViewportPresetNames() []string {
	names := make([]string, 0, len(ViewportPresets))
	for n := range ViewportPresets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ViewportRequest is a viewport operation's fields; nil means "not given".
type ViewportRequest struct {
	Preset            string
	Width             *int
	Height            *int
	DeviceScaleFactor *float64
	Mobile            *bool
	Touch             *bool
	Orientation       string
	ToolbarHeight     *int
	UserAgent         string
}

const (
	minViewportSide = 200
	maxViewportSide = 4096
)

// DefaultViewport is the configured desktop viewport every new tab opens at.
func (m *SessionManager) DefaultViewport() Viewport {
	w, h := m.cfg.GetViewportWidth(), m.cfg.GetViewportHeight()
	o := "landscape"
	if h > w {
		o = "portrait"
	}
	return Viewport{Width: w, Height: h, DeviceScaleFactor: 1, Orientation: o}
}

// ResolveViewport turns a request into a viewport: the preset (or, without
// one, the base viewport) with every given field applied on top.
func ResolveViewport(req ViewportRequest, base Viewport) (Viewport, error) {
	v := base
	v.Preset = ""
	if p := strings.ToLower(strings.TrimSpace(req.Preset)); p != "" {
		preset, ok := ViewportPresets[p]
		if !ok {
			return Viewport{}, fmt.Errorf("unknown viewport preset %q; use one of %s", req.Preset, strings.Join(ViewportPresetNames(), ", "))
		}
		v = preset
	}
	if req.Width != nil {
		v.Width = *req.Width
	}
	if req.Height != nil {
		v.Height = *req.Height
	}
	if req.DeviceScaleFactor != nil {
		v.DeviceScaleFactor = *req.DeviceScaleFactor
	}
	if req.Mobile != nil {
		v.Mobile = *req.Mobile
		if !v.Mobile && req.ToolbarHeight == nil {
			v.ToolbarHeight = 0
		}
	}
	if req.Touch != nil {
		v.Touch = *req.Touch
	}
	if req.ToolbarHeight != nil {
		v.ToolbarHeight = *req.ToolbarHeight
	}
	if req.UserAgent != "" {
		v.UserAgent = req.UserAgent
	}
	if req.Width != nil || req.Height != nil {
		v.Preset = "" // a preset resized is a custom viewport
	}
	if o := strings.ToLower(strings.TrimSpace(req.Orientation)); o != "" {
		if o != "portrait" && o != "landscape" {
			return Viewport{}, fmt.Errorf("orientation must be portrait or landscape (got %q)", req.Orientation)
		}
		if (o == "landscape") != (v.Width > v.Height) && v.Width != v.Height {
			v.Width, v.Height = v.Height, v.Width
		}
		v.Orientation = o
	} else if v.Width > v.Height {
		v.Orientation = "landscape"
	} else {
		v.Orientation = "portrait"
	}
	if v.DeviceScaleFactor <= 0 {
		v.DeviceScaleFactor = 1
	}
	if v.DeviceScaleFactor > 4 {
		return Viewport{}, fmt.Errorf("device_scale_factor must be at most 4 (got %g)", v.DeviceScaleFactor)
	}
	if v.Width < minViewportSide || v.Height < minViewportSide || v.Width > maxViewportSide || v.Height > maxViewportSide {
		return Viewport{}, fmt.Errorf("viewport %dx%d is outside %d-%d px per side", v.Width, v.Height, minViewportSide, maxViewportSide)
	}
	if v.ToolbarHeight < 0 || v.ToolbarHeight >= v.Height/2 {
		return Viewport{}, fmt.Errorf("toolbar_height must be between 0 and half the height (got %d)", v.ToolbarHeight)
	}
	return v, nil
}

var chromeVersionRE = regexp.MustCompile(`(?:Headless)?Chrome/([0-9.]+)`)

// mobileUserAgent rewrites the browser's own user agent as Chrome on Android,
// keeping its version, so a server that picks markup by user agent sees a
// phone (or, above 600 px wide, a tablet) from the real engine.
func mobileUserAgent(browserUA string, width int) string {
	version := "140.0.0.0"
	if m := chromeVersionRE.FindStringSubmatch(browserUA); len(m) == 2 {
		version = m[1]
	}
	mobile := " Mobile"
	if width >= 600 {
		mobile = ""
	}
	return fmt.Sprintf("Mozilla/5.0 (Linux; Android 14; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s%s Safari/537.36", version, mobile)
}

// browserUserAgent is the browser's own user agent, without the Headless tag
// a headless launch adds.
func browserUserAgent(page *rod.Page) string {
	ua := ""
	if page != nil && page.Browser() != nil {
		if v, err := (proto.BrowserGetVersion{}).Call(page.Browser()); err == nil {
			ua = v.UserAgent
		}
	}
	return strings.Replace(ua, "HeadlessChrome/", "Chrome/", 1)
}

// applyViewport sends the emulation for v to one tab.
func applyViewport(page *rod.Page, v Viewport, restoreUA bool) error {
	orientation := &proto.EmulationScreenOrientation{Type: proto.EmulationScreenOrientationTypePortraitPrimary, Angle: 0}
	if v.Orientation == "landscape" {
		orientation = &proto.EmulationScreenOrientation{Type: proto.EmulationScreenOrientationTypeLandscapePrimary, Angle: 90}
	}
	sw, sh := v.Width, v.Height
	if err := (proto.EmulationSetDeviceMetricsOverride{
		Width: v.Width, Height: v.Height, DeviceScaleFactor: v.DeviceScaleFactor, Mobile: v.Mobile,
		ScreenWidth: &sw, ScreenHeight: &sh, ScreenOrientation: orientation,
	}).Call(page); err != nil {
		return fmt.Errorf("set device metrics: %w", err)
	}
	points := 5
	touch := proto.EmulationSetTouchEmulationEnabled{Enabled: v.Touch}
	if v.Touch {
		touch.MaxTouchPoints = &points
	}
	if err := touch.Call(page); err != nil {
		return fmt.Errorf("set touch emulation: %w", err)
	}
	ua := v.UserAgent
	if ua == "" && v.Mobile {
		ua = mobileUserAgent(browserUserAgent(page), v.Width)
	}
	if ua == "" && restoreUA {
		ua = browserUserAgent(page)
	}
	if ua != "" {
		if err := (proto.EmulationSetUserAgentOverride{UserAgent: ua}).Call(page); err != nil {
			return fmt.Errorf("set user agent: %w", err)
		}
	}
	return nil
}

// SetViewport emulates v in the session's tab only, records it on the
// session, and waits for the page to lay out at the new size.
func (m *SessionManager) SetViewport(ctx context.Context, sessionID string, v Viewport) (Viewport, error) {
	page, ok := m.Page(sessionID)
	if !ok {
		return Viewport{}, fmt.Errorf("session not found: %s", sessionID)
	}
	m.mu.RLock()
	prev := m.sessions[sessionID].meta.Viewport
	m.mu.RUnlock()
	restoreUA := prev != nil && (prev.Mobile || prev.UserAgent != "")
	if err := applyViewport(page.Context(ctx), v, restoreUA); err != nil {
		return Viewport{}, err
	}
	m.UpdateMetadata(sessionID, func(s Session) Session {
		vv := v
		s.Viewport = &vv
		return s
	})
	SettleLayout(ctx, page)
	_ = m.persistSessions()
	return v, nil
}

// ResetViewport returns the session's tab to the configured default viewport
// and the browser's own user agent.
func (m *SessionManager) ResetViewport(ctx context.Context, sessionID string) (Viewport, error) {
	page, ok := m.Page(sessionID)
	if !ok {
		return Viewport{}, fmt.Errorf("session not found: %s", sessionID)
	}
	v := m.DefaultViewport()
	if err := applyViewport(page.Context(ctx), v, true); err != nil {
		return Viewport{}, err
	}
	m.UpdateMetadata(sessionID, func(s Session) Session {
		s.Viewport = nil
		return s
	})
	SettleLayout(ctx, page)
	_ = m.persistSessions()
	return v, nil
}

// SessionViewport is the viewport a session's tab is emulating: the recorded
// one, or the configured default.
func (m *SessionManager) SessionViewport(sessionID string) Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rec, ok := m.sessions[sessionID]; ok && rec.meta.Viewport != nil {
		return *rec.meta.Viewport
	}
	return m.DefaultViewport()
}

// HasViewportOverride reports whether a viewport operation sized the session.
func (m *SessionManager) HasViewportOverride(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.sessions[sessionID]
	return ok && rec.meta.Viewport != nil
}

// settleScript resolves after two animation frames, so resize and matchMedia
// handlers have run and the page has laid out; a background tab produces no
// frames, so a timer ends the wait, and fonts must be ready either way.
const settleScript = `() => new Promise((resolve) => {
	let done = false;
	const finish = () => { if (!done) { done = true; resolve(true); } };
	const fonts = (document.fonts && document.fonts.ready) ? document.fonts.ready : Promise.resolve();
	requestAnimationFrame(() => requestAnimationFrame(() => fonts.then(finish, finish)));
	setTimeout(() => fonts.then(finish, finish), 400);
})`

// SettleLayout waits, at most two seconds, for the page to lay out.
func SettleLayout(ctx context.Context, page *rod.Page) {
	if page == nil {
		return
	}
	_, _ = page.Context(ctx).Timeout(2 * time.Second).Eval(settleScript)
}
