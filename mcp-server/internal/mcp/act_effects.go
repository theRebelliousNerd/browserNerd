package mcp

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

// The effects digest answers "what did those actions cause?" in the act
// result itself, so an agent does not spend an observe or reason call to
// learn that its click produced a 500, a console error and an error toast.
// It reads the facts the browser recorded since the batch started, plus a
// before/after read of the page's alert and status regions.

const (
	defaultSettle     = 1500 * time.Millisecond
	settleQuiet       = 250 * time.Millisecond
	effectsListCap    = 5
	effectsTextCap    = 120
	pendingMinimumAge = 300 * time.Millisecond
)

// liveRegionScript reads the visible alert, status and toast regions.
const liveRegionScript = `() => {
  const sel = '[role=alert],[role=alertdialog],[role=status],[aria-live=assertive],[aria-live=polite],[data-sonner-toast],.Toastify__toast,[class*="toast" i],[class*="snackbar" i]';
  // A router's navigation announcement (the page title, read aloud) is not a toast.
  const announcer = 'next-route-announcer, #__next-route-announcer__, #gatsby-announcer, [data-route-announcer]';
  const title = (document.title || '').trim();
  const found = [];
  const out = [];
  for (const el of document.querySelectorAll(sel)) {
    if (found.some((f) => f.contains(el))) continue;
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    if (r.width <= 0 || r.height <= 0 || cs.visibility === 'hidden' || cs.display === 'none') continue;
    const text = (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 160);
    if (!text || el.closest(announcer) || (title !== '' && text === title)) continue;
    found.push(el);
    const role = el.getAttribute('role') || (el.getAttribute('aria-live') ? 'status' : 'toast');
    out.push(role + ': ' + text);
    if (out.length >= 12) break;
  }
  return out;
}`

// actBaseline is the page before the batch ran.
type actBaseline struct {
	session string
	url     string
	title   string
	regions map[string]bool
}

func readLiveRegions(ctx context.Context, sessions *browser.SessionManager, sessionID string) ([]string, bool) {
	if sessions == nil || sessionID == "" {
		return nil, false
	}
	page, ok := sessions.Page(sessionID)
	if !ok {
		return nil, false
	}
	res, err := page.Context(ctx).Timeout(1500 * time.Millisecond).Eval(liveRegionScript)
	if err != nil {
		return nil, false
	}
	return jsonStrings(res.Value.Arr()), true
}

func captureActBaseline(ctx context.Context, sessions *browser.SessionManager, sessionID string) actBaseline {
	base := actBaseline{session: sessionID, regions: map[string]bool{}}
	if sessions == nil || sessionID == "" {
		return base
	}
	page, ok := sessions.Page(sessionID)
	if !ok {
		return base
	}
	if info, err := page.Info(); err == nil && info != nil {
		base.url, base.title = info.URL, info.Title
	}
	regions, _ := readLiveRegions(ctx, sessions, sessionID)
	for _, r := range regions {
		base.regions[r] = true
	}
	return base
}

// effectsWindow selects the facts a batch caused.
type effectsWindow struct {
	since    time.Time
	sessions map[string]bool
}

func (w effectsWindow) covers(f mangle.Fact) bool {
	if len(f.Args) == 0 || f.Timestamp.Before(w.since) {
		return false
	}
	return w.sessions[fmt.Sprint(f.Args[0])]
}

// settleEffects waits, up to limit, for the requests the batch started to be
// answered, its downloads to finish, and their consequences (console output,
// toasts) to land.
func settleEffects(ctx context.Context, engine *mangle.Engine, w effectsWindow, lastOp time.Time, limit time.Duration) {
	if engine == nil || limit <= 0 {
		return
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if time.Since(lastOp) >= settleQuiet &&
			len(pendingRequests(engine, w, lastOp.Add(settleQuiet))) == 0 &&
			!downloadsInProgress(engine, w) {
			return
		}
		if sleepWithContext(ctx, 50*time.Millisecond) != nil {
			return
		}
	}
}

// pageDrainBound bounds the drain of one page's tracker buffer before the
// digest is read; a wedged renderer costs this and no more.
const pageDrainBound = 1500 * time.Millisecond

// drainTouchedPages turns what each touched page's trackers buffered since
// their last 500 ms poll - toasts, clicks, input - into facts, so the digest
// reads a toast raised during the settle window instead of missing it or
// seeing it only as a live region.
func drainTouchedPages(ctx context.Context, sessions *browser.SessionManager, touched map[string]bool) {
	if sessions == nil {
		return
	}
	for id := range touched {
		drainCtx, cancel := context.WithTimeout(ctx, pageDrainBound)
		_ = sessions.DrainPageEvents(drainCtx, id)
		cancel()
	}
}

// downloadsInProgress reports a download in the window whose latest state is
// not yet completed or canceled.
func downloadsInProgress(engine *mangle.Engine, w effectsWindow) bool {
	latest := map[string]string{}
	for _, f := range engine.FactsByPredicate("download") {
		if w.covers(f) && len(f.Args) >= 5 {
			latest[fmt.Sprint(f.Args[1])] = fmt.Sprint(f.Args[4])
		}
	}
	for _, state := range latest {
		if state != "completed" && state != "canceled" {
			return true
		}
	}
	return false
}

type requestKey struct{ session, id string }

type requestInfo struct {
	method, url string
	at          time.Time
}

func requestsByID(engine *mangle.Engine) map[requestKey]requestInfo {
	out := map[requestKey]requestInfo{}
	for _, f := range engine.FactsByPredicate("net_request") {
		if len(f.Args) < 4 {
			continue
		}
		out[requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}] = requestInfo{
			method: fmt.Sprint(f.Args[2]), url: fmt.Sprint(f.Args[3]), at: f.Timestamp,
		}
	}
	return out
}

// pendingRequests lists requests started in the window, before startedBefore,
// that have no response yet.
func pendingRequests(engine *mangle.Engine, w effectsWindow, startedBefore time.Time) []requestInfo {
	answered := map[requestKey]bool{}
	for _, f := range engine.FactsByPredicate("net_response") {
		if len(f.Args) >= 2 {
			answered[requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}] = true
		}
	}
	var pending []requestInfo
	for _, f := range engine.FactsByPredicate("net_request") {
		if !w.covers(f) || len(f.Args) < 4 || !f.Timestamp.Before(startedBefore) {
			continue
		}
		key := requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}
		if answered[key] || strings.HasPrefix(fmt.Sprint(f.Args[3]), "data:") {
			continue
		}
		pending = append(pending, requestInfo{method: fmt.Sprint(f.Args[2]), url: fmt.Sprint(f.Args[3]), at: f.Timestamp})
	}
	return pending
}

// countedList keeps first-seen order and counts repeats.
type countedList struct {
	order  []string
	counts map[string]int
}

func (c *countedList) add(s string) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	if c.counts[s] == 0 {
		c.order = append(c.order, s)
	}
	c.counts[s]++
}

func (c *countedList) items() []string {
	out := make([]string, 0, len(c.order))
	for i, s := range c.order {
		if i == effectsListCap {
			out = append(out, fmt.Sprintf("+%d more", len(c.order)-effectsListCap))
			break
		}
		if n := c.counts[s]; n > 1 {
			s = fmt.Sprintf("%s (x%d)", s, n)
		}
		out = append(out, s)
	}
	return out
}

// collectActEffects builds the digest: only keys with something in them.
func collectActEffects(
	ctx context.Context,
	engine *mangle.Engine,
	sessions *browser.SessionManager,
	base actBaseline,
	active string,
	w effectsWindow,
) map[string]interface{} {
	effects := map[string]interface{}{}
	origin := originOf(base.url)

	if active != "" && sessions != nil {
		if page, ok := sessions.Page(active); ok {
			if info, err := page.Info(); err == nil && info != nil && origin == "" {
				// A batch that opened its tab has no baseline URL; name paths
				// relative to the page the batch ended on.
				origin = originOf(info.URL)
			}
		}
	}
	if active != "" && active == base.session && sessions != nil {
		if page, ok := sessions.Page(active); ok {
			if info, err := page.Info(); err == nil && info != nil {
				if base.url != "" && info.URL != base.url {
					effects["url_changed"] = map[string]interface{}{"from": shortURL(base.url, origin), "to": shortURL(info.URL, origin)}
				}
				if base.title != "" && info.Title != base.title {
					effects["title"] = clip(info.Title)
				}
			}
		}
	}

	var sightings []toastSighting
	if engine != nil {
		for _, f := range engine.FactsByPredicate("toast_notification") {
			if !w.covers(f) || len(f.Args) < 3 {
				continue
			}
			level := fmt.Sprint(f.Args[2])
			// An info-level notification is usually a status region's passing
			// text ("loading"); what it settled on is read from the live
			// regions below.
			switch level {
			case "error", "warning":
				sightings = append(sightings, toastSighting{label: level, text: fmt.Sprint(f.Args[1])})
			case "success":
				sightings = append(sightings, toastSighting{text: fmt.Sprint(f.Args[1])})
			}
		}
	}
	if regions, ok := readLiveRegions(ctx, sessions, active); ok {
		for _, region := range regions {
			if base.regions[region] && active == base.session {
				continue
			}
			role, text := "", region
			if i := strings.Index(region, ": "); i >= 0 {
				role, text = region[:i], region[i+2:]
			}
			sightings = append(sightings, toastSighting{label: role, text: text})
		}
	}
	if items := digestToasts(sightings); len(items) > 0 {
		effects["toasts"] = items
	}

	if engine == nil {
		return effects
	}

	var consoleErrors countedList
	for _, f := range engine.FactsByPredicate("console_event") {
		if !w.covers(f) || len(f.Args) < 3 {
			continue
		}
		if level := fmt.Sprint(f.Args[1]); level == "error" || level == "assert" {
			consoleErrors.add(clip(fmt.Sprint(f.Args[2])))
		}
	}
	if items := consoleErrors.items(); len(items) > 0 {
		effects["errors"] = items
	}

	requests := requestsByID(engine)
	// A 4xx on a page asset (favicon, image, font, ...) is not something an
	// action broke; failed_request draws the same line.
	assets := map[requestKey]bool{}
	for _, f := range engine.FactsByPredicate("net_http_error") {
		if len(f.Args) >= 5 && assetResourceTypes[fmt.Sprint(f.Args[4])] {
			if status, ok := numberValue(f.Args[3]); ok && status < 500 {
				assets[requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}] = true
			}
		}
	}
	bodies := map[requestKey]string{}
	for _, f := range engine.FactsByPredicate("net_failure_body") {
		if len(f.Args) >= 3 {
			bodies[requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}] = fmt.Sprint(f.Args[2])
		}
	}
	var failed countedList
	for _, f := range engine.FactsByPredicate("net_response") {
		if !w.covers(f) || len(f.Args) < 3 {
			continue
		}
		status, ok := numberValue(f.Args[2])
		if !ok || status < 400 {
			continue
		}
		key := requestKey{fmt.Sprint(f.Args[0]), fmt.Sprint(f.Args[1])}
		if assets[key] {
			continue
		}
		req, known := requests[key]
		label := fmt.Sprintf("%.0f", status)
		if known {
			label = fmt.Sprintf("%s %s %.0f", req.method, shortURL(req.url, origin), status)
		}
		if body := strings.TrimSpace(bodies[key]); body != "" {
			label += ": " + clip(body)
		}
		failed.add(label)
	}
	if items := failed.items(); len(items) > 0 {
		effects["failed_requests"] = items
	}

	var pending countedList
	for _, req := range pendingRequests(engine, w, time.Now().Add(-pendingMinimumAge)) {
		pending.add(req.method + " " + shortURL(req.url, origin))
	}
	if items := pending.items(); len(items) > 0 {
		effects["pending"] = items
	}

	var dialogs countedList
	if sessions != nil {
		for session := range w.sessions {
			for _, d := range sessions.DialogsSince(session, w.since) {
				dialogs.add(describeDialog(d.Type, d.Message, d.Accepted))
			}
		}
	}
	if len(dialogs.order) == 0 {
		for _, f := range engine.FactsByPredicate("js_dialog") {
			if !w.covers(f) || len(f.Args) < 4 {
				continue
			}
			accepted, _ := f.Args[3].(bool)
			dialogs.add(describeDialog(fmt.Sprint(f.Args[1]), fmt.Sprint(f.Args[2]), accepted))
		}
	}
	if items := dialogs.items(); len(items) > 0 {
		effects["dialogs"] = items
	}

	downloads := map[string]string{}
	var downloadOrder []string
	for _, f := range engine.FactsByPredicate("download") {
		if !w.covers(f) || len(f.Args) < 5 {
			continue
		}
		guid := fmt.Sprint(f.Args[1])
		if _, seen := downloads[guid]; !seen {
			downloadOrder = append(downloadOrder, guid)
		}
		name := fmt.Sprint(f.Args[3])
		if name == "" {
			name = shortURL(fmt.Sprint(f.Args[2]), origin)
		}
		downloads[guid] = name + " " + fmt.Sprint(f.Args[4])
	}
	if len(downloadOrder) > 0 {
		var list countedList
		for _, guid := range downloadOrder {
			list.add(downloads[guid])
		}
		effects["downloads"] = list.items()
	}

	var wsFailures countedList
	for _, f := range engine.FactsByPredicate("ws_event") {
		if !w.covers(f) || len(f.Args) < 5 {
			continue
		}
		event := fmt.Sprint(f.Args[3])
		if !wsFailurePattern.MatchString(event) {
			continue
		}
		label := fmt.Sprint(f.Args[2]) + " " + event
		if detail := strings.TrimSpace(fmt.Sprint(f.Args[4])); detail != "" {
			label += ": " + detail
		}
		wsFailures.add(clip(label))
	}
	if items := wsFailures.items(); len(items) > 0 {
		effects["ws_failures"] = items
	}

	var loadFailures countedList
	for _, f := range engine.FactsByPredicate("page_load_failed") {
		if !w.covers(f) || len(f.Args) < 3 {
			continue
		}
		loadFailures.add(clip(shortURL(fmt.Sprint(f.Args[1]), origin) + ": " + fmt.Sprint(f.Args[2])))
	}
	if items := loadFailures.items(); len(items) > 0 {
		effects["load_failed"] = items
	}
	return effects
}

var wsFailurePattern = regexp.MustCompile(`(?i)error|fail|abnormal`)

// toastSighting is one sighting of a notification: a toast fact, labelled
// with its level (error, warning; empty for success), or a live region read
// when the batch settled, labelled with its role (alert, status, toast).
type toastSighting struct {
	label string
	text  string
}

// toastPartMinimum is the shortest key that may merge into a longer one as its
// title or description; a shorter one ("ok") is a coincidence, not a part.
const toastPartMinimum = 6

// toastKey is a notification's text without whitespace or case, so the fact
// read from textContent ("Login failedInvalid credentials") and the region
// read from innerText ("Login failed Invalid credentials") are one key.
func toastKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), ""))
}

// sameToast reports whether two keys are sightings of one notification: the
// same text, or one is the other's title (prefix) or description (suffix).
func sameToast(a, b string) bool {
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= toastPartMinimum && (strings.HasPrefix(long, short) || strings.HasSuffix(long, short))
}

// toastLabelRank orders labels by what they tell the reader: a level beats a
// role, and a role beats nothing.
func toastLabelRank(label string) int {
	switch label {
	case "error":
		return 3
	case "warning":
		return 2
	case "":
		return 0
	}
	return 1
}

// digestToasts folds the sightings of each notification into one entry. A
// toast is seen as a fact and again as a live region, and a toast library
// re-renders its title and description as parts: listing each sighting
// reported one failed login as four toasts. Each entry keeps the fullest
// text, the spelling with its words separated, and the most telling label.
func digestToasts(sightings []toastSighting) []string {
	type entry struct{ label, text, key string }
	var entries []*entry
	merge := func(into *entry, label, text, key string) {
		if len(key) > len(into.key) || (len(key) == len(into.key) && strings.Count(text, " ") > strings.Count(into.text, " ")) {
			into.text, into.key = text, key
		}
		if toastLabelRank(label) > toastLabelRank(into.label) {
			into.label = label
		}
	}
	for _, s := range sightings {
		text := strings.Join(strings.Fields(s.text), " ")
		key := toastKey(text)
		if key == "" {
			continue
		}
		var host *entry
		for _, e := range entries {
			if sameToast(e.key, key) {
				host = e
				break
			}
		}
		if host == nil {
			entries = append(entries, &entry{label: s.label, text: text, key: key})
			continue
		}
		merge(host, s.label, text, key)
	}
	// An entry that grew can now contain one listed after it.
	kept := entries[:0:0]
	for _, e := range entries {
		var host *entry
		for _, k := range kept {
			if sameToast(k.key, e.key) {
				host = k
				break
			}
		}
		if host == nil {
			kept = append(kept, e)
			continue
		}
		merge(host, e.label, e.text, e.key)
	}
	var list countedList
	for _, e := range kept {
		if e.label == "" {
			list.add(clip(e.text))
			continue
		}
		list.add(clip(e.label + ": " + e.text))
	}
	return list.items()
}

// assetResourceTypes are the CDP resource types failed_request treats as page
// assets rather than data calls.
var assetResourceTypes = map[string]bool{
	"Image": true, "Font": true, "Stylesheet": true, "Media": true,
	"Manifest": true, "TextTrack": true, "Other": true,
}

func describeDialog(kind, message string, accepted bool) string {
	answer := "dismissed"
	if accepted {
		answer = "accepted"
	}
	return fmt.Sprintf("%s %q %s", kind, clip(message), answer)
}

// effectsSummary folds the digest into one line for view:summary.
func effectsSummary(effects map[string]interface{}) string {
	if len(effects) == 0 {
		return ""
	}
	var parts []string
	if u, ok := effects["url_changed"].(map[string]interface{}); ok {
		parts = append(parts, fmt.Sprintf("url %v -> %v", u["from"], u["to"]))
	}
	listed := func(key, label string) {
		if items, ok := effects[key].([]string); ok && len(items) > 0 {
			parts = append(parts, label+" "+strings.Join(items, "; "))
		}
	}
	listed("failed_requests", "failed")
	listed("errors", "console error")
	listed("dialogs", "dialog")
	listed("toasts", "toast")
	listed("pending", "pending")
	listed("downloads", "download")
	listed("ws_failures", "websocket")
	listed("load_failed", "load failed")
	return strings.Join(parts, " | ")
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// shortURL drops the origin when it is the page's own, and caps the length.
func shortURL(raw, origin string) string {
	if origin != "" && strings.HasPrefix(raw, origin) {
		raw = strings.TrimPrefix(raw, origin)
		if raw == "" {
			raw = "/"
		}
	}
	return clip(raw)
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > effectsTextCap {
		return s[:effectsTextCap-3] + "..."
	}
	return s
}
