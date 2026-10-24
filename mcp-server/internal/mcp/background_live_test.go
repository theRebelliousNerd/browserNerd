package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
	"github.com/go-rod/rod/lib/proto"
)

// Run headed only on a dedicated virtual display: never on a person's desktop.
func TestLiveBackgroundOperationsPreserveDesktopFocus(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("set BROWSERNERD_BACKGROUND_HEADED=1; the test creates and owns an isolated Xvfb display")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	startOwnedBackgroundDisplay(t, ctx)
	activeWindow := func() string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "xprop", "-root", "_NET_ACTIVE_WINDOW").CombinedOutput()
		if err != nil {
			t.Fatalf("desktop focus probe: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	sentinel := exec.CommandContext(ctx, "xmessage", "-title", "BrowserNERD focus sentinel", "Human desktop sentinel")
	sentinelLog, err := os.Create(filepath.Join(t.TempDir(), "sentinel.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer sentinelLog.Close()
	sentinel.Stdout, sentinel.Stderr = sentinelLog, sentinelLog
	if err := sentinel.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() }()
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(activeWindow(), "0x") && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	before := activeWindow()
	if !strings.Contains(before, "0x") || strings.HasSuffix(before, "0x0") {
		log, _ := os.ReadFile(sentinelLog.Name())
		t.Fatalf("sentinel did not receive desktop focus: %s; %s", before, log)
	}
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := false
	manager := browser.NewSessionManager(config.BrowserConfig{
		Headless: &headless, SessionStore: filepath.Join(t.TempDir(), "sessions.json"),
		IsolatedDisplay: true,
	}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if instances := manager.ListBrowsers(); len(instances) != 1 || !instances[0].IsolatedDisplay {
		t.Fatalf("managed browser did not report display isolation: %+v", instances)
	}
	defer manager.Shutdown(context.Background())
	assertFocus := func(stage string) {
		t.Helper()
		if after := activeWindow(); after != before {
			t.Fatalf("%s changed desktop focus: before=%q after=%q", stage, before, after)
		}
	}
	assertFocus("managed headed launch")
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Background fixture</title><style>body{background:#123456;color:white;font:24px sans-serif}button,input{padding:20px}</style>
<h1>Background fixture</h1><label>Name <input id="name"></label><button id="save">Save</button><button id="hover">Hover me</button><output id="result"></output>
<script>let clicks=0,hovers=0,keys=0,clickTrusted=false,hoverTrusted=false,keyTrusted=false;
document.querySelector('#save').onclick=e=>{clicks++;clickTrusted=e.isTrusted;document.querySelector('#result').textContent='Saved'};
document.querySelector('#hover').onmouseenter=e=>{hovers++;hoverTrusted=e.isTrusted};document.querySelector('#name').onkeydown=e=>{if(e.key==='Enter'){keys++;keyTrusted=e.isTrusted}};</script>`)
	}))
	defer fixture.Close()
	session, err := manager.CreateSession(ctx, fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	assertFocus("first background target")
	page, _ := manager.Page(session.ID)
	window, err := proto.BrowserGetWindowForTarget{TargetID: page.TargetID}.Call(page)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first target window state: %s; desktop %s", window.Bounds.WindowState, before)
	stacking := func() string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "xprop", "-root", "_NET_CLIENT_LIST_STACKING").CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	initialStacking := stacking()
	t.Logf("isolated DISPLAY=%s headless=false active=%s stacking=%s", os.Getenv("DISPLAY"), before, initialStacking)
	if strings.Count(initialStacking, "0x") != 1 {
		t.Fatalf("Chrome leaked onto the outer sentinel display: %s", initialStacking)
	}
	assertBackground := func(stage string) {
		t.Helper()
		assertFocus(stage)
		bounds, err := proto.BrowserGetWindowBounds{WindowID: window.WindowID}.Call(page)
		if err != nil || bounds.Bounds.WindowState != window.Bounds.WindowState {
			t.Fatalf("%s changed background window state: %+v %v", stage, bounds, err)
		}
		if after := stacking(); after != initialStacking {
			t.Fatalf("%s changed desktop stacking: before=%q after=%q", stage, initialStacking, after)
		}
	}
	act := &BrowserActTool{sessions: manager, engine: engine}
	for _, op := range []map[string]interface{}{
		{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Name", "value": "Background Ada"}}},
		{"type": "key", "key": "Enter"},
		{"type": "click", "text": "Save"},
		{"type": "hover", "text": "Hover me"},
	} {
		res, err := act.Execute(ctx, map[string]interface{}{"session_id": session.ID, "operations": ops(op), "settle_ms": 0})
		if err != nil || asMap(res)["success"] != true {
			t.Fatalf("background %v: %v %+v", op, err, res)
		}
		assertBackground(fmt.Sprint(op["type"]))
		t.Logf("%s retained %s", op["type"], activeWindow())
	}
	state, err := page.Eval(`() => ({name:document.querySelector('#name').value,clicks,hovers,keys,focused:document.hasFocus(),trusted:clickTrusted&&hoverTrusted&&keyTrusted})`)
	if err != nil || state.Value.Get("name").Str() != "Background Ada" || state.Value.Get("clicks").Int() != 1 || state.Value.Get("hovers").Int() < 1 || state.Value.Get("keys").Int() != 1 || !state.Value.Get("focused").Bool() || !state.Value.Get("trusted").Bool() {
		t.Fatalf("background page handlers: %+v %v", state, err)
	}
	out := os.Getenv("BROWSERNERD_BACKGROUND_OUT")
	if out == "" {
		out = t.TempDir()
	}
	policy, err := security.NewPathPolicy(out, []string{out})
	if err != nil {
		t.Fatal(err)
	}
	observe := &BrowserObserveTool{sessions: manager, engine: engine, pathPolicy: policy}
	for _, mode := range []string{"screenshot", "layout"} {
		res, err := observe.Execute(ctx, map[string]interface{}{"session_id": session.ID, "mode": mode, "view": "full", "ready_timeout_ms": 1000})
		if err != nil || asMap(res)["success"] != true {
			t.Fatalf("background %s: %v %+v", mode, err, res)
		}
		t.Logf("%s: %+v", mode, res)
		assertBackground(mode)
	}
	second, err := manager.CreateSession(ctx, fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	assertBackground("second background target")
	if err := page.SetWindow(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateMinimized}); err != nil {
		t.Fatal(err)
	}
	if err := manager.FocusSession(second.ID); err != nil {
		t.Fatalf("explicit focus: %v", err)
	}
	assertBackground("explicit focus inside private display")
	if bounds, err := page.GetWindow(); err != nil || bounds.WindowState != proto.BrowserWindowStateNormal {
		t.Fatalf("explicit focus did not restore private window: %+v %v", bounds, err)
	}
	t.Log("headed fill/key/click/hover/screenshot/layout and second target preserved outer sentinel focus and stacking; explicit focus restored only the private display window")
}
