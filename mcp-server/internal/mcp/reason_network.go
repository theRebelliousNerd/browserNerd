package mcp

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	pathUUIDPattern  = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	pathNumPattern   = regexp.MustCompile(`^[0-9]+$`)
	pathHexPattern   = regexp.MustCompile(`(?i)^[0-9a-f]{12,}$`)
	pathTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)
	hasDigitPattern  = regexp.MustCompile(`[0-9]`)
)

// normalizeRequestURL is the grouping key for a request URL: numeric, uuid
// and opaque-token path segments collapse to {id}, query values are dropped
// (keys kept, sorted), and a same-origin URL loses its origin.
func normalizeRequestURL(raw, origin string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme == "" && u.Path == "") {
		s := raw
		if i := strings.IndexAny(s, "?#"); i >= 0 {
			s = s[:i]
		}
		return s
	}
	segments := strings.Split(u.Path, "/")
	for i, seg := range segments {
		switch {
		case seg == "":
		case pathNumPattern.MatchString(seg), pathUUIDPattern.MatchString(seg), pathHexPattern.MatchString(seg):
			segments[i] = "{id}"
		case pathTokenPattern.MatchString(seg) && hasDigitPattern.MatchString(seg):
			segments[i] = "{id}"
		}
	}
	p := strings.Join(segments, "/")
	if p == "" {
		p = "/"
	}
	prefix := ""
	if u.Host != "" {
		o := u.Scheme + "://" + u.Host
		if o != origin {
			prefix = u.Host
			if u.Scheme == "ws" || u.Scheme == "wss" {
				prefix = o
			}
		}
	}
	out := prefix + p
	if u.RawQuery != "" {
		keys := make([]string, 0)
		for k := range u.Query() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out += "?" + strings.Join(keys, "=&") + "="
	}
	return out
}

// requestStorm is a URL requested often enough, or regularly enough, to be a
// loop: a retry storm, a reconnect loop, a render loop, an N+1 fetch.
type requestStorm struct {
	peak30s    int
	intervalMs int64
	fixed      bool
}

const (
	stormWindowMs      = 30000
	stormMinInWindow   = 10
	stormFixedMaxGapMs = 2000
	stormFixedMinCalls = 5
)

// detectStorm flags a request storm from start times: at least 10 calls in
// any 30 s, or at least 5 calls at a steady interval of 2 s or less. The
// interval is the median gap.
func detectStorm(starts []int64) *requestStorm {
	times := make([]int64, 0, len(starts))
	for _, s := range starts {
		if s > 0 {
			times = append(times, s)
		}
	}
	if len(times) < stormFixedMinCalls {
		return nil
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	peak, lo := 0, 0
	for hi := range times {
		for times[hi]-times[lo] > stormWindowMs {
			lo++
		}
		if n := hi - lo + 1; n > peak {
			peak = n
		}
	}
	gaps := make([]float64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, float64(times[i]-times[i-1]))
	}
	sorted := append([]float64(nil), gaps...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	mean, sq := 0.0, 0.0
	for _, g := range gaps {
		mean += g
	}
	mean /= float64(len(gaps))
	for _, g := range gaps {
		sq += (g - mean) * (g - mean)
	}
	fixed := mean > 0 && math.Sqrt(sq/float64(len(gaps)))/mean < 0.35
	if peak < stormMinInWindow && !(fixed && median <= stormFixedMaxGapMs) {
		return nil
	}
	return &requestStorm{peak30s: peak, intervalMs: int64(math.Round(median)), fixed: fixed}
}

// requestGroup is every call to one method and normalized URL.
type requestGroup struct {
	method        string
	url           string
	calls         []*netCall
	statuses      map[string]int
	firstMs       int64
	lastMs        int64
	maxDurationMs int64
	failures      int
	diagFailures  int
	// recovered counts diagnostic failures whose exact URL later succeeded:
	// the call works now. A failure on the same normalized URL with other
	// parameters is not recovered by it.
	recovered  int
	worstClass string
	storm      *requestStorm
	// asset is page scaffolding: every call is a bundle chunk, stylesheet,
	// font, image, dev-server hot update or the favicon.
	asset bool
}

// openFailures are the group's diagnostic failures that nothing has since
// recovered: the calls failing now.
func (g *requestGroup) openFailures() int { return g.diagFailures - g.recovered }

// assetPathMarkers are path fragments that only page scaffolding carries.
var assetPathMarkers = []string{
	"/_next/static/", "/_next/image", "/_next/webpack-hmr", "/__nextjs", "/@vite/", "/@fs/",
	"/@react-refresh", "/node_modules/", "/static/",
}

// isAssetRequest reports whether a URL is page scaffolding rather than a data
// call or the document: a static file by extension (an HTML document
// excepted), a dev server's chunk, image, font or hot-update path, or the
// favicon. Anything under an api or graphql path is a data call.
func isAssetRequest(raw string) bool {
	p := strings.ToLower(requestPath(raw))
	if strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "blob:") {
		return true
	}
	if strings.Contains(p, "/api/") || strings.HasPrefix(p, "/api") || strings.Contains(p, "graphql") {
		return false
	}
	if isFavicon(p) {
		return true
	}
	for _, marker := range assetPathMarkers {
		if strings.Contains(p, marker) {
			return true
		}
	}
	ext := path.Ext(p)
	return staticAssetExtensions[ext] && ext != ".html" && ext != ".htm"
}

// succeeded reports a call that got a non-error response.
func (c *netCall) succeeded() bool {
	return c.status > 0 && c.status < 400 && (c.errorText == "" || c.canceled)
}

func failureClassRank(class string) int {
	switch class {
	case "5xx":
		return 3
	case "network":
		return 2
	case "4xx":
		return 1
	default:
		return 0
	}
}

// groupRequests folds calls by method and normalized URL, failures first
// (worst class first), then storms, then by count.
func groupRequests(calls []*netCall, origin string) []*requestGroup {
	byKey := make(map[string]*requestGroup)
	out := make([]*requestGroup, 0)
	for _, c := range calls {
		if strings.HasPrefix(c.url, "data:") || strings.HasPrefix(c.url, "blob:") {
			continue
		}
		method := c.method
		if method == "" {
			method = "?"
		}
		norm := normalizeRequestURL(c.url, origin)
		key := method + " " + norm
		g, ok := byKey[key]
		if !ok {
			g = &requestGroup{method: method, url: norm, statuses: make(map[string]int), asset: true}
			byKey[key] = g
			out = append(out, g)
		}
		g.asset = g.asset && isAssetRequest(c.url)
		g.calls = append(g.calls, c)
		g.statuses[c.statusKey()]++
		if c.startMs > 0 && (g.firstMs == 0 || c.startMs < g.firstMs) {
			g.firstMs = c.startMs
		}
		if c.startMs > g.lastMs {
			g.lastMs = c.startMs
		}
		if c.durationMs > g.maxDurationMs {
			g.maxDurationMs = c.durationMs
		}
		if c.failed() {
			g.failures++
		}
		if c.diagnosticFailure() {
			g.diagFailures++
			if failureClassRank(c.failureClass()) > failureClassRank(g.worstClass) {
				g.worstClass = c.failureClass()
			}
		}
	}
	for _, g := range out {
		starts := make([]int64, 0, len(g.calls))
		for _, c := range g.calls {
			starts = append(starts, c.startMs)
		}
		g.storm = detectStorm(starts)
		g.recovered = recoveredFailures(g.calls)
	}
	// Open failures first (worst class first), then storms, then data calls
	// before scaffolding, then recovered failures among their peers, then by
	// count: a dev server's page load is mostly chunks and fonts, and a list
	// ordered by count alone spent its rows on them.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := openFailureRank(out[i]), openFailureRank(out[j])
		if ri != rj {
			return ri > rj
		}
		if (out[i].storm != nil) != (out[j].storm != nil) {
			return out[i].storm != nil
		}
		if out[i].asset != out[j].asset {
			return !out[i].asset
		}
		if (out[i].recovered > 0) != (out[j].recovered > 0) {
			return out[i].recovered > 0
		}
		if len(out[i].calls) != len(out[j].calls) {
			return len(out[i].calls) > len(out[j].calls)
		}
		return out[i].url < out[j].url
	})
	return out
}

// openFailureRank is the group's worst failure class while it still has an
// open failure, and 0 once every failure has recovered.
func openFailureRank(g *requestGroup) int {
	if g.openFailures() <= 0 {
		return 0
	}
	return failureClassRank(g.worstClass)
}

// recoveredFailures counts the diagnostic failures in calls whose exact URL a
// later call answered without an error.
func recoveredFailures(calls []*netCall) int {
	lastSuccess := make(map[string]int64)
	for _, c := range calls {
		if c.succeeded() && c.startMs > lastSuccess[c.url] {
			lastSuccess[c.url] = c.startMs
		}
	}
	recovered := 0
	for _, c := range calls {
		if c.diagnosticFailure() && lastSuccess[c.url] > c.startMs {
			recovered++
		}
	}
	return recovered
}

// latestCall is the group's most recent call with an outcome, else its most
// recent call.
func (g *requestGroup) latestCall() *netCall {
	var latest, latestDone *netCall
	for _, c := range g.calls {
		if latest == nil || c.startMs >= latest.startMs {
			latest = c
		}
		if (c.status > 0 || c.errorText != "") && (latestDone == nil || c.startMs >= latestDone.startMs) {
			latestDone = c
		}
	}
	if latestDone != nil {
		return latestDone
	}
	return latest
}

func (g *requestGroup) latestFailure() *netCall {
	for i := len(g.calls) - 1; i >= 0; i-- {
		if g.calls[i].diagnosticFailure() {
			return g.calls[i]
		}
	}
	return nil
}

func (g *requestGroup) row() map[string]interface{} {
	row := map[string]interface{}{
		"method":   g.method,
		"url":      g.url,
		"count":    len(g.calls),
		"statuses": g.statuses,
		"first_ms": g.firstMs,
		"last_ms":  g.lastMs,
		"failures": g.failures,
	}
	if g.maxDurationMs > 0 {
		row["max_duration_ms"] = g.maxDurationMs
	}
	if g.storm != nil {
		row["storm"] = map[string]interface{}{"in_30s": g.storm.peak30s, "interval_ms": g.storm.intervalMs}
	}
	if f := g.latestFailure(); f != nil {
		if detail := bodyDetail(f.body); detail != "" {
			row["detail"] = detail
		} else if f.errorText != "" {
			row["detail"] = f.errorText
		}
		// A group with a failure says what its latest call got, so an old
		// failure never reads as the current state.
		if latest := g.latestCall(); latest != nil {
			row["latest"] = latest.outcome()
		}
		if g.recovered > 0 {
			row["recovered"] = g.recovered
		}
	}
	return row
}

// line renders a group as one line: its failing (or dominant) status, the
// call, the count, a storm's interval, and a failure body's detail. A group
// whose failures all recovered leads with its latest status.
func (g *requestGroup) line() string {
	head := ""
	if f := g.latestFailure(); f != nil && g.openFailures() > 0 {
		head = f.outcome()
	} else if f != nil {
		head = g.latestCall().outcome()
	} else if g.storm != nil {
		head = "storm"
	} else {
		best, bestN := "", 0
		for status, n := range g.statuses {
			if n > bestN || (n == bestN && status < best) {
				best, bestN = status, n
			}
		}
		head = best
	}
	line := fmt.Sprintf("%s %s %s x%d", head, g.method, g.url, len(g.calls))
	if g.storm != nil {
		line += fmt.Sprintf(" every %dms", g.storm.intervalMs)
		if head != "storm" {
			line += " (storm)"
		}
	}
	if f := g.latestFailure(); f != nil {
		if g.openFailures() == 0 {
			line += fmt.Sprintf(" (recovered from %s)", f.outcome())
		}
		if detail := bodyDetail(f.body); detail != "" {
			line += fmt.Sprintf(" %q", detail)
		}
	}
	return line
}

func (g *requestGroup) matches(expand string) bool {
	expand = strings.TrimSpace(expand)
	if expand == "" {
		return false
	}
	if strings.Contains(g.url, expand) || strings.Contains(g.method+" "+g.url, expand) {
		return true
	}
	for _, c := range g.calls {
		if strings.Contains(c.url, expand) {
			return true
		}
	}
	return false
}

func callRow(c *netCall, origin string) map[string]interface{} {
	row := map[string]interface{}{
		"id":     c.id,
		"method": c.method,
		"url":    displayURL(c.url, origin),
		"status": c.outcome(),
		"at_ms":  c.startMs,
	}
	if c.status > 0 {
		row["status"] = c.status
	}
	if c.durationMs > 0 {
		row["duration_ms"] = c.durationMs
	}
	if c.errorText != "" {
		row["error"] = c.errorText
	}
	if c.body != "" {
		row["body"] = c.body
	}
	return row
}

// wsGroup is every WebSocket connection to one URL and its lifecycle.
type wsGroup struct {
	url     string
	conns   []string
	events  map[string]int
	failed  int
	detail  string
	firstMs int64
	lastMs  int64
}

// A WebSocket URL connected this many times inside the scope is a reconnect loop.
const wsReconnectLoopConnections = 3

func groupWebSockets(records []wsRecord) []*wsGroup {
	type conn struct {
		opened, closed, errored bool
		detail                  string
	}
	byURL := make(map[string]*wsGroup)
	conns := make(map[string]*conn)
	connURL := make(map[string]string)
	out := make([]*wsGroup, 0)
	for _, r := range records {
		u := r.url
		if u == "" {
			u = connURL[r.id]
		}
		if u == "" {
			continue
		}
		connURL[r.id] = u
		g, ok := byURL[u]
		if !ok {
			g = &wsGroup{url: u, events: make(map[string]int)}
			byURL[u] = g
			out = append(out, g)
		}
		c, ok := conns[r.id]
		if !ok {
			c = &conn{}
			conns[r.id] = c
			g.conns = append(g.conns, r.id)
		}
		g.events[r.event]++
		switch r.event {
		case "open":
			c.opened = true
		case "closed":
			c.closed = true
		case "error":
			c.errored = true
		}
		if r.detail != "" && (r.event == "error" || r.event == "closed") {
			c.detail = r.detail
		}
		if r.ts > 0 && (g.firstMs == 0 || r.ts < g.firstMs) {
			g.firstMs = r.ts
		}
		if r.ts > g.lastMs {
			g.lastMs = r.ts
		}
	}
	for _, g := range out {
		for _, id := range g.conns {
			c := conns[id]
			if c.errored || (c.closed && !c.opened) {
				g.failed++
				if c.detail != "" {
					g.detail = c.detail
				} else if g.detail == "" && !c.opened {
					g.detail = "closed before open"
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].failed > 0) != (out[j].failed > 0) {
			return out[i].failed > 0
		}
		return len(out[i].conns) > len(out[j].conns)
	})
	return out
}

func (g *wsGroup) reconnectLoop() bool { return len(g.conns) >= wsReconnectLoopConnections }

func (g *wsGroup) row() map[string]interface{} {
	row := map[string]interface{}{
		"url":         g.url,
		"connections": len(g.conns),
		"events":      g.events,
		"failed":      g.failed,
	}
	if g.detail != "" {
		row["detail"] = clipText(g.detail, 160)
	}
	if g.reconnectLoop() {
		row["reconnect_loop"] = true
	}
	return row
}

func (g *wsGroup) line() string {
	switch {
	case g.reconnectLoop():
		return fmt.Sprintf("ws reconnect loop %s x%d connections, %d failed", g.url, len(g.conns), g.failed)
	case g.failed > 0 && g.detail != "":
		return fmt.Sprintf("ws %s failed: %s", g.url, clipText(g.detail, 100))
	case g.failed > 0:
		return fmt.Sprintf("ws %s failed", g.url)
	default:
		return fmt.Sprintf("ws %s x%d connections", g.url, len(g.conns))
	}
}

// renderNetwork is the network topic: requests grouped by method and
// normalized URL, open failures first, storms flagged, data calls before page
// scaffolding, WebSockets by URL, and one group's individual calls when expand
// names it. The compact and summary views fold healthy asset groups (chunks,
// stylesheets, fonts, hot updates) into one assets summary. pageStartMs, when
// set, is where the page on screen began: calls before it are marked
// before_navigation.
func renderNetwork(d *diagnosis, view, expand string, maxItems int, pageStartMs int64) map[string]interface{} {
	groups := d.groups
	failedGroups, recoveredGroups, storms := 0, 0, 0
	for _, g := range groups {
		switch {
		case g.openFailures() > 0:
			failedGroups++
		case g.recovered > 0:
			recoveredGroups++
		}
		if g.storm != nil {
			storms++
		}
	}
	wsFailed := 0
	for _, g := range d.wsGroups {
		wsFailed += g.failed
	}
	status := "ok"
	switch {
	case failedGroups > 0:
		status = "error"
	case storms > 0 || wsFailed > 0 || recoveredGroups > 0:
		status = "warning"
	}
	summary := fmt.Sprintf("requests=%d groups=%d failed_groups=%d storms=%d", len(d.ev.calls), len(groups), failedGroups, storms)
	if recoveredGroups > 0 {
		summary += fmt.Sprintf(" recovered_groups=%d", recoveredGroups)
	}
	if before := callsBefore(d.ev.calls, pageStartMs); before > 0 {
		summary += fmt.Sprintf(" before_navigation=%d", before)
	}
	if len(d.wsGroups) > 0 {
		summary += fmt.Sprintf(" ws=%d ws_failed=%d", len(d.wsGroups), wsFailed)
	}
	out := map[string]interface{}{
		"success": true,
		"topic":   "network",
		"status":  status,
		"summary": summary,
	}
	groupRow := func(g *requestGroup) map[string]interface{} {
		row := g.row()
		if before := callsBefore(g.calls, pageStartMs); before > 0 {
			row["before_navigation"] = before
		}
		return row
	}

	if strings.TrimSpace(expand) != "" {
		matched := make([]map[string]interface{}, 0)
		rows := make([]map[string]interface{}, 0)
		for _, g := range groups {
			if !g.matches(expand) {
				continue
			}
			matched = append(matched, groupRow(g))
			calls := g.calls
			if len(calls) > maxItems {
				calls = calls[len(calls)-maxItems:]
			}
			for _, c := range calls {
				row := callRow(c, d.ev.origin)
				if pageStartMs > 0 && c.startMs > 0 && c.startMs < pageStartMs {
					row["before_navigation"] = true
				}
				rows = append(rows, row)
			}
		}
		for _, g := range d.wsGroups {
			if strings.Contains(g.url, expand) {
				matched = append(matched, g.row())
			}
		}
		out["expand"] = expand
		out["groups"] = matched
		out["calls"] = rows
		if len(matched) == 0 {
			out["note"] = "no request matched expand"
		}
		return out
	}

	// Healthy scaffolding folds into one summary; everything else is listed.
	listed := make([]*requestGroup, 0, len(groups))
	assets := assetSummary{statuses: map[string]int{}}
	for _, g := range groups {
		if view != "full" && g.healthyAsset() {
			assets.add(g)
			continue
		}
		listed = append(listed, g)
	}

	lines := make([]string, 0, 3)
	for _, g := range listed {
		if g.openFailures() == 0 && g.storm == nil {
			continue
		}
		lines = append(lines, g.line())
		if len(lines) == 3 {
			break
		}
	}
	for _, g := range d.wsGroups {
		if len(lines) == 3 {
			break
		}
		if g.failed > 0 || g.reconnectLoop() {
			lines = append(lines, g.line())
		}
	}
	for _, g := range listed {
		if len(lines) == 3 {
			break
		}
		if g.openFailures() == 0 && g.storm == nil {
			lines = append(lines, g.line())
		}
	}
	if len(lines) < 3 && assets.groups > 0 {
		lines = append(lines, assets.line())
	}

	switch view {
	case "summary":
		out["top"] = lines
	default:
		limit := maxItems
		if view == "full" {
			limit = len(listed)
		}
		rows := make([]map[string]interface{}, 0, minInt(limit, len(listed)))
		for i, g := range listed {
			if i >= limit {
				break
			}
			rows = append(rows, groupRow(g))
		}
		out["groups"] = rows
		if len(listed) > limit {
			out["more_groups"] = len(listed) - limit
		}
		if assets.groups > 0 {
			out["assets"] = assets.row()
		}
		if len(d.wsGroups) > 0 {
			ws := make([]map[string]interface{}, 0, len(d.wsGroups))
			for _, g := range d.wsGroups {
				ws = append(ws, g.row())
			}
			out["websockets"] = ws
		}
	}
	for _, g := range groups {
		if g.openFailures() > 0 || g.storm != nil {
			out["next"] = map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "network", "expand": expandKey(g)}}
			break
		}
	}
	return out
}

// healthyAsset is a scaffolding group with nothing to report: no open or
// recovered failure and no storm.
func (g *requestGroup) healthyAsset() bool {
	return g.asset && g.diagFailures == 0 && g.storm == nil
}

// assetSummary folds healthy scaffolding groups into one line of counts.
type assetSummary struct {
	groups, requests, failed int
	statuses                 map[string]int
}

func (a *assetSummary) add(g *requestGroup) {
	a.groups++
	a.requests += len(g.calls)
	a.failed += g.failures
	for status, n := range g.statuses {
		a.statuses[status] += n
	}
}

func (a *assetSummary) row() map[string]interface{} {
	row := map[string]interface{}{
		"groups":   a.groups,
		"requests": a.requests,
		"statuses": a.statuses,
		"failed":   a.failed,
		"note":     "healthy page assets; view=full lists them",
	}
	return row
}

func (a *assetSummary) line() string {
	keys := make([]string, 0, len(a.statuses))
	for status := range a.statuses {
		keys = append(keys, status)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, status := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", status, a.statuses[status]))
	}
	return fmt.Sprintf("assets %d requests in %d groups: %s", a.requests, a.groups, strings.Join(parts, ", "))
}

// callsBefore counts the calls that started before startMs; 0 when unset.
func callsBefore(calls []*netCall, startMs int64) int {
	if startMs <= 0 {
		return 0
	}
	n := 0
	for _, c := range calls {
		if c.startMs > 0 && c.startMs < startMs {
			n++
		}
	}
	return n
}

// expandKey is the shortest string that selects a group for expand: its URL
// without the query.
func expandKey(g *requestGroup) string {
	u := g.url
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	return u
}
