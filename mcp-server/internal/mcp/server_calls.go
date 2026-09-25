package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// callTracker remembers the calls in flight and the recent finished ones, so
// the doctor can name a wedged tab ("browser-act on session X, running 312 s")
// instead of an agent discovering it by waiting out a transport timeout.
type callTracker struct {
	mu       sync.Mutex
	nextID   uint64
	inflight map[uint64]*callRecord
	recent   []callRecord
}

type callRecord struct {
	ID        uint64
	Tool      string
	SessionID string
	Detail    string
	Started   time.Time
	Duration  time.Duration
	OK        bool
	TimedOut  bool
	Error     string
}

const maxRecentCalls = 64

func newCallTracker() *callTracker {
	return &callTracker{inflight: make(map[uint64]*callRecord)}
}

func (c *callTracker) begin(tool, sessionID, detail string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.inflight[c.nextID] = &callRecord{ID: c.nextID, Tool: tool, SessionID: sessionID, Detail: detail, Started: time.Now()}
	return c.nextID
}

// timedOut marks a call whose caller has been answered with a timeout while
// its work is still running. It stays in flight until the work returns.
func (c *callTracker) timedOut(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rec, ok := c.inflight[id]; ok {
		rec.TimedOut = true
	}
}

func (c *callTracker) end(id uint64, ok bool, errText string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, found := c.inflight[id]
	if !found {
		return
	}
	delete(c.inflight, id)
	rec.Duration = time.Since(rec.Started)
	rec.OK = ok
	rec.Error = clipCallText(errText, 200)
	c.recent = append(c.recent, *rec)
	if len(c.recent) > maxRecentCalls {
		c.recent = c.recent[len(c.recent)-maxRecentCalls:]
	}
}

// report summarizes calls for the doctor: every call still running, the most
// recent failures and timeouts, and the slowest recent calls.
func (c *callTracker) report() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()

	inflight := make([]map[string]interface{}, 0, len(c.inflight))
	for _, rec := range c.inflight {
		row := map[string]interface{}{
			"tool":           rec.Tool,
			"running_for_ms": now.Sub(rec.Started).Milliseconds(),
		}
		if rec.SessionID != "" {
			row["session_id"] = rec.SessionID
		}
		if rec.Detail != "" {
			row["detail"] = rec.Detail
		}
		if rec.TimedOut {
			row["timed_out"] = true
		}
		inflight = append(inflight, row)
	}
	sort.Slice(inflight, func(i, j int) bool {
		return asInt64(inflight[i]["running_for_ms"]) > asInt64(inflight[j]["running_for_ms"])
	})

	failures := make([]map[string]interface{}, 0)
	for i := len(c.recent) - 1; i >= 0 && len(failures) < 5; i-- {
		rec := c.recent[i]
		if rec.OK && !rec.TimedOut {
			continue
		}
		failures = append(failures, trackedCallRow(rec, now))
	}

	slowest := append([]callRecord(nil), c.recent...)
	sort.Slice(slowest, func(i, j int) bool { return slowest[i].Duration > slowest[j].Duration })
	slow := make([]map[string]interface{}, 0, 3)
	for _, rec := range slowest {
		if len(slow) == 3 || rec.Duration < 5*time.Second {
			break
		}
		slow = append(slow, trackedCallRow(rec, now))
	}

	out := map[string]interface{}{
		"in_flight":    inflight,
		"recent_count": len(c.recent),
	}
	if len(failures) > 0 {
		out["recent_failures"] = failures
	}
	if len(slow) > 0 {
		out["slowest"] = slow
	}
	return out
}

func trackedCallRow(rec callRecord, now time.Time) map[string]interface{} {
	row := map[string]interface{}{
		"tool":        rec.Tool,
		"duration_ms": rec.Duration.Milliseconds(),
		"ago_s":       int(now.Sub(rec.Started.Add(rec.Duration)).Seconds()),
	}
	if rec.SessionID != "" {
		row["session_id"] = rec.SessionID
	}
	if rec.Detail != "" {
		row["detail"] = rec.Detail
	}
	if rec.TimedOut {
		row["timed_out"] = true
	}
	if rec.Error != "" {
		row["error"] = rec.Error
	}
	return row
}

type toolOutcome struct {
	result interface{}
	err    error
}

// executeWithDeadline runs tool under timeout. When the deadline passes first
// it returns timedOut and leaves the work running: a CDP call blocked on a
// wedged renderer cannot be interrupted, but its caller no longer has to wait
// out a transport timeout to learn that. A panic in the tool becomes an error.
func (s *Server) executeWithDeadline(ctx context.Context, tool Tool, args map[string]interface{}, timeout time.Duration) (interface{}, error, bool) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	done := make(chan toolOutcome, 1)
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				done <- toolOutcome{err: fmt.Errorf("panic in %s: %v", tool.Name(), r)}
			}
		}()
		res, err := tool.Execute(callCtx, args)
		done <- toolOutcome{result: res, err: err}
	}()
	select {
	case out := <-done:
		return out.result, out.err, false
	case <-callCtx.Done():
		// The work may have finished in the same instant; prefer its answer.
		select {
		case out := <-done:
			return out.result, out.err, false
		default:
		}
		if ctx.Err() != nil {
			return nil, ctx.Err(), false
		}
		return nil, nil, true
	}
}

// timeoutResult is what a caller receives when a call outlives its deadline.
func timeoutResult(tool, detail, sessionID string, timeout time.Duration) map[string]interface{} {
	res := map[string]interface{}{
		"success":   false,
		"status":    "timeout",
		"timed_out": true,
		"error":     fmt.Sprintf("%s did not finish within %s", tool, timeout.Round(time.Second)),
		"next":      "browser-observe mode:doctor shows what is still running; a wedged tab recovers with close-session then browser-act session_create",
	}
	if detail != "" {
		res["detail"] = detail
	}
	if sessionID != "" {
		res["session_id"] = sessionID
	}
	return res
}

// resultReportsFailure reports a tool result that carries success:false.
func resultReportsFailure(result interface{}) bool {
	m, ok := result.(map[string]interface{})
	if !ok {
		return false
	}
	success, present := m["success"].(bool)
	return present && !success
}

func errorText(err error, result interface{}) string {
	if err != nil {
		return err.Error()
	}
	if m, ok := result.(map[string]interface{}); ok {
		if e, ok := m["error"].(string); ok {
			return e
		}
		if s, ok := m["summary"].(string); ok && resultReportsFailure(result) {
			return s
		}
	}
	return ""
}

// callDetail names what a call was asked to do, compactly: the op types of a
// browser-act batch, or the mode/topic/operation/phase of the other tools.
func callDetail(args map[string]interface{}) string {
	if ops, ok := args["operations"].([]interface{}); ok {
		types := make([]string, 0, len(ops))
		for _, raw := range ops {
			if op, ok := raw.(map[string]interface{}); ok {
				types = append(types, fmt.Sprintf("%v", op["type"]))
			}
		}
		return clipCallText(strings.Join(types, ","), 120)
	}
	for _, key := range []string{"mode", "topic", "operation", "phase", "intent"} {
		if v, ok := args[key].(string); ok && v != "" {
			return key + "=" + v
		}
	}
	return ""
}

// declaredWaitBudget sums the waits a call declares for itself - sleep
// durations and timeout_ms values in its operations or test steps - so a call
// that asks to wait three minutes is not cut off by the default deadline.
func declaredWaitBudget(args map[string]interface{}) time.Duration {
	var total int64
	var walk func(v interface{}, depth int)
	walk = func(v interface{}, depth int) {
		if depth > 6 {
			return
		}
		switch x := v.(type) {
		case map[string]interface{}:
			for key, val := range x {
				if key == "duration_ms" || key == "timeout_ms" || key == "ms" {
					if n := asInt64(val); n > 0 {
						total += n
					}
					continue
				}
				walk(val, depth+1)
			}
		case []interface{}:
			for _, item := range x {
				walk(item, depth+1)
			}
		}
	}
	walk(args, 0)
	budget := time.Duration(total) * time.Millisecond
	if budget > 30*time.Minute {
		budget = 30 * time.Minute
	}
	return budget
}

func clipCallText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
