package mcp

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
)

// captureProbe is the probe page in headless Chrome with a real engine, like
// startLiveProbe, but with its own download directory.
type captureProbe struct {
	ctx         context.Context
	manager     *browser.SessionManager
	engine      *mangle.Engine
	session     string
	baseURL     string
	downloadDir string
}

func startCaptureProbe(t *testing.T) *captureProbe {
	t.Helper()
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 8192})
	if err != nil {
		t.Fatalf("mangle engine: %v", err)
	}
	headless := true
	downloadDir := t.TempDir()
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless, DownloadDir: downloadDir, EnableHeaderIngestion: true}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	srv := probeServer(t)
	session, err := manager.CreateSession(ctx, srv.URL+"/index.html")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	return &captureProbe{ctx: ctx, manager: manager, engine: engine, session: session.ID, baseURL: srv.URL, downloadDir: downloadDir}
}

// waitForFact polls until a fact of predicate satisfies match.
func waitForFact(t *testing.T, engine *mangle.Engine, predicate string, timeout time.Duration, match func(mangle.Fact) bool) mangle.Fact {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, f := range engine.FactsByPredicate(predicate) {
			if match(f) {
				return f
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s fact matched within %s; have %d %s facts: %s", predicate, timeout, len(engine.FactsByPredicate(predicate)), predicate, sampleFacts(engine.FactsByPredicate(predicate), 6))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func sampleFacts(facts []mangle.Fact, n int) string {
	parts := make([]string, 0, n)
	for i, f := range facts {
		if i >= n {
			break
		}
		parts = append(parts, fmt.Sprintf("%v", f.Args))
	}
	return strings.Join(parts, " | ")
}

func argContains(i int, want string) func(mangle.Fact) bool {
	return func(f mangle.Fact) bool {
		return len(f.Args) > i && strings.Contains(fmt.Sprintf("%v", f.Args[i]), want)
	}
}

// The engine must see the page load itself: the document request, the
// load-time 404 and slow fetch, the console warning of the first script, and
// the WebSocket that fails during load. These are the calls whose failures
// leave a page empty, and the event stream used to attach after them.
func TestLiveCaptureFromFirstByte(t *testing.T) {
	p := startCaptureProbe(t)

	doc := waitForFact(t, p.engine, "net_request", 5*time.Second, argContains(3, "/index.html"))
	t.Logf("document request: %v", doc.Args)
	missing := waitForFact(t, p.engine, "net_http_error", 5*time.Second, argContains(2, "/api/missing"))
	if fmt.Sprintf("%v", missing.Args[3]) != "404" || fmt.Sprintf("%v", missing.Args[4]) != "Fetch" {
		t.Fatalf("expected a 404 Fetch error, got %v", missing.Args)
	}
	t.Logf("load-time 404: %v", missing.Args)
	slow := waitForFact(t, p.engine, "net_request", 5*time.Second, argContains(3, "/api/slow"))
	t.Logf("load-time slow request: %v", slow.Args)
	slowID := slow.Args[1]
	waitForFact(t, p.engine, "net_response", 6*time.Second, func(f mangle.Fact) bool { return f.Args[1] == slowID })
	warn := waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Deprecated prop used"))
	if warn.Args[1] != "warning" {
		t.Fatalf("expected a warning, got %v", warn.Args)
	}
	t.Logf("load-time console warning: %v", warn.Args)

	created := waitForFact(t, p.engine, "ws_event", 5*time.Second, func(f mangle.Fact) bool {
		return strings.Contains(fmt.Sprintf("%v", f.Args[2]), "ws://127.0.0.1:1/hmr") && f.Args[3] == "created"
	})
	failed := waitForFact(t, p.engine, "ws_event", 5*time.Second, func(f mangle.Fact) bool {
		return f.Args[1] == created.Args[1] && (f.Args[3] == "error" || f.Args[3] == "closed")
	})
	t.Logf("websocket: %v then %v", created.Args, failed.Args)

	// http.FileServer redirects /index.html to /, so the page lives at the root.
	current := p.engine.FactsByPredicate("current_url")
	if len(current) == 0 || fmt.Sprintf("%v", current[len(current)-1].Args[1]) != p.baseURL+"/" {
		t.Fatalf("current_url must be the probe page, got %s", sampleFacts(current, 4))
	}
}

// A confirm() must be answered the moment it opens: by the policy (dismiss)
// when nothing is armed, by the armed answer when one is.
func TestLiveDialogsNeverBlock(t *testing.T) {
	p := startCaptureProbe(t)
	page, ok := p.manager.Page(p.session)
	if !ok {
		t.Fatal("no page")
	}
	waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Deprecated prop used"))
	status := func() string {
		res, err := page.Timeout(5 * time.Second).Eval(`() => document.getElementById('status').textContent`)
		if err != nil {
			t.Fatalf("read status: %v", err)
		}
		return res.Value.String()
	}
	click := func() time.Duration {
		start := time.Now()
		el, err := page.Timeout(5 * time.Second).Element("#del")
		if err != nil {
			t.Fatalf("find #del: %v", err)
		}
		if err := el.Timeout(10*time.Second).Click(proto.InputMouseButtonLeft, 1); err != nil {
			t.Fatalf("click #del: %v", err)
		}
		return time.Since(start)
	}

	since := time.Now()
	elapsed := click()
	if elapsed > 5*time.Second {
		t.Fatalf("clicking a confirm() button took %s", elapsed)
	}
	if got := status(); got != "kept" {
		t.Fatalf("dismiss policy must keep, status %q", got)
	}
	dialogs := p.manager.DialogsSince(p.session, since)
	if len(dialogs) != 1 || dialogs[0].Type != "confirm" || dialogs[0].Accepted || dialogs[0].HandledBy != "policy" || dialogs[0].Message != "Delete everything?" {
		t.Fatalf("expected one dismissed confirm handled by policy, got %+v", dialogs)
	}
	t.Logf("unarmed click returned in %s, status kept, dialog %+v", elapsed, dialogs[0])

	p.manager.ArmDialog(p.session, true, "")
	since = time.Now()
	elapsed = click()
	if got := status(); got != "deleted" {
		t.Fatalf("an armed accept must delete, status %q", got)
	}
	dialogs = p.manager.DialogsSince(p.session, since)
	if len(dialogs) != 1 || !dialogs[0].Accepted || dialogs[0].HandledBy != "armed" {
		t.Fatalf("expected one accepted confirm handled by the arm, got %+v", dialogs)
	}
	t.Logf("armed click returned in %s, status deleted", elapsed)

	fact := waitForFact(t, p.engine, "js_dialog", 3*time.Second, func(f mangle.Fact) bool { return f.Args[4] == "armed" })
	t.Logf("js_dialog: %v", fact.Args)
}

// A failed save leaves its diagnosis in the response body; the engine keeps
// the first KB of it, and the toast the page shows.
func TestLiveFailureEvidence(t *testing.T) {
	p := startCaptureProbe(t)
	page, _ := p.manager.Page(p.session)
	waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Deprecated prop used"))
	el, err := page.Timeout(5 * time.Second).Element("#failbtn")
	if err != nil {
		t.Fatal(err)
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		t.Fatal(err)
	}
	httpErr := waitForFact(t, p.engine, "net_http_error", 5*time.Second, argContains(2, "/api/fail"))
	body := waitForFact(t, p.engine, "net_failure_body", 5*time.Second, func(f mangle.Fact) bool { return f.Args[1] == httpErr.Args[1] })
	if !strings.Contains(fmt.Sprintf("%v", body.Args[2]), "database unavailable") {
		t.Fatalf("failure body must carry the diagnosis, got %v", body.Args)
	}
	t.Logf("net_http_error: %v", httpErr.Args)
	t.Logf("net_failure_body: %v", body.Args)
	toast := waitForFact(t, p.engine, "error_toast", 5*time.Second, argContains(1, "Could not save"))
	t.Logf("error_toast: %v", toast.Args)
	// A toast library that reuses its live region rewrites the text node in
	// place: a characterData change, not an added element.
	if _, err := page.Timeout(5 * time.Second).Eval(`() => { document.getElementById('toast').firstChild.data = 'Retry failed: timeout'; }`); err != nil {
		t.Fatal(err)
	}
	rewritten := waitForFact(t, p.engine, "error_toast", 5*time.Second, argContains(1, "Retry failed: timeout"))
	t.Logf("rewritten error_toast: %v", rewritten.Args)
	consoleErr := waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Save failed with status 500"))
	t.Logf("console error: %v", consoleErr.Args)

	// The derived chain ties the console error and the toast to the request.
	caused, err := p.engine.Query(p.ctx, `caused_by(S, "Save failed with status 500", R).`)
	if err != nil || len(caused) != 1 || caused[0]["R"] != httpErr.Args[1] {
		t.Fatalf("caused_by must link the console error to %v: %v, %v", httpErr.Args[1], caused, err)
	}
	chain, err := p.engine.Query(p.ctx, `error_chain(S, Msg, R, Url, Status).`)
	if err != nil || len(chain) != 1 {
		t.Fatalf("error_chain: %v, %v", chain, err)
	}
	t.Logf("error_chain: %v", chain[0])
	// The load-time 404 also falls inside the toast's 5 s window; the save
	// that failed nearest the toast is its cause.
	toastLink, err := p.engine.Query(p.ctx, `toast_after_api_failure(S, "Could not save: server error", R, Url, Status, Delta).`)
	if err != nil || len(toastLink) != 1 || toastLink[0]["R"] != httpErr.Args[1] {
		t.Fatalf("toast_after_api_failure must link the toast to %v only: %v, %v", httpErr.Args[1], toastLink, err)
	}
	t.Logf("toast_after_api_failure: %v", toastLink[0])
}

// A download lands in the configured directory and becomes started and
// completed facts.
func TestLiveDownloadCapture(t *testing.T) {
	p := startCaptureProbe(t)
	page, _ := p.manager.Page(p.session)
	waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Deprecated prop used"))
	since := time.Now()
	el, err := page.Timeout(5 * time.Second).Element("#dl")
	if err != nil {
		t.Fatal(err)
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		t.Fatal(err)
	}
	started := waitForFact(t, p.engine, "download", 10*time.Second, func(f mangle.Fact) bool { return f.Args[4] == "started" })
	completed := waitForFact(t, p.engine, "download", 10*time.Second, func(f mangle.Fact) bool {
		return f.Args[1] == started.Args[1] && f.Args[4] == "completed"
	})
	t.Logf("download: %v then %v", started.Args, completed.Args)
	records := p.manager.DownloadsSince(p.session, since)
	if len(records) != 1 || records[0].SuggestedName != "report.csv" {
		t.Fatalf("expected one report.csv download record, got %+v", records)
	}
	data, err := os.ReadFile(records[0].Path)
	if err != nil || string(data) != "a,b\n1,2\n" {
		t.Fatalf("downloaded file at %s: %q, %v", records[0].Path, data, err)
	}
}

// A client-side route change moves current_url; a document that cannot load
// (the backend went away) is one page_load_failed, and the tab is left on
// Chrome's error page.
func TestLiveRouteChangeAndPageLoadFailure(t *testing.T) {
	p := startCaptureProbe(t)
	page, _ := p.manager.Page(p.session)
	waitForFact(t, p.engine, "console_event", 5*time.Second, argContains(2, "Deprecated prop used"))

	if _, err := page.Timeout(5 * time.Second).Eval(`() => history.pushState({}, '', '/spa-route')`); err != nil {
		t.Fatal(err)
	}
	route := waitForFact(t, p.engine, "navigation_event", 5*time.Second, argContains(1, "/spa-route"))
	t.Logf("same-document navigation: %v", route.Args)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadURL := "http://" + listener.Addr().String() + "/dashboard"
	_ = listener.Close()
	_ = page.Timeout(10 * time.Second).Navigate(deadURL)

	failed := waitForFact(t, p.engine, "page_load_failed", 10*time.Second, argContains(1, "/dashboard"))
	if !strings.Contains(fmt.Sprintf("%v", failed.Args[2]), "ERR_CONNECTION_REFUSED") {
		t.Fatalf("expected a refused connection, got %v", failed.Args)
	}
	t.Logf("page_load_failed: %v", failed.Args)
	loading := waitForFact(t, p.engine, "net_loading_failed", 5*time.Second, argContains(2, "ERR_CONNECTION_REFUSED"))
	t.Logf("net_loading_failed: %v", loading.Args)
	errorPage := waitForFact(t, p.engine, "current_url", 5*time.Second, argContains(1, "chrome-error://"))
	t.Logf("tab left on: %v", errorPage.Args)
	var matched []mangle.Fact
	for _, f := range p.engine.FactsByPredicate("page_load_failed") {
		if strings.Contains(fmt.Sprintf("%v", f.Args[1]), "/dashboard") {
			matched = append(matched, f)
		}
	}
	if len(matched) != 1 {
		for _, f := range matched {
			t.Logf("page_load_failed fact: %v", f.Args)
		}
		for _, f := range p.engine.FactsByPredicate("navigation_event") {
			t.Logf("navigation_event: %v", f.Args)
		}
		t.Fatalf("one failed load must be one page_load_failed, got %d", len(matched))
	}
}
