package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

func realReactInputFixture(t *testing.T, ctx context.Context) *httptest.Server {
	t.Helper()
	modules := os.Getenv("BROWSERNERD_REACT_NODE_MODULES")
	if modules == "" {
		t.Fatal("set BROWSERNERD_REACT_NODE_MODULES to an installed node_modules containing React, Radix Tabs, ReactFlow and esbuild")
	}
	modules, err := filepath.Abs(modules)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"react", "react-dom", "@radix-ui/react-tabs", "@xyflow/react", "esbuild"} {
		raw, err := os.ReadFile(filepath.Join(modules, name, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		var pkg struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &pkg); err != nil {
			t.Fatal(err)
		}
		t.Logf("real fixture dependency %s=%s", name, pkg.Version)
	}
	dir := t.TempDir()
	entry, err := filepath.Abs("../../testdata/probe/react-input-contract.jsx")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, filepath.Join(modules, "esbuild", "bin", "esbuild"), entry,
		"--bundle", "--format=iife", "--define:process.env.NODE_ENV=\"production\"", "--outfile="+filepath.Join(dir, "fixture.js"))
	build.Env = append(os.Environ(), "NODE_PATH="+modules)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("real React fixture build: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!doctype html><title>Native input contract</title><link rel="stylesheet" href="/fixture.css"><style>body{font:16px sans-serif;margin:20px}button,input{padding:12px;margin:8px}button[data-state=active]{background:#ade}output{display:block}</style><div id="root"></div><script src="/fixture.js"></script>`), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv
}

type inputProofEvent struct {
	Type        string  `json:"type"`
	Target      string  `json:"target"`
	Control     string  `json:"control"`
	Key         string  `json:"key"`
	Value       string  `json:"value"`
	Trusted     bool    `json:"trusted"`
	Button      int     `json:"button"`
	Buttons     int     `json:"buttons"`
	Ctrl        bool    `json:"ctrl"`
	Shift       bool    `json:"shift"`
	Alt         bool    `json:"alt"`
	Meta        bool    `json:"meta"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Detail      int     `json:"detail"`
	TabAtBubble string  `json:"tabAtBubble"`
}

type reactInputReceipt struct {
	Events   []inputProofEvent                                     `json:"events"`
	Tab      string                                                `json:"tab"`
	Name     string                                                `json:"name"`
	Clicks   []string                                              `json:"clicks"`
	Drags    int                                                   `json:"drags"`
	Selected string                                                `json:"selected"`
	Focused  bool                                                  `json:"focused"`
	Rects    map[string]struct{ Left, Right, Top, Bottom float64 } `json:"rects"`
	Rendered struct {
		Tab    string   `json:"tab"`
		Name   string   `json:"name"`
		Clicks []string `json:"clicks"`
	} `json:"rendered"`
}

// This opt-in proof owns a headed browser on a private display and never
// connects to a daemon or a person's existing browser/profile.
func TestLiveBackgroundRealReactInputContracts(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("set BROWSERNERD_BACKGROUND_HEADED=1 for isolated headed React input proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	fixture := realReactInputFixture(t, ctx)
	startOwnedBackgroundDisplay(t, ctx)
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := false
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	if instances := manager.ListBrowsers(); len(instances) != 1 || !instances[0].IsolatedDisplay {
		t.Fatalf("browser is not isolated: %+v", instances)
	}
	pages := make([]*rod.Page, 2)
	sids := make([]string, 2)
	for index := range pages {
		session, err := manager.CreateSession(ctx, fixture.URL)
		if err != nil {
			t.Fatal(err)
		}
		sids[index] = session.ID
		var ok bool
		pages[index], ok = manager.Page(session.ID)
		if !ok {
			t.Fatal("created session has no page")
		}
		if _, err := pages[index].Timeout(10 * time.Second).Element("[role=tab]"); err != nil {
			t.Fatal(err)
		}
	}
	// Run independent sequences concurrently while both renderer sessions remain
	// alive. Errors return to the testing goroutine so Fatal cannot strand a peer.
	var wg sync.WaitGroup
	errors := make(chan error, len(pages))
	for index, page := range pages {
		wg.Add(1)
		go func(index int, page *rod.Page) {
			defer wg.Done()
			errors <- proveBackgroundReactInput(ctx, t, manager, engine, sids[index], page, index)
		}(index, page)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
}

func proveBackgroundReactInput(ctx context.Context, t *testing.T, manager *browser.SessionManager, engine *mangle.Engine, sid string, page *rod.Page, index int) error {
	act := &BrowserActTool{sessions: manager, engine: engine}
	run := func(operations ...map[string]interface{}) error {
		items := make([]interface{}, len(operations))
		for i, op := range operations {
			items[i] = op
		}
		res, err := act.Execute(ctx, map[string]interface{}{"session_id": sid, "operations": items, "settle_ms": 0})
		if err != nil || asMap(res)["success"] != true {
			return fmt.Errorf("tab %d action: err=%v result=%+v", index, err, res)
		}
		return nil
	}
	for modeIndex, mode := range []string{"role", "ref", "testid"} {
		if _, err := page.Eval(`() => { window.inputProof.events=[]; }`); err != nil {
			return err
		}
		if err := run(map[string]interface{}{"type": "click", "role": "tab", "name": "Alpha", "exact": true},
			map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Draft name", "value": fmt.Sprintf("Draft %d %s", index, mode)}}},
			map[string]interface{}{"type": "key", "key": "Control+Shift+ArrowLeft"},
			map[string]interface{}{"type": "key", "key": "Control+a"},
			map[string]interface{}{"type": "key", "key": "Escape"}); err != nil {
			return err
		}
		prepared, err := page.Eval(`() => ({events:window.inputProof.events.filter(event=>['keydown','keyup','beforeinput','input'].includes(event.type)),name:window.inputProof.name})`)
		if err != nil {
			return err
		}
		var preparation reactInputReceipt
		if err := json.Unmarshal([]byte(prepared.Value.JSON("", "")), &preparation); err != nil {
			return err
		}
		keys := []string{}
		inputSeen := false
		for _, event := range preparation.Events {
			if !event.Trusted {
				// Fill clears an existing value with the native value setter and
				// a synthetic empty input event before trusted CDP InsertText.
				if event.Type != "input" || event.Value != "" || modeIndex == 0 {
					return fmt.Errorf("tab %d %s unexpected untrusted preparation: %+v", index, mode, event)
				}
			}
			if event.Type == "input" && event.Trusted && event.Value == fmt.Sprintf("Draft %d %s", index, mode) {
				inputSeen = true
			}
			if event.Type == "keydown" || event.Type == "keyup" {
				keys = append(keys, event.Type+":"+event.Key)
			}
		}
		wantKeys := []string{"keydown:Control", "keydown:Shift", "keydown:ArrowLeft", "keyup:ArrowLeft", "keyup:Shift", "keyup:Control", "keydown:Control", "keydown:a", "keyup:a", "keyup:Control", "keydown:Escape", "keyup:Escape"}
		if !inputSeen || preparation.Name != fmt.Sprintf("Draft %d %s", index, mode) || !reflect.DeepEqual(keys, wantKeys) {
			return fmt.Errorf("tab %d %s preparation mismatch: %s", index, mode, prepared.Value.JSON("", ""))
		}
		t.Logf("tab=%d locator=%s trusted controlled input=%q released chord keys=%v", index, mode, preparation.Name, keys)
		// Clear only the passive ledger, preserving native pointer/keyboard state
		// and all controlled React/library state between preparation and click.
		if _, err := page.Eval(`() => { window.inputProof.events=[]; }`); err != nil {
			return err
		}
		tab := map[string]interface{}{"type": "click", "role": "tab", "name": "Role section", "exact": true}
		card := map[string]interface{}{"type": "click", "role": "button", "name": "Movable role", "exact": true}
		if mode == "ref" {
			tab = map[string]interface{}{"type": "click", "ref": "tab-ref"}
			card = map[string]interface{}{"type": "click", "ref": "card-ref"}
		}
		if mode == "testid" {
			tab = map[string]interface{}{"type": "click", "ref": "testid:section-test"}
			card = map[string]interface{}{"type": "click", "ref": "testid:card-test"}
		}
		if err := run(tab, card); err != nil {
			return err
		}
		value, err := page.Eval(`() => ({...window.inputProof,rendered:JSON.parse(document.querySelector('output').textContent),focused:document.hasFocus(),selected:document.querySelector('[role=tab][aria-selected=true]')?.dataset.inputTarget,rects:Object.fromEntries([...document.querySelectorAll('[data-input-target]')].map(el=>{const r=el.getBoundingClientRect();return[el.dataset.inputTarget,{Left:r.left,Right:r.right,Top:r.top,Bottom:r.bottom}]}))})`)
		if err != nil {
			return err
		}
		raw := value.Value.JSON("", "")
		var receipt reactInputReceipt
		if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
			return err
		}
		t.Logf("tab=%d locator=%s receipt=%s", index, mode, raw)
		if receipt.Tab != mode || receipt.Selected != "tab-"+mode || receipt.Name != fmt.Sprintf("Draft %d %s", index, mode) || !receipt.Focused || receipt.Drags != 0 || !reflect.DeepEqual(receipt.Clicks, []string{"role", "ref", "testid"}[:modeIndex+1]) {
			return fmt.Errorf("tab %d %s wrong React/Radix/ReactFlow state: %s", index, mode, raw)
		}
		if receipt.Rendered.Tab != receipt.Tab || receipt.Rendered.Name != receipt.Name || !reflect.DeepEqual(receipt.Rendered.Clicks, receipt.Clicks) {
			return fmt.Errorf("tab %d %s controlled React render disagrees with handlers: %s", index, mode, raw)
		}
		for _, control := range []string{"tab-" + mode, "card-" + mode} {
			order := []string{}
			for _, event := range receipt.Events {
				if event.Control != control {
					continue
				}
				switch event.Type {
				case "pointerdown", "mousedown", "pointerup", "mouseup", "click":
				default:
					continue
				}
				order = append(order, event.Type)
				rect := receipt.Rects[control]
				buttons := 0
				if event.Type == "pointerdown" || event.Type == "mousedown" {
					buttons = 1
				}
				if !event.Trusted || event.Button != 0 || event.Buttons != buttons || event.Ctrl || event.Shift || event.Alt || event.Meta || event.X < rect.Left || event.X > rect.Right || event.Y < rect.Top || event.Y > rect.Bottom || event.Type == "click" && event.Detail != 1 {
					return fmt.Errorf("tab %d %s invalid native event for %s: %+v; receipt=%s", index, mode, control, event, raw)
				}
				if control == "tab-"+mode && event.Type == "mousedown" && event.TabAtBubble != mode {
					return fmt.Errorf("tab %d %s Radix did not accept native unmodified left mousedown: %+v", index, mode, event)
				}
			}
			if !reflect.DeepEqual(order, []string{"pointerdown", "mousedown", "pointerup", "mouseup", "click"}) {
				return fmt.Errorf("tab %d %s event order %s=%v", index, mode, control, order)
			}
		}
	}
	// A real native drag is the positive control: a disconnected drag callback
	// must not let a zero drag count make ordinary clicks appear correct.
	point, err := page.Eval(`() => { const r=document.getElementById('card-role').getBoundingClientRect(); window.inputProof.events=[]; return {x:r.x+r.width/2,y:r.y+r.height/2}; }`)
	if err != nil {
		return err
	}
	x, y := point.Value.Get("x").Num(), point.Value.Get("y").Num()
	for _, event := range []proto.InputDispatchMouseEvent{
		{Type: proto.InputDispatchMouseEventTypeMouseMoved, X: x, Y: y},
		{Type: proto.InputDispatchMouseEventTypeMousePressed, X: x, Y: y, Button: proto.InputMouseButtonLeft, Buttons: gson.Int(1), ClickCount: 1},
		{Type: proto.InputDispatchMouseEventTypeMouseMoved, X: x + 40, Y: y + 25, Button: proto.InputMouseButtonLeft, Buttons: gson.Int(1)},
		{Type: proto.InputDispatchMouseEventTypeMouseReleased, X: x + 40, Y: y + 25, Button: proto.InputMouseButtonLeft, Buttons: gson.Int(0), ClickCount: 1},
	} {
		if err := event.Call(page.Context(ctx)); err != nil {
			return err
		}
	}
	drag, err := page.Eval(`() => ({drags:window.inputProof.drags,clicks:window.inputProof.clicks,events:window.inputProof.events})`)
	if err != nil {
		return err
	}
	var dragged reactInputReceipt
	if err := json.Unmarshal([]byte(drag.Value.JSON("", "")), &dragged); err != nil {
		return err
	}
	if dragged.Drags != 1 || !reflect.DeepEqual(dragged.Clicks, []string{"role", "ref", "testid"}) {
		return fmt.Errorf("tab %d native drag positive control failed: %s", index, drag.Value.JSON("", ""))
	}
	t.Logf("tab=%d native drag positive control: drags=%d click sequence=%v (unchanged)", index, dragged.Drags, dragged.Clicks)
	return nil
}
