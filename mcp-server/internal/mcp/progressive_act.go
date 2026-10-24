package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// BrowserActTool consolidates browser actions with progressive-disclosure results.
type BrowserActTool struct {
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	redactor                *security.Redactor
	specsCfg                config.SpecsConfig
	disableUnsafeJavaScript bool
	// pathPolicy confines the files an upload op may hand to a page.
	pathPolicy *security.PathPolicy
	// secrets are values browser-test resolved from value_env. They are typed
	// into the page but never appear in the result.
	secrets map[string]bool
}

// maxActBatchDuration bounds a whole batch, whatever its operations say.
const maxActBatchDuration = 5 * time.Minute

func (t *BrowserActTool) Name() string { return "browser-act" }
func (t *BrowserActTool) Description() string {
	return `Act on a page: operations run in order, each time-bounded. The batch is validated
first; a bad field rejects it before anything runs, naming the fix.

Locate by ref (browser-observe), text (visible text or name), label (form
control), role+name, or x/y (click/hover); add index when several match.

navigate{url,wait_until} history{action} interact{action,<locator>,value,submit}
fill{fields:[{<locator>,value}],submit|submit_button} key{key:"Control+a"}
upload{ref,files} sleep{duration_ms<=60000} await_stable await_fact{predicate,args}
await_conditions{conditions} wait{predicate,match_args} dialog{accept,prompt_text}
session_create{url,browser_id,isolated} (later ops use new session_id) session_focus session_close
session_fork{source_session_id} session_attach{target_id,browser_id}
browser_launch{new_instance} browser_close{browser_id} js{script} plan
viewport{preset:phone|iphone-14|pixel-7|phone-small|tablet|laptop|desktop, or width,height,device_scale_factor,
mobile,touch,orientation,toolbar_height; reset:true} sizes this tab only

type/fill read the value back; select verifies the widget shows the option;
navigate to the tab's current URL reloads it (reloaded:true); any op may carry
dialog:accept|dismiss. Results give each op's error and effects: url change,
console errors, failed requests, dialogs, toasts, live_regions, downloads.
toasts requires toast/snackbar markers; generic ARIA status/alert text is live_regions.`
}

func (t *BrowserActTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string"},
			"operations": map[string]interface{}{
				"type":  "array",
				"items": actOperationItemSchema(),
			},
			"stop_on_error": map[string]interface{}{"type": "boolean", "description": "default true"},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "default compact",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{"type": "integer", "description": "compact results cap (default 20)"},
			"settle_ms": map[string]interface{}{
				"type":        "integer",
				"description": "max wait for the batch's requests before reporting effects (default 1500)",
			},
			"include_specs": map[string]interface{}{
				"type":        "boolean",
				"description": "attach configured spec context (default false)",
			},
			"spec_terms": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		"required": []string{"operations"},
	}
}

// actPageOps change or load a page; after them the batch waits for effects.
var actPageOps = map[string]bool{
	"navigate": true, "interact": true, "fill": true, "upload": true, "key": true,
	"history": true, "js": true, "plan": true, "session_create": true, "session_fork": true,
}

// actCompactKeys are lifted from an op's result into its compact entry.
var actCompactKeys = []string{"session_id", "browser_id", "isolated", "isolated_display", "managed", "default", "url", "title", "reloaded", "located", "selected", "verified_by", "armed", "warning", "note", "dialogs"}

func (t *BrowserActTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")

	rawOps, ok := args["operations"].([]interface{})
	if !ok || len(rawOps) == 0 {
		return map[string]interface{}{"success": false, "error": "operations must be a non-empty array"}, nil
	}
	view := normalizeProgressiveView(getStringArg(args, "view"))

	ops, issues, notes := validateActOperations(rawOps, false)
	if len(issues) > 0 {
		return t.scrubSecrets(rejectedActBatch(len(ops), issues, notes, view)), nil
	}

	stopOnError := getBoolArg(args, "stop_on_error", true)
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}
	settle := defaultSettle
	if ms, ok := numberValue(args["settle_ms"]); ok {
		settle = time.Duration(ms) * time.Millisecond
		if settle > 10*time.Second {
			settle = 10 * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(ctx, maxActBatchDuration)
	defer cancel()

	run := &actRun{tool: t, session: sessionID, touched: map[string]bool{}}
	if sessionID != "" {
		run.touched[sessionID] = true
	}
	moved := unattendedMove(t.sessions, sessionID)
	base := captureActBaseline(ctx, t.sessions, sessionID)
	batchStart := time.Now()
	lastOp := batchStart
	pageTouched := false

	results := make([]map[string]interface{}, 0, len(ops))
	succeeded, failed := 0, 0
	firstFailure := ""

	for idx, op := range ops {
		opType := getStringFromMap(op, "type")
		entry := map[string]interface{}{"index": idx, "type": opType}
		if err := ctx.Err(); err != nil {
			entry["success"] = false
			entry["error"] = fmt.Sprintf("batch stopped before this op: %v", err)
			results = append(results, entry)
			failed++
			if firstFailure == "" {
				firstFailure = fmt.Sprintf("op %d %s: %s", idx, opType, entry["error"])
			}
			break
		}

		opStart := time.Now()
		opResult, err := run.execute(ctx, op)
		lastOp = time.Now()
		pageTouched = pageTouched || actPageOps[opType]

		success, errMsg := actOpOutcome(opResult, err)
		entry["success"] = success
		if !success {
			entry["error"] = errMsg
		}
		entry["result"] = opResult
		liftActFields(entry, opType, opResult)
		if success {
			run.follow(opType, entry)
		}
		if run.session != "" && t.sessions != nil {
			dialogCtx, cancelDialogs := context.WithTimeout(ctx, time.Second)
			dialogErr := t.sessions.AwaitDialogReports(dialogCtx, run.session, opStart)
			cancelDialogs()
			if dialogErr != nil {
				warning := fmt.Sprintf("dialog reporting incomplete: %v", dialogErr)
				if previous, ok := entry["warning"]; ok {
					warning = fmt.Sprintf("%v; %s", previous, warning)
				}
				entry["warning"] = warning
			}
			if dialogs := t.sessions.DialogsSince(run.session, opStart); len(dialogs) > 0 {
				described := make([]string, 0, len(dialogs))
				for _, d := range dialogs {
					described = append(described, describeDialog(d.Type, d.Message, d.Accepted))
				}
				entry["dialogs"] = described
			}
		}

		results = append(results, entry)
		if success {
			succeeded++
			continue
		}
		failed++
		if firstFailure == "" {
			firstFailure = fmt.Sprintf("op %d %s: %s", idx, opType, errMsg)
		}
		// A failed switch cannot let later page operations fall back to the
		// previous tab, even in a best-effort batch.
		if stopOnError || opType == "session_create" || opType == "session_attach" || opType == "session_fork" || opType == "session_focus" {
			break
		}
	}
	run.finish()

	var effects map[string]interface{}
	if len(run.touched) > 0 && len(results) > 0 {
		w := effectsWindow{since: batchStart, sessions: run.touched}
		if pageTouched {
			settleEffects(ctx, t.engine, w, lastOp, settle)
			drainTouchedPages(ctx, t.sessions, run.touched)
		}
		effects = collectActEffects(ctx, t.engine, t.sessions, base, run.session, w)
	}
	for id := range run.touched {
		markAttended(t.sessions, id)
	}

	now := time.Now().UnixMilli()
	handle := fmt.Sprintf("act:%s:%d", run.session, now)
	emitDisclosureFacts(ctx, t.engine, run.session, []string{handle}, "act")

	skipped := len(ops) - len(results)
	summary := fmt.Sprintf("Executed %d operation(s): %d succeeded, %d failed", len(results), succeeded, failed)
	if skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	if firstFailure != "" {
		summary += "; first failure " + firstFailure
	}
	if moved != nil {
		// Said in every view: an agent that believes the tab is where its last
		// call left it acts on the wrong page.
		summary = fmt.Sprintf("Before this call the tab moved %v -> %v on its own. %s", moved["from"], moved["to"], summary)
	}
	counts := map[string]interface{}{"total": len(ops), "succeeded": succeeded, "failed": failed}
	if skipped > 0 {
		counts["skipped"] = skipped
	}

	response := map[string]interface{}{
		"success":          failed == 0,
		"status":           ternaryStatus(failed == 0, "ok", "error"),
		"summary":          summary,
		"counts":           counts,
		"evidence_handles": []string{handle},
		"view":             view,
	}
	if run.session != "" {
		response["session_id"] = run.session
	}
	if len(notes) > 0 {
		response["normalized"] = notes
	}
	if moved != nil {
		response["tab_moved"] = moved
	}

	switch view {
	case "summary":
		if line := effectsSummary(effects); line != "" {
			response["summary"] = summary + ". Effects: " + line
		}
	case "compact":
		compact := make([]map[string]interface{}, 0, len(results))
		for _, r := range results {
			entry := map[string]interface{}{"index": r["index"], "type": r["type"], "success": r["success"]}
			if r["success"] != true {
				entry["error"] = r["error"]
			}
			for _, key := range actCompactKeys {
				if v, ok := r[key]; ok {
					entry[key] = v
				}
			}
			compact = append(compact, entry)
		}
		response["results"] = limitMapSlice(compact, maxItems)
		if len(compact) > maxItems {
			response["truncated"] = true
		}
	default:
		response["results"] = results
	}
	if view != "summary" && len(effects) > 0 {
		response["effects"] = effects
	}
	if getBoolArg(args, "include_specs", false) {
		if matches := specContextForSession(t.specsCfg, t.sessions, run.session, specTermsFromRaw(args["spec_terms"])); len(matches) > 0 {
			response["spec_context"] = matches
			response["evidence_handles"] = append(response["evidence_handles"].([]string), "act:"+run.session+":spec_context")
		}
	}

	return t.scrubSecrets(response), nil
}

// rejectedActBatch reports a batch that failed validation; nothing ran.
func rejectedActBatch(total int, issues []actOpIssue, notes []string, view string) map[string]interface{} {
	first := issues[0]
	summary := fmt.Sprintf("Rejected the batch before running anything (%d problem(s)); op %d %s: %s", len(issues), first.Index, first.Type, first.Message)
	response := map[string]interface{}{
		"success": false,
		"status":  "invalid",
		"summary": summary,
		"error":   fmt.Sprintf("op %d %s: %s", first.Index, first.Type, first.Message),
		"issues":  issues,
		"counts":  map[string]interface{}{"total": total, "succeeded": 0, "failed": 0, "skipped": total},
		"view":    view,
	}
	if len(notes) > 0 {
		response["normalized"] = notes
	}
	return response
}

// actOpOutcome reads success and the reason for a failure from an op's
// return, whether the failure came back as an error or inside its result.
func actOpOutcome(result interface{}, err error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		return true, ""
	}
	if s, exists := m["success"].(bool); exists && !s {
		for _, key := range []string{"error", "message", "reason", "status"} {
			if v, present := m[key]; present && v != nil {
				if msg := strings.TrimSpace(fmt.Sprint(v)); msg != "" {
					return false, msg
				}
			}
		}
		return false, "the operation reported failure without a reason"
	}
	return true, ""
}

// liftActFields copies the high-signal parts of an op's result onto its entry.
func liftActFields(entry map[string]interface{}, opType string, result interface{}) {
	m, ok := result.(map[string]interface{})
	if !ok {
		return
	}
	switch opType {
	case "session_create", "session_fork", "session_attach":
		if sess, ok := m["session"].(*browser.Session); ok && sess != nil {
			entry["session_id"] = sess.ID
			entry["browser_id"] = sess.BrowserID
			entry["isolated"] = sess.Isolated
			entry["isolated_display"] = sess.IsolatedDisplay
			if sess.URL != "" {
				entry["url"] = sess.URL
			}
			if sess.Title != "" {
				entry["title"] = sess.Title
			}
		}
	case "browser_launch":
		if instance, ok := m["browser"].(*browser.BrowserInstance); ok && instance != nil {
			entry["browser_id"] = instance.ID
			entry["managed"] = instance.Managed
			entry["default"] = instance.Default
			entry["isolated_display"] = instance.IsolatedDisplay
		}
	case "navigate", "history":
		if u, ok := m["url"].(string); ok && u != "" {
			entry["url"] = u
		}
		if reloaded, ok := m["reloaded"].(bool); ok && reloaded {
			entry["reloaded"] = true
		}
	}
	for _, key := range []string{"located", "selected", "verified_by", "armed", "warning", "note"} {
		if v, ok := m[key]; ok && v != nil && v != "" {
			entry[key] = v
		}
	}
}

// scrubSecrets removes every value_env value from a result before it leaves.
func (t *BrowserActTool) scrubSecrets(response map[string]interface{}) map[string]interface{} {
	if len(t.secrets) == 0 {
		return response
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return map[string]interface{}{"success": false, "error": "result withheld: it could not be checked for secrets"}
	}
	text := string(raw)
	for secret := range t.secrets {
		if len(secret) < 4 {
			continue
		}
		encoded, _ := json.Marshal(secret)
		inner := strings.Trim(string(encoded), `"`)
		text = strings.ReplaceAll(text, inner, security.Redacted)
	}
	var scrubbed map[string]interface{}
	if err := json.Unmarshal([]byte(text), &scrubbed); err != nil {
		return map[string]interface{}{"success": false, "error": "result withheld: it could not be checked for secrets"}
	}
	return scrubbed
}

// actRun is the state one batch carries between its operations.
type actRun struct {
	tool    *BrowserActTool
	session string
	touched map[string]bool
	armed   bool
}

// follow makes a created, forked, attached or focused session the batch's
// session for the operations after it.
func (r *actRun) follow(opType string, entry map[string]interface{}) {
	switch opType {
	case "session_create", "session_fork", "session_attach":
		if id, ok := entry["session_id"].(string); ok && id != "" {
			r.session = id
			r.touched[id] = true
		}
	case "session_focus":
		if result, ok := entry["result"].(map[string]interface{}); ok {
			if id, ok := result["session_id"].(string); ok && id != "" {
				r.session = id
				r.touched[id] = true
			}
		}
	}
}

// finish drops a dialog answer a dialog op armed and no dialog used.
func (r *actRun) finish() {
	if r.armed && r.session != "" && r.tool.sessions != nil {
		r.tool.sessions.DisarmDialog(r.session)
	}
}

// execute runs one operation within its time bound, answering a dialog it
// opens when the op carries dialog: accept|dismiss.
func (r *actRun) execute(ctx context.Context, op map[string]interface{}) (interface{}, error) {
	opType := getStringFromMap(op, "type")
	if answer := getStringFromMap(op, "dialog"); answer != "" && opType != "dialog" {
		if r.session == "" || r.tool.sessions == nil {
			return nil, fmt.Errorf("dialog needs a session")
		}
		r.tool.sessions.ArmDialog(r.session, answer == "accept", getStringFromMap(op, "prompt_text"))
		defer r.tool.sessions.DisarmDialog(r.session)
	}
	if opType == "dialog" {
		r.armed = true
	}
	session := r.session
	return runBounded(ctx, actOpBound(op), opType, func(opCtx context.Context) (interface{}, error) {
		return r.dispatch(opCtx, session, op)
	})
}

// actOpBound is the longest one operation may take.
func actOpBound(op map[string]interface{}) time.Duration {
	timeout := time.Duration(getIntArg(op, "timeout_ms", 0)) * time.Millisecond
	switch getStringFromMap(op, "type") {
	case "sleep":
		return time.Duration(getIntArg(op, "duration_ms", 0))*time.Millisecond + 2*time.Second
	case "wait", "await_stable", "await_fact", "await_conditions":
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		return timeout + 2*time.Second
	case "navigate", "history":
		if timeout == 0 {
			timeout = defaultNavigateTimeout
		}
		return timeout + 2*time.Second
	case "js":
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		return timeout + 2*time.Second
	case "plan":
		return 2 * time.Minute
	case "session_create", "session_fork", "session_attach", "session_close", "browser_launch", "browser_close":
		return time.Minute
	}
	if timeout > 0 {
		return timeout
	}
	return 30 * time.Second
}

// runBounded runs fn with a deadline and returns when it does, or when the
// deadline passes even if fn is stuck inside a browser call.
func runBounded(ctx context.Context, bound time.Duration, label string, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	opCtx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	type outcome struct {
		result interface{}
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- outcome{err: fmt.Errorf("%s failed: %v", label, p)}
			}
		}()
		result, err := fn(opCtx)
		done <- outcome{result, err}
	}()
	timedOut := func() error {
		if ctx.Err() != nil {
			return fmt.Errorf("%s stopped: %v", label, ctx.Err())
		}
		return fmt.Errorf("%s did not finish within %s; the page may be blocked by a JavaScript dialog or a busy renderer", label, bound)
	}
	select {
	case o := <-done:
		if o.err != nil && errors.Is(o.err, context.DeadlineExceeded) {
			return o.result, timedOut()
		}
		return o.result, o.err
	case <-opCtx.Done():
		return nil, timedOut()
	}
}

// dispatch runs one validated operation against session.
func (r *actRun) dispatch(ctx context.Context, sessionID string, op map[string]interface{}) (interface{}, error) {
	t := r.tool
	withSession := func() map[string]interface{} {
		out := make(map[string]interface{}, len(op)+1)
		for k, v := range op {
			out[k] = v
		}
		out["session_id"] = sessionID
		return out
	}
	passThrough := func(keys ...string) map[string]interface{} {
		out := map[string]interface{}{}
		for _, key := range keys {
			if v, ok := op[key]; ok {
				out[key] = v
			}
		}
		return out
	}

	switch getStringFromMap(op, "type") {
	case "navigate":
		return (&NavigateURLTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, withSession())
	case "interact":
		return (&InteractTool{sessions: t.sessions, engine: t.engine, redactor: t.redactor, secrets: t.secrets}).Execute(ctx, withSession())
	case "fill":
		return (&FillFormTool{sessions: t.sessions, engine: t.engine, redactor: t.redactor, secrets: t.secrets}).Execute(ctx, withSession())
	case "upload":
		return uploadFiles(ctx, t.sessions, t.engine, t.pathPolicy, sessionID, getStringFromMap(op, "ref"), op["files"])
	case "key":
		return (&PressKeyTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, withSession())
	case "history":
		return (&BrowserHistoryTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, withSession())
	case "sleep":
		ms := getIntArg(op, "duration_ms", 0)
		err := sleepWithContext(ctx, time.Duration(ms)*time.Millisecond)
		return map[string]interface{}{"success": err == nil, "slept_ms": ms}, err
	case "dialog":
		if sessionID == "" || t.sessions == nil {
			return nil, fmt.Errorf("dialog needs a session_id")
		}
		accept := getBoolArg(op, "accept", false)
		t.sessions.ArmDialog(sessionID, accept, getStringFromMap(op, "prompt_text"))
		return map[string]interface{}{"success": true, "armed": ternaryStatus(accept, "accept", "dismiss")}, nil
	case "session_create":
		return (&CreateSessionTool{sessions: t.sessions}).Execute(ctx, map[string]interface{}{
			"url":        getStringFromMap(op, "url"),
			"browser_id": getStringFromMap(op, "browser_id"),
			"isolated":   getBoolArg(op, "isolated", false),
		})
	case "session_attach":
		return (&AttachSessionTool{sessions: t.sessions}).Execute(ctx, passThrough("target_id", "browser_id"))
	case "session_fork":
		forkArgs := map[string]interface{}{"session_id": firstNonEmpty(getStringFromMap(op, "source_session_id"), sessionID)}
		if u := getStringFromMap(op, "url"); u != "" {
			forkArgs["url"] = u
		}
		return (&ForkSessionTool{sessions: t.sessions}).Execute(ctx, forkArgs)
	case "session_focus":
		return (&FocusSessionTool{sessions: t.sessions}).Execute(ctx, map[string]interface{}{
			"session_id": firstNonEmpty(getStringFromMap(op, "session_id"), sessionID),
		})
	case "session_close":
		return (&CloseSessionTool{sessions: t.sessions}).Execute(ctx, map[string]interface{}{
			"session_id": firstNonEmpty(getStringFromMap(op, "session_id"), sessionID),
		})
	case "browser_launch":
		return (&LaunchBrowserTool{sessions: t.sessions}).Execute(ctx, map[string]interface{}{
			"new_instance": getBoolArg(op, "new_instance", true),
		})
	case "browser_close":
		return (&ShutdownBrowserTool{sessions: t.sessions}).Execute(ctx, passThrough("browser_id"))
	case "wait":
		return (&WaitForConditionTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, passThrough("predicate", "match_args", "timeout_ms", "poll_interval_ms"))
	case "await_stable":
		stableArgs := passThrough("timeout_ms", "network_idle_ms")
		stableArgs["session_id"] = sessionID
		return (&AwaitStableStateTool{engine: t.engine}).Execute(ctx, stableArgs)
	case "await_fact":
		return (&AwaitFactTool{engine: t.engine}).Execute(ctx, passThrough("predicate", "args", "timeout_ms"))
	case "await_conditions":
		return (&AwaitConditionsTool{engine: t.engine}).Execute(ctx, passThrough("conditions", "timeout_ms"))
	case "js":
		jsArgs := passThrough("script", "element_ref", "timeout_ms", "gate_reason", "approved_by_handle", "result_mode", "max_result_bytes")
		jsArgs["session_id"] = sessionID
		return (&EvaluateJSTool{
			sessions:                t.sessions,
			engine:                  t.engine,
			redactor:                t.redactor,
			disableUnsafeJavaScript: t.disableUnsafeJavaScript,
		}).Execute(ctx, jsArgs)
	case "plan":
		planArgs := passThrough("actions", "predicate", "delay_ms")
		planArgs["session_id"] = sessionID
		return (&ExecutePlanTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, planArgs)
	case "viewport":
		return runViewportOp(ctx, t.sessions, t.engine, sessionID, op)
	}
	return nil, fmt.Errorf("unknown operation type: %s", getStringFromMap(op, "type"))
}
