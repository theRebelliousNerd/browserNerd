package mcp

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

// reasonScope bounds what one browser-reason call reads: one session, and
// events at or after sinceMs. strict drops rows that carry no timestamp once a
// bound is set; otherwise they are kept.
type reasonScope struct {
	sessionID string
	sinceMs   int64
	strict    bool
}

func (s reasonScope) admits(ts int64) bool {
	if s.sinceMs <= 0 {
		return true
	}
	if ts <= 0 {
		return !s.strict
	}
	return ts >= s.sinceMs
}

// netCall is one browser request joined with its outcome: a response status,
// a network-level failure, and the failure body snippet when one was kept.
type netCall struct {
	id         string
	method     string
	url        string
	startMs    int64
	endMs      int64
	status     int
	durationMs int64
	errorText  string
	canceled   bool
	body       string
	// resourceType is the CDP resource type of a failed response
	// (net_http_error), which decides whether a 4xx is a page asset.
	resourceType string
}

// A request can not plausibly run longer than this; a larger gap between a
// request's start and its response fact means the two clocks disagree.
const maxPlausibleRequestMs = 10 * 60 * 1000

func (c *netCall) failed() bool {
	return c.status >= 400 || (c.errorText != "" && !c.canceled)
}

// failureClass is "5xx", "4xx", "network", or "" for a call that did not fail.
func (c *netCall) failureClass() string {
	switch {
	case c.status >= 500:
		return "5xx"
	case c.status >= 400:
		return "4xx"
	case c.errorText != "" && !c.canceled:
		return "network"
	default:
		return ""
	}
}

// doneMs is when the call's outcome was known: its response or failure, else
// its start.
func (c *netCall) doneMs() int64 {
	if c.endMs > 0 {
		return c.endMs
	}
	if c.startMs > 0 && c.durationMs > 0 {
		return c.startMs + c.durationMs
	}
	return c.startMs
}

// outcome is an error status, else the network error text (a body can fail
// after a 200 arrived), else the status, else "canceled" or "pending".
func (c *netCall) outcome() string {
	switch {
	case c.status >= 400:
		return fmt.Sprintf("%d", c.status)
	case c.errorText != "" && !c.canceled:
		return c.errorText
	case c.status > 0:
		return fmt.Sprintf("%d", c.status)
	case c.canceled:
		return "canceled"
	default:
		return "pending"
	}
}

// statusKey is the bucket a call counts under in a group's statuses.
func (c *netCall) statusKey() string {
	switch {
	case c.failureClass() == "network":
		return "failed"
	case c.status > 0:
		return fmt.Sprintf("%d", c.status)
	case c.canceled:
		return "canceled"
	default:
		return "pending"
	}
}

// diagnosticFailure reports whether a failed call explains something: every
// 5xx, a 4xx that is not a page asset, and a network-level failure of anything
// but media. A 4xx is judged by its CDP resource type - the line
// failed_request and browser-act's effects draw - and by its URL only when
// the type was not recorded.
func (c *netCall) diagnosticFailure() bool {
	switch c.failureClass() {
	case "5xx":
		return !isFavicon(c.url)
	case "4xx":
		if c.resourceType != "" {
			return !assetResourceTypes[c.resourceType] && !isFavicon(c.url)
		}
		return isAPIRequest(c.url)
	case "network":
		return !isMediaAsset(c.url)
	default:
		return false
	}
}

var staticAssetExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true, ".html": true, ".htm": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true, ".avif": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".mp4": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true,
	".txt": true, ".pdf": true, ".csv": true, ".wasm": true,
}

var mediaAssetExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true, ".avif": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".mp4": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true,
}

func requestPath(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

func isFavicon(raw string) bool {
	return strings.Contains(strings.ToLower(requestPath(raw)), "favicon")
}

// isAPIRequest reports whether a URL is a data call rather than a static
// asset: anything under an api/graphql path, or any path without a static
// file extension.
func isAPIRequest(raw string) bool {
	p := strings.ToLower(requestPath(raw))
	if isFavicon(p) || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "blob:") {
		return false
	}
	if strings.Contains(p, "/api/") || strings.HasPrefix(p, "/api") || strings.Contains(p, "graphql") {
		return true
	}
	if strings.Contains(p, "/_next/static/") || strings.Contains(p, "/static/") {
		return false
	}
	return !staticAssetExtensions[path.Ext(p)]
}

func isMediaAsset(raw string) bool {
	p := strings.ToLower(requestPath(raw))
	return isFavicon(p) || mediaAssetExtensions[path.Ext(p)]
}

type consoleRecord struct {
	level   string
	message string
	ts      int64
}

type toastRecord struct {
	text   string
	level  string
	source string
	ts     int64
}

type liveRegionRecord struct {
	text  string
	role  string
	level string
	ts    int64
}

type dialogRecord struct {
	kind      string
	message   string
	accepted  bool
	handledBy string
	ts        int64
}

type wsRecord struct {
	id     string
	url    string
	event  string
	detail string
	ts     int64
}

type loadFailureRecord struct {
	url       string
	errorText string
	ts        int64
}

type navRecord struct {
	url string
	ts  int64
}

// browserEvidence is everything one session recorded inside a scope, read
// from the fact buffer so it works whether or not a predicate is declared.
// Load failures and navigations are kept unscoped: whether the tab is stuck
// is current state, not history.
type browserEvidence struct {
	scope             reasonScope
	calls             []*netCall
	console           []consoleRecord
	toasts            []toastRecord
	liveRegions       []liveRegionRecord
	dialogs           []dialogRecord
	ws                []wsRecord
	loadFailures      []loadFailureRecord
	navs              []navRecord
	currentURL        string
	origin            string
	backendRootCauses []map[string]interface{}
	blocking          []map[string]interface{}
	contradictions    []map[string]interface{}
}

func factInSession(f mangle.Fact, sessionID string) bool {
	return len(f.Args) > 0 && fmt.Sprintf("%v", f.Args[0]) == sessionID
}

func factString(f mangle.Fact, i int) string {
	if i >= len(f.Args) || f.Args[i] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", f.Args[i]))
}

func factInt64(f mangle.Fact, i int) int64 {
	if i >= len(f.Args) {
		return 0
	}
	return asInt64(f.Args[i])
}

func factBool(f mangle.Fact, i int) bool {
	if i >= len(f.Args) {
		return false
	}
	switch v := f.Args[i].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes" || s == "/true"
	default:
		return asInt64(v) != 0
	}
}

// factTime is a fact's own timestamp argument, else its buffer timestamp.
func factTime(f mangle.Fact, i int) int64 {
	if ts := factInt64(f, i); ts > 0 {
		return ts
	}
	if !f.Timestamp.IsZero() {
		return f.Timestamp.UnixMilli()
	}
	return 0
}

func plausibleDuration(startMs, endMs, argDuration int64) int64 {
	if startMs > 0 && endMs >= startMs && endMs-startMs <= maxPlausibleRequestMs {
		return endMs - startMs
	}
	if argDuration > 0 && argDuration <= maxPlausibleRequestMs {
		return argDuration
	}
	return 0
}

func collectBrowserEvidence(ctx context.Context, engine *mangle.Engine, scope reasonScope) *browserEvidence {
	ev := &browserEvidence{scope: scope}
	if engine == nil {
		return ev
	}
	sid := scope.sessionID
	byID := make(map[string]*netCall)
	order := make([]*netCall, 0, 64)
	callFor := func(id string) *netCall {
		if c, ok := byID[id]; ok {
			return c
		}
		c := &netCall{id: id}
		byID[id] = c
		order = append(order, c)
		return c
	}

	// net_request(SessionId, Id, Method, Url, InitiatorId, StartTime)
	for _, f := range engine.FactsByPredicate("net_request") {
		if !factInSession(f, sid) || len(f.Args) < 4 {
			continue
		}
		c := callFor(factString(f, 1))
		c.method = factString(f, 2)
		c.url = factString(f, 3)
		// A redirect re-sends under the same id; the first start is the call's start.
		if start := factTime(f, 5); start > 0 && (c.startMs == 0 || start < c.startMs) {
			c.startMs = start
		}
	}
	// net_response(SessionId, Id, Status, Latency, Duration)
	for _, f := range engine.FactsByPredicate("net_response") {
		if !factInSession(f, sid) || len(f.Args) < 3 {
			continue
		}
		c, ok := byID[factString(f, 1)]
		if !ok {
			continue
		}
		c.status = int(factInt64(f, 2))
		if !f.Timestamp.IsZero() {
			c.endMs = f.Timestamp.UnixMilli()
		}
		c.durationMs = plausibleDuration(c.startMs, c.endMs, factInt64(f, 4))
		if c.durationMs == 0 || c.endMs-c.startMs != c.durationMs {
			c.endMs = 0
		}
	}
	// Failures derived without their raw request (a request evicted from the
	// buffer, or a fact pushed directly) still count.
	derived := queryToRows(ctx, engine, fmt.Sprintf("failed_request_at(%q, ReqId, Url, Status, ReqTs).", sid))
	derived = append(derived, queryToRows(ctx, engine, fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", sid))...)
	for _, row := range derived {
		id := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if id == "" {
			continue
		}
		if _, ok := byID[id]; ok {
			continue
		}
		c := callFor(id)
		c.url = strings.TrimSpace(fmt.Sprintf("%v", row["Url"]))
		c.status = asInt(row["Status"])
		c.startMs = asInt64(row["ReqTs"])
	}
	// net_loading_failed(SessionId, ReqId, ErrorText, Canceled, Timestamp)
	for _, f := range engine.FactsByPredicate("net_loading_failed") {
		if !factInSession(f, sid) {
			continue
		}
		c, ok := byID[factString(f, 1)]
		if !ok {
			continue
		}
		c.errorText = factString(f, 2)
		c.canceled = factBool(f, 3)
		if ts := factTime(f, 4); ts > 0 {
			c.endMs = ts
			c.durationMs = plausibleDuration(c.startMs, ts, 0)
		}
	}
	// net_http_error(SessionId, Id, Url, Status, ResourceType, Timestamp)
	for _, f := range engine.FactsByPredicate("net_http_error") {
		if !factInSession(f, sid) {
			continue
		}
		if c, ok := byID[factString(f, 1)]; ok {
			c.resourceType = factString(f, 4)
		}
	}
	// net_failure_body(SessionId, ReqId, Snippet)
	for _, f := range engine.FactsByPredicate("net_failure_body") {
		if !factInSession(f, sid) {
			continue
		}
		if c, ok := byID[factString(f, 1)]; ok {
			c.body = redactSnippet(factString(f, 2), 400)
		}
	}
	// slow_api facts pushed without their raw request.
	for _, row := range queryToRows(ctx, engine, fmt.Sprintf("slow_api(%q, ReqId, Url, Duration).", sid)) {
		id := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if id == "" {
			continue
		}
		if _, ok := byID[id]; ok {
			continue
		}
		c := callFor(id)
		c.url = strings.TrimSpace(fmt.Sprintf("%v", row["Url"]))
		c.durationMs = asInt64(row["Duration"])
	}
	for _, c := range order {
		if scope.admits(c.startMs) {
			ev.calls = append(ev.calls, c)
		}
	}
	sort.SliceStable(ev.calls, func(i, j int) bool { return ev.calls[i].startMs < ev.calls[j].startMs })

	// console_event(SessionId, Level, Message, Timestamp)
	for _, f := range engine.FactsByPredicate("console_event") {
		if !factInSession(f, sid) {
			continue
		}
		rec := consoleRecord{level: normalizeConsoleLevel(factString(f, 1)), message: factString(f, 2), ts: factTime(f, 3)}
		if rec.message != "" && scope.admits(rec.ts) {
			ev.console = append(ev.console, rec)
		}
	}
	// toast_notification(SessionId, Text, Level, Source, Timestamp)
	for _, f := range engine.FactsByPredicate("toast_notification") {
		if !factInSession(f, sid) {
			continue
		}
		rec := toastRecord{text: factString(f, 1), level: strings.ToLower(factString(f, 2)), source: factString(f, 3), ts: factTime(f, 4)}
		if rec.text != "" && scope.admits(rec.ts) {
			ev.toasts = append(ev.toasts, rec)
		}
	}
	// live_region_notification(SessionId, Text, Role, Level, Timestamp)
	for _, f := range engine.FactsByPredicate("live_region_notification") {
		if !factInSession(f, sid) {
			continue
		}
		rec := liveRegionRecord{text: factString(f, 1), role: factString(f, 2), level: strings.ToLower(factString(f, 3)), ts: factTime(f, 4)}
		if rec.text != "" && scope.admits(rec.ts) {
			ev.liveRegions = append(ev.liveRegions, rec)
		}
	}
	// js_dialog(SessionId, Type, Message, Accepted, HandledBy, Timestamp)
	for _, f := range engine.FactsByPredicate("js_dialog") {
		if !factInSession(f, sid) {
			continue
		}
		rec := dialogRecord{kind: strings.ToLower(factString(f, 1)), message: factString(f, 2), accepted: factBool(f, 3), handledBy: strings.ToLower(factString(f, 4)), ts: factTime(f, 5)}
		if scope.admits(rec.ts) {
			ev.dialogs = append(ev.dialogs, rec)
		}
	}
	// ws_event(SessionId, WsId, Url, Event, Detail, Timestamp)
	for _, f := range engine.FactsByPredicate("ws_event") {
		if !factInSession(f, sid) {
			continue
		}
		rec := wsRecord{id: factString(f, 1), url: factString(f, 2), event: strings.ToLower(factString(f, 3)), detail: factString(f, 4), ts: factTime(f, 5)}
		if scope.admits(rec.ts) {
			ev.ws = append(ev.ws, rec)
		}
	}
	// page_load_failed(SessionId, Url, ErrorText, Timestamp)
	for _, f := range engine.FactsByPredicate("page_load_failed") {
		if factInSession(f, sid) {
			ev.loadFailures = append(ev.loadFailures, loadFailureRecord{url: factString(f, 1), errorText: factString(f, 2), ts: factTime(f, 3)})
		}
	}
	// navigation_event(SessionId, Url, Timestamp)
	for _, f := range engine.FactsByPredicate("navigation_event") {
		if factInSession(f, sid) {
			ev.navs = append(ev.navs, navRecord{url: factString(f, 1), ts: factTime(f, 2)})
		}
	}
	sort.SliceStable(ev.navs, func(i, j int) bool { return ev.navs[i].ts < ev.navs[j].ts })

	for _, f := range engine.FactsByPredicate("current_url") {
		if factInSession(f, sid) {
			ev.currentURL = factString(f, 1)
		}
	}
	if ev.currentURL == "" && len(ev.navs) > 0 {
		ev.currentURL = ev.navs[len(ev.navs)-1].url
	}
	ev.origin = originFromURL(ev.currentURL)
	if ev.origin == "" {
		for _, n := range ev.navs {
			if o := originFromURL(n.url); o != "" {
				ev.origin = o
			}
		}
	}
	if ev.origin == "" {
		ev.origin = dominantOrigin(ev.calls)
	}

	rootCauses := queryToRows(ctx, engine, fmt.Sprintf("root_cause_at(%q, ConsoleMsg, Source, Cause, Ts).", sid))
	if len(rootCauses) == 0 {
		rootCauses = queryToRows(ctx, engine, fmt.Sprintf("root_cause(%q, ConsoleMsg, Source, Cause).", sid))
	}
	ev.backendRootCauses = filterRowsSinceScoped(rootCauses, []string{"Ts", "Timestamp"}, scope.sinceMs, scope.strict)
	ev.blocking = filterRowsByField(queryToRows(ctx, engine, "interaction_blocked(SessionId, Reason)."), "SessionId", sid)
	ev.contradictions = detectContradictions(ev)
	return ev
}

// dominantOrigin is the http(s) origin most calls went to: the page's own
// origin when no navigation was recorded.
func dominantOrigin(calls []*netCall) string {
	counts := make(map[string]int)
	best, bestN := "", 0
	for _, c := range calls {
		if !strings.HasPrefix(c.url, "http://") && !strings.HasPrefix(c.url, "https://") {
			continue
		}
		o := originFromURL(c.url)
		if o == "" {
			continue
		}
		counts[o]++
		if n := counts[o]; n > bestN || (n == bestN && o < best) {
			best, bestN = o, n
		}
	}
	return best
}

func normalizeConsoleLevel(level string) string {
	switch l := strings.ToLower(strings.TrimSpace(level)); l {
	case "warn":
		return "warning"
	default:
		return l
	}
}

// displayURL shortens a URL for a one-line diagnosis: same-origin URLs lose
// their origin, others their scheme. The query string is dropped.
func displayURL(raw, origin string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if origin != "" && strings.HasPrefix(s, origin) {
		s = strings.TrimPrefix(s, origin)
		if s == "" {
			s = "/"
		}
		return s
	}
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, scheme) {
			return strings.TrimPrefix(s, scheme)
		}
	}
	return s
}

func clipText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		cut := max
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
		return s[:cut] + "..."
	}
	return s
}

// latestActionMs is when the agent last acted on the page (a click, typed
// input, key press, or form submit), or 0 when it never did.
func latestActionMs(engine *mangle.Engine, sessionID string) int64 {
	if engine == nil {
		return 0
	}
	latest := int64(0)
	for _, spec := range []struct {
		predicate string
		tsArg     int
	}{
		{"click_event", 2}, {"input_event", 3}, {"user_keypress", 2}, {"form_submitted", 2}, {"form_field_filled", 2},
	} {
		for _, f := range engine.FactsByPredicate(spec.predicate) {
			if !factInSession(f, sessionID) {
				continue
			}
			if ts := factTime(f, spec.tsArg); ts > latest {
				latest = ts
			}
		}
	}
	return latest
}

// actionScopeSlackMs widens a since-action scope slightly: the click's own
// timestamp is taken in the page, a hair after the handler starts work.
const actionScopeSlackMs = 50

func nowMs() int64 { return time.Now().UnixMilli() }
