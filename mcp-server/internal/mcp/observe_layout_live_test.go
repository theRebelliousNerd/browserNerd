package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/layout"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// layoutHarness is headless Chrome with one session and the layout fixtures
// served over HTTP.
type layoutHarness struct {
	ctx     context.Context
	manager *browser.SessionManager
	engine  *mangle.Engine
	session string
	baseURL string
	observe *BrowserObserveTool
	act     *BrowserActTool
}

func startLayoutHarness(t *testing.T) *layoutHarness {
	t.Helper()
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatalf("mangle engine: %v", err)
	}
	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "layout"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	policy, err := security.NewPathPolicy(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &layoutHarness{
		ctx: ctx, manager: manager, engine: engine, session: session.ID, baseURL: srv.URL,
		observe: &BrowserObserveTool{sessions: manager, engine: engine, pathPolicy: policy},
		act:     &BrowserActTool{sessions: manager, engine: engine},
	}
}

func (h *layoutHarness) act1(t *testing.T, op map[string]interface{}) map[string]interface{} {
	t.Helper()
	res, err := h.act.Execute(h.ctx, map[string]interface{}{"session_id": h.session, "operations": []interface{}{op}})
	if err != nil {
		t.Fatalf("browser-act %v: %v", op, err)
	}
	m := asMap(res)
	if m["success"] != true {
		t.Fatalf("browser-act %v failed: %v", op, m["summary"])
	}
	return m
}

// audit loads a fixture at the phone preset and returns the full findings.
func (h *layoutHarness) audit(t *testing.T, page string) ([]layout.Finding, map[string]interface{}) {
	t.Helper()
	h.act1(t, map[string]interface{}{"type": "viewport", "preset": "phone"})
	h.act1(t, map[string]interface{}{"type": "navigate", "url": h.baseURL + "/" + page})
	res, err := h.observe.Execute(h.ctx, map[string]interface{}{"session_id": h.session, "mode": "layout", "view": "full"})
	if err != nil {
		t.Fatalf("layout %s: %v", page, err)
	}
	m := asMap(res)
	if m["success"] != true {
		t.Fatalf("layout %s failed: %v", page, m["error"])
	}
	data := asMap(m["data"])
	findings, _ := data["findings"].([]layout.Finding)
	return findings, m
}

// marked is an element a fixture marks: what it must be reported as, and what
// it may also be named in.
type marked struct {
	ID     string   `json:"id"`
	Expect []string `json:"expect"`
	Allow  []string `json:"allow"`
}

const markedScript = `() => Array.from(document.querySelectorAll('[data-expect],[data-allow]')).map((el, i) => {
	if (!el.id) el.id = '__marked_' + i;
	const list = (v) => (v || '').split(/\s+/).filter(Boolean);
	return { id: el.id, expect: list(el.dataset.expect), allow: list(el.dataset.allow) };
})`

// markOf resolves a selector to the id of its nearest marked ancestor-or-self,
// "" when it names no marked element, or "?" when it resolves to nothing.
const markOfScript = `(sel) => {
	let el = null;
	try { el = document.querySelector(sel.replace(/::[a-z-]+$/, '')); } catch (e) { return '?'; }
	if (!el) return '?';
	const m = el.closest('[data-expect],[data-allow]');
	return m ? m.id : '';
}`

func (h *layoutHarness) marks(t *testing.T) map[string]marked {
	t.Helper()
	page, _ := h.manager.Page(h.session)
	res, err := page.Eval(markedScript)
	if err != nil {
		t.Fatal(err)
	}
	var list []marked
	raw, _ := json.Marshal(res.Value.Val())
	_ = json.Unmarshal(raw, &list)
	out := map[string]marked{}
	for _, m := range list {
		out[m.ID] = m
	}
	return out
}

func (h *layoutHarness) markOf(t *testing.T, sel string) string {
	t.Helper()
	page, _ := h.manager.Page(h.session)
	res, err := page.Eval(markOfScript, sel)
	if err != nil {
		t.Fatalf("resolve %q: %v", sel, err)
	}
	return res.Value.Str()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// checkFixture holds a fixture to its marks: every data-expect type of every
// marked element is reported naming it, and every finding names at least one
// element marked (expect or allow) with its type.
func (h *layoutHarness) checkFixture(t *testing.T, page string) []layout.Finding {
	t.Helper()
	findings, _ := h.audit(t, page)
	marks := h.marks(t)
	got := map[string]map[string]bool{} // mark id -> types reported naming it
	for _, f := range findings {
		sels := []string{}
		for _, e := range f.Elements {
			sels = append(sels, e.Selector)
		}
		sels = append(sels, f.Members...)
		justified := false
		for _, sel := range sels {
			id := h.markOf(t, sel)
			if id == "?" {
				t.Errorf("%s: finding %d %s names selector %q, which resolves to nothing", page, f.N, f.Type, sel)
				continue
			}
			if id == "" {
				continue
			}
			if got[id] == nil {
				got[id] = map[string]bool{}
			}
			got[id][f.Type] = true
			m := marks[id]
			if contains(m.Expect, f.Type) || contains(m.Allow, f.Type) {
				justified = true
			}
		}
		if !justified {
			t.Errorf("%s: unexpected finding %d %s (%s): %s %v", page, f.N, f.Type, f.Severity, f.Message, sels)
		}
	}
	ids := make([]string, 0, len(marks))
	for id := range marks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, typ := range marks[id].Expect {
			if !got[id][typ] {
				t.Errorf("%s: missed %s on #%s", page, typ, id)
			}
		}
	}
	for _, f := range findings {
		t.Logf("%s: #%d %s %s cv=%s %s", page, f.N, f.Type, f.Severity, f.CV, f.Message)
	}
	return findings
}

func findingsOfType(fs []layout.Finding, typ string) []layout.Finding {
	var out []layout.Finding
	for _, f := range fs {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

func TestLiveLayoutFixtures(t *testing.T) {
	h := startLayoutHarness(t)

	t.Run("overlap", func(t *testing.T) {
		fs := h.checkFixture(t, "overlap.html")
		for _, f := range findingsOfType(fs, layout.TypeOverlap) {
			if f.CV == layout.CVNoInk {
				t.Errorf("overlap %q: the CV pass saw no ink where two labels collide", f.Message)
			}
		}
	})
	t.Run("bleed", func(t *testing.T) { h.checkFixture(t, "bleed.html") })
	t.Run("overflow", func(t *testing.T) { h.checkFixture(t, "overflow.html") })
	t.Run("offscreen", func(t *testing.T) { h.checkFixture(t, "offscreen.html") })
	t.Run("fixed", func(t *testing.T) { h.checkFixture(t, "fixed.html") })
	t.Run("columns", func(t *testing.T) { h.checkFixture(t, "columns.html") })
	t.Run("taps", func(t *testing.T) { h.checkFixture(t, "taps.html") })
	t.Run("occlusion", func(t *testing.T) { h.checkFixture(t, "occlusion.html") })
	t.Run("clean", func(t *testing.T) {
		fs := h.checkFixture(t, "clean.html")
		if len(fs) != 0 {
			t.Errorf("clean.html: want no findings, got %d", len(fs))
		}
	})
}

// TestLiveLayoutDesktopIsQuiet proves the phone-only checks stay off at a
// desktop size: the columns and tap-target fixtures report nothing there.
func TestLiveLayoutDesktopIsQuiet(t *testing.T) {
	h := startLayoutHarness(t)
	h.act1(t, map[string]interface{}{"type": "viewport", "preset": "desktop"})
	for _, page := range []string{"columns.html", "taps.html"} {
		h.act1(t, map[string]interface{}{"type": "navigate", "url": h.baseURL + "/" + page})
		res, err := h.observe.Execute(h.ctx, map[string]interface{}{"session_id": h.session, "mode": "layout", "view": "full"})
		if err != nil {
			t.Fatal(err)
		}
		fs, _ := asMap(asMap(res)["data"])["findings"].([]layout.Finding)
		for _, f := range fs {
			if f.Type == layout.TypeNonCollapsing || f.Type == layout.TypeTapTarget {
				t.Errorf("%s at desktop: %s %s", page, f.Type, f.Message)
			}
		}
	}
}

// TestLiveViewportOperation proves the viewport op sizes only its own tab,
// shows in sessions, and resets.
func TestLiveViewportOperation(t *testing.T) {
	h := startLayoutHarness(t)
	other, err := h.manager.CreateSession(h.ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	// A page without a meta viewport lays out 980 px wide on a phone, as a
	// real one does; the fixtures declare width=device-width.
	h.act1(t, map[string]interface{}{"type": "navigate", "url": h.baseURL + "/clean.html"})
	res := h.act1(t, map[string]interface{}{"type": "viewport", "preset": "phone"})
	results, _ := res["results"].([]map[string]interface{})
	if len(results) == 0 {
		t.Fatalf("no results: %v", res)
	}
	page, _ := h.manager.Page(h.session)
	inner, err := page.Eval(`() => [window.innerWidth, window.innerHeight, window.devicePixelRatio, matchMedia('(pointer: coarse)').matches, navigator.userAgent.includes('Mobile')]`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(inner.Value.Val())
	if string(raw) != `[390,844,3,true,true]` {
		t.Errorf("phone tab reports %s, want [390,844,3,true,true]", raw)
	}
	otherPage, _ := h.manager.Page(other.ID)
	ow, err := otherPage.Eval(`() => window.innerWidth`)
	if err != nil {
		t.Fatal(err)
	}
	if ow.Value.Int() == 390 {
		t.Errorf("the other tab was resized too")
	}
	sess, err := h.observe.Execute(h.ctx, map[string]interface{}{"mode": "sessions"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, sess), "390x844@3 mobile touch (phone)") {
		t.Errorf("sessions listing does not show the viewport: %s", mustJSON(t, sess))
	}
	h.act1(t, map[string]interface{}{"type": "viewport", "reset": true})
	back, err := page.Eval(`() => [window.innerWidth, navigator.userAgent.includes('Mobile'), matchMedia('(pointer: coarse)').matches]`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(back.Value.Val())
	if string(raw) != `[1920,false,false]` {
		t.Errorf("after reset the tab reports %s, want [1920,false,false]", raw)
	}
	// A bad field is refused before anything runs.
	bad, err := h.act.Execute(h.ctx, map[string]interface{}{"session_id": h.session, "operations": []interface{}{map[string]interface{}{"type": "viewport", "preset": "phablet"}}})
	if err != nil {
		t.Fatal(err)
	}
	if asMap(bad)["status"] != "invalid" {
		t.Errorf("an unknown preset was not refused: %s", mustJSON(t, bad))
	}
}

// TestLiveLayoutMultiViewport proves the convenience flow: one call audits a
// route at several sizes, reports each, and restores the session's size.
func TestLiveLayoutMultiViewport(t *testing.T) {
	h := startLayoutHarness(t)
	res, err := h.observe.Execute(h.ctx, map[string]interface{}{
		"session_id": h.session, "mode": "layout", "url": h.baseURL + "/columns.html",
		"viewports": []interface{}{"phone", "tablet", map[string]interface{}{"width": 1366, "height": 768}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(res)
	if m["success"] != true {
		t.Fatalf("multi-viewport audit failed: %v", m["error"])
	}
	blocks, _ := asMap(m["data"])["viewports"].([]map[string]interface{})
	if len(blocks) != 3 {
		t.Fatalf("want 3 viewport blocks, got %d: %s", len(blocks), mustJSON(t, m))
	}
	phoneFindings, _ := blocks[0]["findings"].([]map[string]interface{})
	if len(phoneFindings) == 0 {
		t.Errorf("the phone block found nothing on columns.html")
	}
	for _, b := range blocks {
		shot := asMap(b["screenshot"])
		if p, _ := shot["file_path"].(string); p == "" {
			t.Errorf("a viewport block has no screenshot")
		} else if _, err := os.Stat(p); err != nil {
			t.Errorf("screenshot %s: %v", p, err)
		}
	}
	if h.manager.HasViewportOverride(h.session) {
		t.Errorf("the session kept an override after the multi-viewport audit")
	}
	if tokens := responseTokens(t, res); tokens > 3000 {
		t.Errorf("three-viewport compact response costs ~%d tokens, budget 3000", tokens)
	}
	t.Logf("summary: %v (~%d tokens)", m["summary"], responseTokens(t, res))
}
