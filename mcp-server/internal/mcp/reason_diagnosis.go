package mcp

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// causalEffect is something the user or the console saw that a failed
// request explains: a console error, an error toast, or an alert.
type causalEffect struct {
	kind    string
	message string
	atMs    int64
	afterMs int64
	count   int
}

// failureGroup is every diagnostic failure of one method, normalized URL and
// outcome, with the effects linked to any of them.
type failureGroup struct {
	calls    []*netCall
	effects  []*causalEffect
	reqGroup *requestGroup
}

func (fg *failureGroup) latest() *netCall { return fg.calls[len(fg.calls)-1] }

// reasonIssue is one ranked finding. line is its one-line rendering; detail is
// what root_cause shows when the issue leads; blocking names why it can leave
// a page empty or stuck.
type reasonIssue struct {
	kind     string
	level    string
	severity int
	line     string
	atMs     int64
	detail   map[string]interface{}
	next     map[string]interface{}
	blocking string
	failure  *failureGroup
}

type healthScore struct {
	failed5xx       int
	failed4xx       int
	failedNetwork   int
	consoleErrors   int
	consoleWarnings int
	toastErrors     int
	storms          int
	wsFailures      int
	dialogs         int
	pageLoadFailed  int
	slow            int
	blocking        int
}

func (s healthScore) row() map[string]interface{} {
	return map[string]interface{}{
		"failed_5xx":       s.failed5xx,
		"failed_4xx":       s.failed4xx,
		"failed_network":   s.failedNetwork,
		"console_errors":   s.consoleErrors,
		"console_warnings": s.consoleWarnings,
		"toast_errors":     s.toastErrors,
		"storms":           s.storms,
		"ws_failures":      s.wsFailures,
		"dialogs":          s.dialogs,
		"page_load_failed": s.pageLoadFailed,
		"slow":             s.slow,
		"blocking":         s.blocking,
	}
}

// diagnosis is the ranked reading of one scope's evidence.
type diagnosis struct {
	ev       *browserEvidence
	groups   []*requestGroup
	wsGroups []*wsGroup
	console  []*consoleGroup
	toasts   []*consoleGroup
	failures []*failureGroup
	issues   []*reasonIssue
	score    healthScore
	status   string
	linked   int
}

const slowRequestMs = 1000

var errorTextPattern = regexp.MustCompile(`(?i)\b(error|errors|failed|failure|fail|could not|couldn't|cannot|unable|denied|forbidden|unauthori[sz]ed|not found|timed out|timeout|invalid|went wrong)\b`)

// toastIsError reports an error toast: level error, or any non-success toast
// whose text reads as a failure (a role=alert region carries no level).
func toastIsError(t toastRecord) bool {
	if t.level == "error" {
		return true
	}
	return t.level != "success" && t.level != "warning" && errorTextPattern.MatchString(t.text)
}

type effectCandidate struct {
	kind    string
	message string
	ts      int64
	linked  bool
}

// linkWindow bounds how long after a failed request's outcome an effect may
// appear and still be its effect. Console errors follow within a promise tick;
// toasts and alerts render later. The small negative slack absorbs the gap
// between the page clock and the recorder clock.
func linkWindow(kind string) (int64, int64) {
	if kind == "console_error" {
		return -50, 2000
	}
	return -300, 5000
}

// An effect that names the request (its status or path) may follow it by up to this long.
const mentionedLinkWindowMs = 10000

func mentionsCall(message string, c *netCall) bool {
	m := strings.ToLower(message)
	if c.status > 0 && strings.Contains(m, strconv.Itoa(c.status)) {
		return true
	}
	p := strings.ToLower(requestPath(c.url))
	return p != "" && p != "/" && strings.Contains(m, p)
}

// bestCause picks the failed request an effect most likely follows from: one
// the effect names wins, then the nearest preceding outcome in the window.
func bestCause(e effectCandidate, failures []*netCall) *netCall {
	var best *netCall
	bestMention := false
	bestDist := int64(math.MaxInt64)
	for _, c := range failures {
		done := c.doneMs()
		if done <= 0 || e.ts <= 0 {
			continue
		}
		delta := e.ts - done
		lo, hi := linkWindow(e.kind)
		mention := mentionsCall(e.message, c)
		if mention && hi < mentionedLinkWindowMs {
			hi = mentionedLinkWindowMs
		}
		if delta < lo || delta > hi {
			continue
		}
		dist := delta
		if dist < 0 {
			dist = -dist
		}
		if best == nil || (mention && !bestMention) || (mention == bestMention && dist < bestDist) {
			best, bestMention, bestDist = c, mention, dist
		}
	}
	return best
}

func diagnose(ev *browserEvidence) *diagnosis {
	d := &diagnosis{ev: ev}
	d.groups = groupRequests(ev.calls, ev.origin)
	d.wsGroups = groupWebSockets(ev.ws)
	groupOf := make(map[*netCall]*requestGroup)
	for _, g := range d.groups {
		for _, c := range g.calls {
			groupOf[c] = g
		}
	}

	failures := make([]*netCall, 0)
	for _, c := range ev.calls {
		if !c.diagnosticFailure() {
			continue
		}
		failures = append(failures, c)
		switch c.failureClass() {
		case "5xx":
			d.score.failed5xx++
		case "4xx":
			d.score.failed4xx++
		case "network":
			d.score.failedNetwork++
		}
	}

	// Effects, linked to the failure that explains them.
	effects := make([]*effectCandidate, 0)
	for _, r := range ev.console {
		switch r.level {
		case "error":
			d.score.consoleErrors++
			effects = append(effects, &effectCandidate{kind: "console_error", message: r.message, ts: r.ts})
		case "warning":
			d.score.consoleWarnings++
		}
	}
	for _, t := range ev.toasts {
		if toastIsError(t) {
			d.score.toastErrors++
			effects = append(effects, &effectCandidate{kind: "toast", message: t.text, ts: t.ts})
		}
	}
	for _, dl := range ev.dialogs {
		if dl.kind == "alert" {
			effects = append(effects, &effectCandidate{kind: "alert", message: dl.message, ts: dl.ts})
		}
	}
	causeOf := make(map[*effectCandidate]*netCall)
	for _, e := range effects {
		if c := bestCause(*e, failures); c != nil {
			causeOf[e] = c
			e.linked = true
			d.linked++
		}
	}

	// Failures grouped by method, normalized URL and outcome.
	byKey := make(map[string]*failureGroup)
	fgOf := make(map[*netCall]*failureGroup)
	for _, c := range failures {
		key := c.method + " " + normalizeRequestURL(c.url, ev.origin) + " " + c.outcome()
		fg, ok := byKey[key]
		if !ok {
			fg = &failureGroup{reqGroup: groupOf[c]}
			byKey[key] = fg
			d.failures = append(d.failures, fg)
		}
		fg.calls = append(fg.calls, c)
		fgOf[c] = fg
	}
	for _, e := range effects {
		c, ok := causeOf[e]
		if !ok {
			continue
		}
		fg := fgOf[c]
		fp := e.kind + "|" + consoleFingerprint(e.message)
		merged := false
		for _, existing := range fg.effects {
			if existing.kind+"|"+consoleFingerprint(existing.message) == fp {
				existing.count++
				merged = true
				break
			}
		}
		if !merged {
			// The page stamps a toast with its own clock and CDP stamps the
			// response with Chrome's; a few ms of skew must not read as an effect
			// that came before its cause.
			fg.effects = append(fg.effects, &causalEffect{kind: e.kind, message: e.message, atMs: e.ts, afterMs: maxInt64(e.ts-c.doneMs(), 0), count: 1})
		}
	}

	d.console = groupConsole(ev.console)
	toastRecords := make([]consoleRecord, 0, len(ev.toasts))
	for _, t := range ev.toasts {
		level := t.level
		if toastIsError(t) {
			level = "error"
		}
		toastRecords = append(toastRecords, consoleRecord{level: level, message: t.text, ts: t.ts})
	}
	d.toasts = groupConsole(toastRecords)

	d.addPageLoadIssues()
	d.addFailureIssues()
	d.addUnlinkedEffectIssues(effects)
	d.addDialogIssues()
	d.addNetworkIssues()
	d.addStateIssues()

	sort.SliceStable(d.issues, func(i, j int) bool {
		if d.issues[i].severity != d.issues[j].severity {
			return d.issues[i].severity > d.issues[j].severity
		}
		return d.issues[i].atMs > d.issues[j].atMs
	})
	d.status = "ok"
	for _, is := range d.issues {
		if is.level == "error" {
			d.status = "error"
			break
		}
		if is.level == "warning" {
			d.status = "warning"
		}
	}
	return d
}

func (d *diagnosis) add(is *reasonIssue) { d.issues = append(d.issues, is) }

// stuckLoad is the load failure the tab is still showing: the newest
// page_load_failed with no successful navigation after it, or a tab whose
// current URL is Chrome's error page.
func (d *diagnosis) stuckLoad() *loadFailureRecord {
	ev := d.ev
	var last *loadFailureRecord
	for i := range ev.loadFailures {
		if last == nil || ev.loadFailures[i].ts >= last.ts {
			last = &ev.loadFailures[i]
		}
	}
	onErrorPage := strings.HasPrefix(ev.currentURL, "chrome-error:")
	if last == nil {
		if !onErrorPage {
			return nil
		}
		target := ""
		for _, n := range ev.navs {
			if !strings.HasPrefix(n.url, "chrome-error:") {
				target = n.url
			}
		}
		return &loadFailureRecord{url: target, errorText: "page failed to load"}
	}
	if onErrorPage {
		return last
	}
	for _, n := range ev.navs {
		if n.ts > last.ts && !strings.HasPrefix(n.url, "chrome-error:") {
			return nil
		}
	}
	return last
}

func (d *diagnosis) addPageLoadIssues() {
	stuck := d.stuckLoad()
	if stuck != nil {
		d.score.pageLoadFailed++
		target := stuck.url
		line := "tab stuck on chrome-error"
		if target != "" {
			line += ": " + displayURL(target, "") + " failed to load"
		}
		if stuck.errorText != "" {
			line += " (" + stuck.errorText + ")"
		}
		line += "; recover: close-session, session_create, log in again"
		ops := []map[string]interface{}{{"type": "session_close"}}
		create := map[string]interface{}{"type": "session_create"}
		if target != "" {
			create["url"] = target
		}
		ops = append(ops, create)
		d.add(&reasonIssue{
			kind: "page_load_failed", level: "error", severity: 100, line: line, atMs: stuck.ts,
			detail: map[string]interface{}{
				"kind": "page_load_failed", "url": target, "error": stuck.errorText, "at_ms": stuck.ts,
				"recovery": "the tab cannot reload itself; close the session, create a new one on the URL, and log in again",
			},
			next:     map[string]interface{}{"tool": "browser-act", "args": map[string]interface{}{"operations": ops}, "then": "log in again"},
			blocking: "tab stuck on chrome-error",
		})
	}
	for _, lf := range d.ev.loadFailures {
		if stuck != nil && lf.ts == stuck.ts && lf.url == stuck.url {
			continue
		}
		if !d.ev.scope.admits(lf.ts) {
			continue
		}
		d.score.pageLoadFailed++
		d.add(&reasonIssue{
			kind: "page_load_failed", level: "warning", severity: 65, atMs: lf.ts,
			line: fmt.Sprintf("page load failed, later recovered: %s (%s)", displayURL(lf.url, ""), lf.errorText),
		})
	}
}

func (d *diagnosis) failureLine(fg *failureGroup) string {
	c := fg.latest()
	var b strings.Builder
	if c.method != "" {
		b.WriteString(c.method + " ")
	}
	b.WriteString(displayURL(c.url, d.ev.origin))
	b.WriteString(" -> " + c.outcome())
	if n := len(fg.calls); n > 1 {
		fmt.Fprintf(&b, " x%d", n)
	}
	if fg.reqGroup != nil && fg.reqGroup.storm != nil {
		fmt.Fprintf(&b, " every %dms", fg.reqGroup.storm.intervalMs)
	}
	if detail := bodyDetail(c.body); detail != "" {
		fmt.Fprintf(&b, " %q", detail)
	}
	for i, e := range fg.effects {
		if i == 3 {
			break
		}
		switch e.kind {
		case "console_error":
			if e.count > 1 {
				fmt.Fprintf(&b, "; console error x%d", e.count)
			} else {
				fmt.Fprintf(&b, "; console error %d ms later", e.afterMs)
			}
		case "toast":
			fmt.Fprintf(&b, "; toast %q", clipText(e.message, 80))
		case "alert":
			fmt.Fprintf(&b, "; alert %q", clipText(e.message, 80))
		}
	}
	return b.String()
}

func (d *diagnosis) failureDetail(fg *failureGroup) map[string]interface{} {
	detail := map[string]interface{}{"request": callRow(fg.latest(), d.ev.origin)}
	if n := len(fg.calls); n > 1 {
		detail["repeats"] = n
	}
	if len(fg.effects) > 0 {
		rows := make([]map[string]interface{}, 0, len(fg.effects))
		for _, e := range fg.effects {
			row := map[string]interface{}{"kind": e.kind, "message": clipText(e.message, 160), "after_ms": e.afterMs}
			if e.count > 1 {
				row["count"] = e.count
			}
			rows = append(rows, row)
		}
		detail["effects"] = rows
	}
	return detail
}

func (d *diagnosis) addFailureIssues() {
	for _, fg := range d.failures {
		c := fg.latest()
		class := c.failureClass()
		auth := c.status == 401 || c.status == 403
		severity := 80
		switch {
		case class == "5xx":
			severity = 92
		case class == "network":
			severity = 89
		case auth:
			severity = 86
		}
		if len(fg.effects) > 0 {
			severity += 5
		}
		if fg.reqGroup != nil && fg.reqGroup.storm != nil && severity < 91 {
			severity = 91
		}
		is := &reasonIssue{
			kind: "failed_request", level: "error", severity: severity, line: d.failureLine(fg), atMs: c.startMs,
			detail: d.failureDetail(fg), failure: fg,
		}
		if fg.reqGroup != nil {
			is.next = map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "network", "expand": expandKey(fg.reqGroup)}}
		}
		switch {
		case auth:
			is.blocking = "request unauthorized"
		case strings.EqualFold(c.method, "GET") || c.method == "":
			is.blocking = "data request failed"
		}
		d.add(is)
	}
}

func (d *diagnosis) addUnlinkedEffectIssues(effects []*effectCandidate) {
	unlinkedConsole := make([]consoleRecord, 0)
	unlinkedToasts := make([]consoleRecord, 0)
	unlinkedAlerts := make([]consoleRecord, 0)
	for _, e := range effects {
		if e.linked {
			continue
		}
		rec := consoleRecord{level: "error", message: e.message, ts: e.ts}
		switch e.kind {
		case "console_error":
			unlinkedConsole = append(unlinkedConsole, rec)
		case "toast":
			unlinkedToasts = append(unlinkedToasts, rec)
		case "alert":
			unlinkedAlerts = append(unlinkedAlerts, rec)
		}
	}
	for _, g := range groupConsole(unlinkedConsole) {
		kind, severity := "console_error", 84
		if k, _ := classifyUserVisibleError("console", g.message); k == "compiler_error" {
			kind, severity = "compiler_error", 98
		}
		is := &reasonIssue{
			kind: kind, level: "error", severity: severity, line: g.line(), atMs: g.lastMs,
			detail: map[string]interface{}{"kind": kind, "console": g.row()},
			next:   map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "why_failed", "view": "full"}},
		}
		if kind == "compiler_error" {
			is.blocking = "frontend build error"
		}
		d.add(is)
	}
	for _, g := range groupConsole(unlinkedToasts) {
		line := fmt.Sprintf("toast %q", clipText(g.message, 120))
		if g.count > 1 {
			line += fmt.Sprintf(" x%d", g.count)
		}
		d.add(&reasonIssue{kind: "toast_error", level: "error", severity: 83, line: line, atMs: g.lastMs,
			detail: map[string]interface{}{"kind": "toast_error", "toast": g.row()}})
	}
	for _, g := range groupConsole(unlinkedAlerts) {
		d.add(&reasonIssue{kind: "alert", level: "warning", severity: 76, line: fmt.Sprintf("alert %q", clipText(g.message, 120)), atMs: g.lastMs})
	}
	for _, g := range d.toasts {
		if g.level != "warning" {
			continue
		}
		d.add(&reasonIssue{kind: "toast_warning", level: "warning", severity: 50, line: fmt.Sprintf("toast warning %q", clipText(g.message, 120)), atMs: g.lastMs})
	}
	for _, g := range d.console {
		if g.level != "warning" {
			continue
		}
		d.add(&reasonIssue{kind: "console_warning", level: "info", severity: 35, line: g.line(), atMs: g.lastMs})
	}
}

func (d *diagnosis) addDialogIssues() {
	type dialogGroup struct {
		rec   dialogRecord
		count int
	}
	groups := make([]*dialogGroup, 0)
	byKey := make(map[string]*dialogGroup)
	for _, dl := range d.ev.dialogs {
		d.score.dialogs++
		key := fmt.Sprintf("%s|%s|%t|%s", dl.kind, dl.message, dl.accepted, dl.handledBy)
		if g, ok := byKey[key]; ok {
			g.count++
			if dl.ts > g.rec.ts {
				g.rec.ts = dl.ts
			}
			continue
		}
		g := &dialogGroup{rec: dl, count: 1}
		byKey[key] = g
		groups = append(groups, g)
	}
	for _, g := range groups {
		dl := g.rec
		if dl.kind == "alert" {
			continue
		}
		byPolicy := dl.handledBy != "armed"
		answer := "dismissed"
		if dl.accepted {
			answer = "accepted"
		}
		line := fmt.Sprintf("%s %q ", dl.kind, clipText(dl.message, 80))
		is := &reasonIssue{kind: "dialog", atMs: dl.ts,
			detail: map[string]interface{}{"kind": "dialog", "type": dl.kind, "message": clipText(dl.message, 160), "accepted": dl.accepted, "handled_by": dl.handledBy, "at_ms": dl.ts}}
		switch {
		case byPolicy && !dl.accepted:
			is.level, is.severity = "error", 90
			is.line = line + "auto-dismissed by policy: the action it guarded did not run"
			is.detail["hint"] = "answer the dialog explicitly (accept) before the click that opens it"
			is.blocking = "dialog auto-dismissed"
		case byPolicy:
			is.level, is.severity = "warning", 55
			is.line = line + "auto-accepted by policy"
		default:
			is.level, is.severity = "info", 30
			is.line = line + answer + " as armed"
		}
		if g.count > 1 {
			is.line += fmt.Sprintf(" x%d", g.count)
		}
		d.add(is)
	}
}

func (d *diagnosis) addNetworkIssues() {
	for _, g := range d.groups {
		if g.storm != nil {
			d.score.storms++
			if g.diagFailures == 0 {
				d.add(&reasonIssue{kind: "storm", level: "warning", severity: 72, line: g.line(), atMs: g.lastMs,
					detail: map[string]interface{}{"kind": "storm", "group": g.row()},
					next:   map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "network", "expand": expandKey(g)}}})
			}
		}
		slowCount, slowMax, slowAt := 0, int64(0), int64(0)
		for _, c := range g.calls {
			if c.durationMs > slowRequestMs && isAPIRequest(c.url) {
				slowCount++
				if c.durationMs > slowMax {
					slowMax, slowAt = c.durationMs, c.startMs
				}
			}
		}
		if slowCount > 0 {
			d.score.slow += slowCount
			line := fmt.Sprintf("slow %s %s %dms", g.method, g.url, slowMax)
			if slowCount > 1 {
				line = fmt.Sprintf("slow %s %s x%d, max %dms", g.method, g.url, slowCount, slowMax)
			}
			d.add(&reasonIssue{kind: "slow", level: "warning", severity: 60, line: line, atMs: slowAt,
				next: map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "network", "expand": expandKey(g)}}})
		}
	}
	for _, g := range d.wsGroups {
		d.score.wsFailures += g.failed
		if g.failed == 0 && !g.reconnectLoop() {
			continue
		}
		is := &reasonIssue{kind: "websocket", level: "warning", severity: 74, line: g.line(), atMs: g.lastMs,
			detail: map[string]interface{}{"kind": "websocket", "websocket": g.row()},
			next:   map[string]interface{}{"tool": "browser-reason", "args": map[string]interface{}{"topic": "network"}}}
		if g.reconnectLoop() {
			is.level, is.severity = "error", 88
		}
		d.add(is)
	}
}

var authPathPattern = regexp.MustCompile(`(?i)/(login|signin|sign-in|auth)(/|$|\?)`)

func (d *diagnosis) addStateIssues() {
	for _, row := range d.ev.backendRootCauses {
		cause := strings.TrimSpace(fmt.Sprintf("%v", row["Cause"]))
		msg := strings.TrimSpace(fmt.Sprintf("%v", row["ConsoleMsg"]))
		source := strings.TrimSpace(fmt.Sprintf("%v", row["Source"]))
		line := fmt.Sprintf("%s: %s", source, clipText(cause, 120))
		if msg != "" && msg != "<nil>" {
			line += fmt.Sprintf(" (console %q)", clipText(msg, 80))
		}
		d.add(&reasonIssue{kind: "root_cause", level: "error", severity: 95, line: line, atMs: extractTimestamp(row, "Ts", "Timestamp"),
			detail: map[string]interface{}{"kind": "root_cause", "source": source, "cause": clipText(cause, 240), "console": clipText(msg, 160)}})
	}
	for _, row := range d.ev.blocking {
		d.score.blocking++
		reason := strings.TrimSpace(fmt.Sprintf("%v", row["Reason"]))
		d.add(&reasonIssue{kind: "blocking", level: "warning", severity: 85, line: "interaction blocked: " + reason,
			blocking: reason,
			next:     map[string]interface{}{"tool": "browser-observe", "args": map[string]interface{}{"mode": "interactive"}}})
	}
	for _, row := range d.ev.contradictions {
		d.add(&reasonIssue{kind: "contradiction", level: "error", severity: 78,
			line: fmt.Sprintf("success toast shown while %v request(s) failed", row["failed_request_count"])})
	}
	if authPathPattern.MatchString(requestPath(d.ev.currentURL)) {
		d.add(&reasonIssue{kind: "auth_wall", level: "info", severity: 45, line: "on the login page: the session is not authenticated",
			blocking: "not logged in"})
	}
}

// primary is the issue the verdict names, or nil when nothing is wrong.
func (d *diagnosis) primary() *reasonIssue {
	for _, is := range d.issues {
		if is.level != "info" {
			return is
		}
	}
	return nil
}

func (d *diagnosis) verdict(scopeLabel string) string {
	if p := d.primary(); p != nil {
		return p.line
	}
	if len(d.ev.calls) == 0 && len(d.ev.console) == 0 && len(d.ev.toasts) == 0 {
		return "no browser events recorded " + scopeLabel + "; check session_id or widen time_window_ms"
	}
	return "no failures recorded " + scopeLabel
}

// issueLines renders every issue but the primary as one line each, skipping
// info-level issues unless all is set.
func (d *diagnosis) issueLines(max int, all bool) ([]string, int) {
	lines := make([]string, 0, max)
	primary := d.primary()
	more := 0
	for _, is := range d.issues {
		if is == primary || (!all && is.level == "info") {
			continue
		}
		if len(lines) >= max {
			more++
			continue
		}
		lines = append(lines, is.line)
	}
	return lines, more
}

func (d *diagnosis) issueRows() []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(d.issues))
	for _, is := range d.issues {
		row := map[string]interface{}{"kind": is.kind, "level": is.level, "line": is.line}
		if is.atMs > 0 {
			row["at_ms"] = is.atMs
		}
		rows = append(rows, row)
	}
	return rows
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
