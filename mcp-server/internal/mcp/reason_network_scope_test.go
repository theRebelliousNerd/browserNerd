package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const nextOrigin = "http://localhost:3001"

// nextDevAPIs are the data calls the Cross-Thread dashboard makes on load.
var nextDevAPIs = []string{
	"/api/v1/cost-control/budget",
	"/api/v1/introductions/suggestions?limit=10",
	"/api/v1/dashboard/pipeline-summary",
	"/api/v1/hygiene/bruce-almighty/metrics?hours=24",
	"/api/v1/dashboard/recent-activities?limit=8",
	"/api/v1/auth/me",
	"/api/v1/notifications?unread=true",
	"/api/v1/dashboard/kpis",
	"/api/v1/opportunities?limit=5",
	"/api/v1/agents/status",
	"/api/v1/wisdom/insights",
	"/api/v1/tenants/current",
}

// nextDevAssets are the scaffolding a Next.js dev server serves for one page
// load: bundle chunks, stylesheets, fonts, a hot update and the favicon.
var nextDevAssets = []string{
	"/_next/static/chunks/webpack.js?v=1790630000000",
	"/_next/static/chunks/main-app.js?v=1790630000000",
	"/_next/static/chunks/app-pages-internals.js",
	"/_next/static/chunks/app/layout.js",
	"/_next/static/chunks/app/(dashboard)/layout.js",
	"/_next/static/chunks/app/(dashboard)/dashboard/page.js",
	"/_next/static/chunks/app/(dashboard)/dashboard/loading.js",
	"/_next/static/chunks/app/(dashboard)/error.js",
	"/_next/static/chunks/app/not-found.js",
	"/_next/static/chunks/polyfills.js",
	"/_next/static/chunks/react-refresh.js",
	"/_next/static/chunks/_app-pages-browser_src_components_dashboard_PipelineCard_tsx.js",
	"/_next/static/chunks/_app-pages-browser_src_components_dashboard_BudgetCard_tsx.js",
	"/_next/static/chunks/_app-pages-browser_node_modules_recharts_es6_index_js.js",
	"/_next/static/css/app/layout.css?v=1790630000000",
	"/_next/static/css/app/(dashboard)/layout.css?v=1790630000000",
	"/_next/static/media/e4af272ccee01ff0-s.p.woff2",
	"/_next/static/media/a34f9d1faa5f3315-s.p.woff2",
	"/__nextjs_font/geist-latin.woff2",
	"/_next/static/webpack/3f2a1b4c5d6e7f80.webpack.hot-update.json",
	"/favicon.ico",
	"/logo-mark.svg",
}

// nextDevRestartScenario is the dashboard loaded while the backend restarted
// (every data call 502), then reloaded two minutes later once it was back
// (every data call 200). Both loads sit inside the default five-minute window.
func nextDevRestartScenario(sid string) *probeFacts {
	p := newProbeFacts(sid)
	p.base = time.Now().Add(-3 * time.Minute)
	load := func(tag string, atMs int64, apiStatus int) {
		p.add("navigation_event", atMs, nextOrigin+"/dashboard", p.at(atMs).UnixMilli())
		p.add("current_url", atMs, nextOrigin+"/dashboard")
		p.request(tag+"-doc", "GET", nextOrigin+"/dashboard", atMs, 200, 40)
		for i, asset := range nextDevAssets {
			p.request(fmt.Sprintf("%s-asset-%d", tag, i), "GET", nextOrigin+asset, atMs+50+int64(i), 200, 5)
		}
		for i, api := range nextDevAPIs {
			id := fmt.Sprintf("%s-api-%d", tag, i)
			p.request(id, "GET", nextOrigin+api, atMs+300+int64(i)*3, apiStatus, 12)
			if apiStatus >= 500 {
				p.add("net_failure_body", atMs+312+int64(i)*3, id, `{"detail":"Bad Gateway"}`)
			}
		}
	}
	load("restart", 0, 502)
	load("reload", 120000, 200)
	return p
}

// After a reload the network topic answers for the page on screen: the 502s
// from the load during the backend restart are not its groups, and the call
// says how much earlier traffic it left out.
func TestReasonNetworkScopesToTheCurrentNavigation(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, nextDevRestartScenario("s-restart"))
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-restart", "topic": "network"})
	raw, _ := json.Marshal(res)
	t.Logf("network compact after reload: status=%v bytes=%d\n%s", res["status"], len(raw), raw)

	if res["status"] != "ok" {
		t.Fatalf("every call of the page on screen succeeded, so the network is ok; got %v", res["status"])
	}
	for _, g := range toMapSlice(res["groups"]) {
		if asInt(g["failures"]) != 0 {
			t.Fatalf("a failure from before the reload is not the current page's: %v", g)
		}
	}
	earlier := asMap(res["earlier"])
	if asInt(earlier["requests"]) != 1+len(nextDevAssets)+len(nextDevAPIs) || asInt(earlier["failed"]) != len(nextDevAPIs) {
		t.Fatalf("the call must say what it left out before the navigation, got %v", earlier)
	}

	wide := runReason(t, tool, map[string]interface{}{"session_id": "s-restart", "topic": "network", "since_navigation": false})
	var pipeline map[string]interface{}
	for _, g := range toMapSlice(wide["groups"]) {
		if g["url"] == "/api/v1/dashboard/pipeline-summary" {
			pipeline = g
		}
	}
	if pipeline == nil || asInt(pipeline["before_navigation"]) != 1 || pipeline["latest"] != "200" || asInt(pipeline["recovered"]) != 1 {
		t.Fatalf("across navigations a group says which calls came before the current page and that it recovered, got %v", pipeline)
	}
	if wide["status"] == "error" {
		t.Fatalf("a group whose failed URL has since succeeded is not failing now, got status %v", wide["status"])
	}
}

func TestIsAssetRequestSeparatesScaffoldingFromDataCalls(t *testing.T) {
	cases := map[string]bool{
		nextOrigin + "/_next/static/chunks/app/layout.js":                 true,
		nextOrigin + "/_next/static/media/e4af272ccee01ff0-s.p.woff2":     true,
		nextOrigin + "/_next/image?url=%2Flogo.png&w=64&q=75":             true,
		nextOrigin + "/__nextjs_font/geist-latin.woff2":                   true,
		nextOrigin + "/_next/static/webpack/3f2a.webpack.hot-update.json": true,
		nextOrigin + "/favicon.ico":                                       true,
		"http://localhost:5173/@vite/client":                              true,
		"http://localhost:5173/assets/index-abc123.js":                    true,
		nextOrigin + "/api/v1/dashboard/kpis":                             false,
		nextOrigin + "/api/v1/static/config":                              false,
		nextOrigin + "/graphql":                                           false,
		nextOrigin + "/dashboard":                                         false,
		nextOrigin + "/index.html":                                        false,
		nextOrigin + "/assets/123":                                        false,
		nextOrigin + "/_next/data/build/dashboard.json":                   false,
	}
	for raw, want := range cases {
		if got := isAssetRequest(raw); got != want {
			t.Fatalf("isAssetRequest(%q) = %v, want %v", raw, got, want)
		}
	}
}

// A dev server's page load is mostly scaffolding. The compact network view
// lists every data call before any asset, and folds healthy assets into one
// line instead of spending most of its groups on chunks and fonts.
func TestReasonNetworkListsDataCallsBeforeAssets(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, nextDevRestartScenario("s-assets"))
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-assets", "topic": "network"})
	raw, _ := json.Marshal(res)
	t.Logf("network compact: bytes=%d groups=%d more_groups=%v", len(raw), len(toMapSlice(res["groups"])), res["more_groups"])

	listed := map[string]bool{}
	for _, g := range toMapSlice(res["groups"]) {
		url := fmt.Sprint(g["url"])
		listed[url] = true
		if strings.HasPrefix(url, "/_next/") || strings.HasPrefix(url, "/__nextjs") || url == "/favicon.ico" {
			t.Fatalf("a healthy asset takes a group slot: %v", g)
		}
	}
	for _, api := range nextDevAPIs {
		key := normalizeRequestURL(nextOrigin+api, nextOrigin)
		if !listed[key] {
			t.Fatalf("data call %s is missing from the compact groups (%s)", key, raw)
		}
	}
	assets := asMap(res["assets"])
	if asInt(assets["requests"]) != len(nextDevAssets) || asInt(assets["failed"]) != 0 {
		t.Fatalf("healthy assets fold into one summary, got %v", assets)
	}
	if len(raw) > 3200 {
		t.Fatalf("the compact network view costs %d bytes, budget 3200", len(raw))
	}

	summary := runReason(t, tool, map[string]interface{}{"session_id": "s-assets", "topic": "network", "view": "summary"})
	for _, line := range toStringSlice(summary["top"]) {
		if strings.Contains(line, "/_next/") {
			t.Fatalf("a summary line on a chunk says nothing: %v", summary["top"])
		}
	}
}
