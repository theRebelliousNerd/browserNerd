package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
)

func inputTraceSchema(schema map[string]interface{}) map[string]interface{} {
	props := schema["properties"].(map[string]interface{})
	for key, value := range map[string]interface{}{
		"trace_action":    map[string]interface{}{"type": "string", "enum": []string{"arm", "read", "stop"}},
		"trace_id":        map[string]interface{}{"type": "string", "description": "Trace read/stop"},
		"duration_ms":     map[string]interface{}{"type": "integer", "minimum": 100, "maximum": 30000, "description": "Trace lease (10000)"},
		"max_events":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 256, "description": "Trace events (32)"},
		"max_trace_bytes": map[string]interface{}{"type": "integer", "minimum": 2048, "maximum": 32768, "description": "Trace capture/response bytes (16000)"},
	} {
		props[key] = value
	}
	return schema
}

type inputTraceArgs struct {
	action, id, view string
	root             inspectionOptions
	options          browser.InputTraceOptions
	maxItems         int
}

func parseInputTraceArgs(args map[string]interface{}) (inputTraceArgs, error) {
	o := inputTraceArgs{view: "compact", maxItems: 20, options: browser.InputTraceOptions{DurationMS: 10000, MaxEvents: 32, MaxBytes: 16000}}
	allowed := map[string]bool{"mode": true, "session_id": true, "view": true, "gate_reason": true, "approved_by_handle": true, "trace_action": true, "trace_id": true, "root_ref": true, "root_selector": true, "duration_ms": true, "max_events": true, "max_trace_bytes": true, "max_items": true}
	for name := range args {
		if !allowed[name] {
			return o, fmt.Errorf("unsupported input trace field")
		}
	}
	stringArg := func(name string) (string, error) {
		value, ok := args[name]
		if !ok {
			return "", nil
		}
		s, ok := value.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("input trace string option is invalid")
		}
		return s, nil
	}
	var err error
	if o.action, err = stringArg("trace_action"); err != nil {
		return o, err
	}
	if o.action != "arm" && o.action != "read" && o.action != "stop" {
		return o, fmt.Errorf("trace_action must be arm, read or stop")
	}
	if value, exists := args["view"]; exists {
		v, ok := value.(string)
		if !ok || (v != "summary" && v != "compact" && v != "full") {
			return o, fmt.Errorf("view must be summary, compact or full")
		}
		o.view = v
	}
	integer := func(name string, fallback, min, max int) (int, error) {
		value, exists := args[name]
		if !exists {
			return fallback, nil
		}
		var n float64
		switch x := value.(type) {
		case int:
			n = float64(x)
		case float64:
			n = x
		default:
			return 0, fmt.Errorf("input trace budget must be an integer")
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < float64(min) || n > float64(max) {
			return 0, fmt.Errorf("input trace budget is outside its bounds")
		}
		return int(n), nil
	}
	if o.maxItems, err = integer("max_items", 20, 1, 200); err != nil {
		return o, err
	}
	if o.action == "arm" {
		if _, exists := args["trace_id"]; exists {
			return o, fmt.Errorf("arm does not accept trace_id")
		}
		if o.root.Ref, err = stringArg("root_ref"); err != nil {
			return o, err
		}
		if o.root.Selector, err = stringArg("root_selector"); err != nil {
			return o, err
		}
		if (o.root.Ref == "") == (o.root.Selector == "") || len(o.root.Ref) > 256 || len(o.root.Selector) > 512 {
			return o, fmt.Errorf("arm requires exactly one bounded root_ref or root_selector")
		}
		if o.options.DurationMS, err = integer("duration_ms", 10000, 100, 30000); err != nil {
			return o, err
		}
		if o.options.MaxEvents, err = integer("max_events", 32, 1, 256); err != nil {
			return o, err
		}
		if o.options.MaxBytes, err = integer("max_trace_bytes", 16000, 2048, 32768); err != nil {
			return o, err
		}
	} else {
		for _, name := range []string{"root_ref", "root_selector", "duration_ms", "max_events", "max_trace_bytes"} {
			if _, exists := args[name]; exists {
				return o, fmt.Errorf("read/stop accepts trace_id, view, max_items and authorization (approved_by_handle/gate_reason); omit arm-only roots and budgets")
			}
		}
		if o.id, err = stringArg("trace_id"); err != nil || len(o.id) != 36 {
			return o, fmt.Errorf("read/stop requires the exact arm trace_id")
		}
	}
	return o, nil
}

func (t *BrowserObserveTool) inputTrace(ctx context.Context, args map[string]interface{}) map[string]interface{} {
	failure := func(status, reason string) map[string]interface{} {
		return map[string]interface{}{"success": false, "status": status, "mode": "input_trace", "error": reason, "summary": "Input trace " + status + ": " + reason}
	}
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return failure("error", "session_id is required")
	}
	gate := &EvaluateJSTool{engine: t.engine}
	if ok, _ := gate.evaluateJSGateOpen(sessionID, getStringArg(args, "gate_reason"), getStringArg(args, "approved_by_handle"), "input_trace"); !ok {
		out := failure("gated", "recent same-session browser-reason authorization is required")
		out["gated"] = true
		out["recommended_tool"] = "browser-reason"
		return out
	}
	options, err := parseInputTraceArgs(args)
	if err != nil {
		return failure("error", err.Error())
	}
	if t.sessions == nil {
		return failure("error", "input trace requires a live owned session")
	}
	var trace *browser.InputTrace
	if options.action == "arm" {
		root, rootErr := inspectionRoot(ctx, t.sessions, sessionID, options.root)
		if rootErr != nil {
			return failure("error", "trace target must be one live unique element or a current observed ref")
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = root.Context(releaseCtx).Release()
		}()
		trace, err = t.sessions.ArmInputTrace(ctx, sessionID, root, options.options)
	} else {
		trace, err = t.sessions.InputTrace(sessionID, options.id)
		if err == nil && options.action == "stop" {
			trace.Stop()
		}
	}
	if err != nil {
		return failure("error", err.Error())
	}
	return inputTraceResponse(trace, sessionID, options)
}

func inputTraceResponse(trace *browser.InputTrace, sessionID string, options inputTraceArgs) map[string]interface{} {
	snapshot, closed, cleanup := trace.Snapshot()
	count := len(snapshot.Events)
	if count > options.maxItems {
		count = options.maxItems
	}
	if options.view == "summary" {
		count = 0
	}
	reasons := []string{}
	if !closed {
		if snapshot.Active {
			reasons = append(reasons, "trace_active")
		} else {
			reasons = append(reasons, "cleanup_pending")
		}
	}
	if snapshot.DroppedEvents > 0 || snapshot.DroppedPhases > 0 {
		reasons = append(reasons, "capture_dropped_events_or_phases")
	}
	if snapshot.NodeLimit {
		reasons = append(reasons, "node_limit")
	}
	if snapshot.PathLimit {
		reasons = append(reasons, "path_limit")
	}
	if snapshot.InvalidUpdates > 0 {
		reasons = append(reasons, "invalid_updates")
	}
	if snapshot.FinalConfirmed < snapshot.EventCount {
		reasons = append(reasons, "final_samples_unconfirmed")
	}
	if snapshot.Reason == "navigation" || snapshot.Reason == "disconnected" || snapshot.Reason == "session_closed" {
		reasons = append(reasons, "document_or_transport_ended")
	}
	data := map[string]interface{}{"trace_id": trace.ID, "active": !closed && snapshot.Active, "terminal_reason": snapshot.Reason, "scope": map[string]interface{}{"target_ref": "n0", "nodes": "trace_local", "frames": "top_document", "callback_execution": "not_assessed", "listener_order": "earlier_stop_immediate_may_hide_phases", "propagation_final": "not_observable_after_dispatch"}, "budgets": trace.Options, "retention_ms": browser.InputTraceRetention.Milliseconds(), "event_count": snapshot.EventCount, "phase_count": snapshot.PhaseCount, "final_confirmed": snapshot.FinalConfirmed, "dropped_events": snapshot.DroppedEvents, "dropped_phases": snapshot.DroppedPhases, "invalid_updates": snapshot.InvalidUpdates, "cleanup_confirmed": cleanup, "capture_complete": len(reasons) == 0, "elapsed_ms": snapshot.ElapsedMS}
	out := map[string]interface{}{"success": true, "status": "ok", "mode": "input_trace", "view": options.view, "session_id": sessionID, "data": data}
	data["cleanup_pending"] = !closed && !snapshot.Active
	for {
		responseReasons := append([]string{}, reasons...)
		if count < len(snapshot.Events) {
			responseReasons = append(responseReasons, "response_omits_events")
		}
		data["nodes"], data["events"], data["returned"], data["response_complete"], data["incomplete_reasons"] = snapshot.Nodes, snapshot.Events[:count], count, count == len(snapshot.Events), responseReasons
		data["complete"] = len(responseReasons) == 0
		out["status"] = "ok"
		if len(responseReasons) > 0 {
			out["status"] = "partial"
		}
		out["summary"] = fmt.Sprintf("Input trace: %d event(s), %d phase(s), %d final sample(s); active=%t", snapshot.EventCount, snapshot.PhaseCount, snapshot.FinalConfirmed, !closed && snapshot.Active)
		raw, _ := json.Marshal(out)
		if len(raw) <= trace.Options.MaxBytes {
			return out
		}
		if count == 0 {
			data["nodes"] = []interface{}{}
			data["incomplete_reasons"] = append(responseReasons, "response_byte_limit")
			data["complete"] = false
			data["response_complete"] = false
			out["status"] = "partial"
			return out
		}
		count--
	}
}

func recordInputTraceCall(tool string, args map[string]interface{}) bool {
	return tool != "browser-observe" || strings.ToLower(getStringArg(args, "mode")) != "input_trace"
}
