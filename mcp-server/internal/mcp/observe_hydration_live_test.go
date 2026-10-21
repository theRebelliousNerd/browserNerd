package mcp

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func interactiveOn(t *testing.T, ctx context.Context, html string, settle time.Duration) map[string]interface{} {
	t.Helper()
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	session, err := manager.CreateSession(ctx, "data:text/html,"+url.PathEscape(html))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(settle)
	observe := &BrowserObserveTool{sessions: manager, engine: engine}
	res, err := observe.Execute(ctx, map[string]interface{}{"session_id": session.ID, "mode": "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := res.(map[string]interface{})["data"].(map[string]interface{})
	interactive, _ := data["interactive"].(map[string]interface{})
	return interactive
}

// TestLiveDeadControlsSpareAWidgetLibrarysOwnDOM: a code editor, map or chart
// builds DOM inside a container React rendered; those controls carry no fiber
// and work. Only a control with nothing React-rendered between it and the
// root is dead. Measured on react.dev, whose live code editor read as dead.
func TestLiveDeadControlsSpareAWidgetLibrarysOwnDOM(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page := `<html><body><section id="root">
<div id="editor-host"><button id="lib-btn">Run code</button></div>
<button id="dead-btn">Dead action</button>
</section><script>
const root = document.getElementById('root');
root['__reactContainer$t'] = {};
const host = document.getElementById('editor-host');
host['__reactFiber$t'] = {};
document.getElementById('lib-btn').addEventListener('click', () => {});
</script></body></html>`
	interactive := interactiveOn(t, ctx, page, 500*time.Millisecond)
	dead, _ := interactive["dead_controls"].(map[string]interface{})
	if dead == nil || asInt(dead["count"]) != 1 {
		t.Fatalf("want exactly the dead button, got %v", interactive["dead_controls"])
	}
	if labels := toStringSlice(dead["labels"]); len(labels) != 1 || labels[0] != "Dead action" {
		t.Fatalf("the widget library's button must not read as dead: %v", dead["labels"])
	}
	if dead["page"] != nil {
		t.Fatalf("React ran on this page; no page-wide note: %v", dead["page"])
	}
}

// TestLiveServerRenderedPageThatNeverHydratedIsNamed: Next.js markup with no
// React root or fiber anywhere, well after load, is a page whose JavaScript
// never ran - the tunnel failure from the uplift notes. Every control but a
// link is inert, and the digest says so page-wide.
func TestLiveServerRenderedPageThatNeverHydratedIsNamed(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page := `<html><body><div id="__next"><h1>Dashboard</h1><button>Buy now</button><a href="/pricing">Pricing</a></div></body></html>`
	interactive := interactiveOn(t, ctx, page, 3200*time.Millisecond)
	dead, _ := interactive["dead_controls"].(map[string]interface{})
	if dead == nil || asInt(dead["count"]) != 1 || dead["page"] == nil {
		t.Fatalf("an unhydrated server-rendered page must name its inert button page-wide, got %v", interactive["dead_controls"])
	}
}

// TestLiveDocumentRootedReactPageIsNotUnhydrated: React 19 under the Next.js App
// Router calls hydrateRoot(document), so the root marker sits on document, not on
// an element inside <body>. Such a page ran React: no control on it is dead and no
// page-wide "never attached" note is made. Measured on the Cross-Thread login page,
// whose five controls all read dead.
func TestLiveDocumentRootedReactPageIsNotUnhydrated(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page := `<html><body><main><button id="go">Sign in</button><input id="email"></main>
<script>self.__next_f = [];
document['__reactContainer$t'] = {};
document.body['__reactFiber$t'] = {};
document.getElementById('go')['__reactFiber$t'] = {};
document.getElementById('email')['__reactFiber$t'] = {};
</script></body></html>`
	interactive := interactiveOn(t, ctx, page, 3200*time.Millisecond)
	if interactive["dead_controls"] != nil {
		t.Fatalf("React hydrated this page through document; no control is dead, got %v", interactive["dead_controls"])
	}
}

// TestLiveDocumentRootedReactPageStillNamesItsDeadControl: a control with no fiber
// and no React-rendered element between it and <body> is still dead when React
// hydrated through document.
func TestLiveDocumentRootedReactPageStillNamesItsDeadControl(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page := `<html><body><main><button id="live">Live action</button><button id="dead">Dead action</button></main>
<script>self.__next_f = [];
document['__reactContainer$t'] = {};
document.body['__reactFiber$t'] = {};
document.getElementById('live')['__reactFiber$t'] = {};
</script></body></html>`
	interactive := interactiveOn(t, ctx, page, 3200*time.Millisecond)
	dead, _ := interactive["dead_controls"].(map[string]interface{})
	if dead == nil || asInt(dead["count"]) != 1 {
		t.Fatalf("want exactly the unrendered button dead, got %v", interactive["dead_controls"])
	}
	if dead["page"] != nil {
		t.Fatalf("React ran on this page; no page-wide note: %v", dead["page"])
	}
}
