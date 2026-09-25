package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/security"
)

// wedgedTool ignores its context, like a CDP call blocked on a renderer that a
// JavaScript dialog froze.
type wedgedTool struct{ release chan struct{} }

func (w *wedgedTool) Name() string                        { return "browser-act" }
func (w *wedgedTool) Description() string                 { return "" }
func (w *wedgedTool) InputSchema() map[string]interface{} { return map[string]interface{}{} }
func (w *wedgedTool) Execute(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	<-w.release
	return map[string]interface{}{"success": true}, nil
}

func TestDeadlineAnswersAWedgedCallAndTheDoctorNamesIt(t *testing.T) {
	s := &Server{calls: newCallTracker()}
	tool := &wedgedTool{release: make(chan struct{})}
	args := map[string]interface{}{
		"session_id": "tab-1",
		"operations": []interface{}{map[string]interface{}{"type": "interact"}},
	}

	id := s.calls.begin(tool.Name(), "tab-1", callDetail(args))
	started := time.Now()
	_, err, timedOut := s.executeWithDeadline(context.Background(), tool, args, 150*time.Millisecond)
	if !timedOut || err != nil {
		t.Fatalf("want a timeout, got timedOut=%v err=%v", timedOut, err)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the caller waited %s for a 150ms deadline", waited)
	}
	s.calls.timedOut(id)

	report := DoctorReport(context.Background(), DoctorDeps{Config: config.DefaultConfig(), Calls: s.calls})
	problems, _ := report["problems"].([]string)
	named := false
	for _, p := range problems {
		if strings.Contains(p, "browser-act") && strings.Contains(p, "tab-1") && strings.Contains(p, "close-session") {
			named = true
		}
	}
	if !named {
		t.Fatalf("doctor did not name the wedged call and its recovery: %v", problems)
	}

	// When the work finally returns, the call leaves the in-flight list.
	close(tool.release)
	s.calls.end(id, true, "")
	inflight := s.calls.report()["in_flight"].([]map[string]interface{})
	if len(inflight) != 0 {
		t.Fatalf("finished call still in flight: %v", inflight)
	}
}

func TestDeadlineReturnsTheResultOfAFastCall(t *testing.T) {
	s := &Server{calls: newCallTracker()}
	tool := &wedgedTool{release: make(chan struct{})}
	close(tool.release)
	res, err, timedOut := s.executeWithDeadline(context.Background(), tool, nil, time.Second)
	if timedOut || err != nil || resultReportsFailure(res) {
		t.Fatalf("fast call: res=%v err=%v timedOut=%v", res, err, timedOut)
	}
}

type panickyTool struct{ wedgedTool }

func (p *panickyTool) Execute(context.Context, map[string]interface{}) (interface{}, error) {
	panic("boom")
}

func TestDeadlineTurnsAPanicIntoAnError(t *testing.T) {
	s := &Server{calls: newCallTracker()}
	_, err, timedOut := s.executeWithDeadline(context.Background(), &panickyTool{}, nil, time.Second)
	if timedOut || err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want the panic as an error, got err=%v timedOut=%v", err, timedOut)
	}
}

func TestDeclaredWaitsExtendTheDeadline(t *testing.T) {
	args := map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "sleep", "duration_ms": float64(40000)},
			map[string]interface{}{"type": "await_fact", "timeout_ms": float64(90000)},
		},
		"test": map[string]interface{}{
			"actions": []interface{}{map[string]interface{}{"type": "sleep", "ms": 5000}},
		},
	}
	if got := declaredWaitBudget(args); got != 135*time.Second {
		t.Fatalf("declared wait budget = %s, want 2m15s", got)
	}
	huge := map[string]interface{}{"timeout_ms": float64(10 * time.Hour / time.Millisecond)}
	if got := declaredWaitBudget(huge); got != 30*time.Minute {
		t.Fatalf("budget is not capped: %s", got)
	}
}

func TestDoctorReportsAMissingCredentialVariableWithoutItsValue(t *testing.T) {
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "")
	// Read the report as an agent does: after the server's credential redactor.
	report := throughServerRedactor(t, "browser-observe", DoctorReport(context.Background(), DoctorDeps{Config: config.DefaultConfig()}))
	if !credentialListed(report, "unset", "BROWSERNERD_TEST_PASSWORD") {
		t.Fatalf("credentials: %v", report["credentials"])
	}
	if report["status"] != "degraded" {
		t.Fatalf("an unset credential variable must degrade the report: %v", report["status"])
	}

	t.Setenv("BROWSERNERD_TEST_PASSWORD", "hunter2-very-secret")
	report = throughServerRedactor(t, "browser-observe", DoctorReport(context.Background(), DoctorDeps{Config: config.DefaultConfig()}))
	if !credentialListed(report, "set", "BROWSERNERD_TEST_PASSWORD") {
		t.Fatalf("credentials: %v", report["credentials"])
	}
	if strings.Contains(strings.ToLower(mustJSON(t, report)), "hunter2") {
		t.Fatal("the doctor leaked the credential value")
	}
}

// throughServerRedactor returns result as an agent receives it: marshalled
// and passed through the server's mandatory credential redactor.
func throughServerRedactor(t *testing.T, tool string, result interface{}) map[string]interface{} {
	t.Helper()
	payload := sanitizeToolPayload(marshalToolPayload(tool, result), security.NewRedactor(nil))
	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("redacted payload is not JSON: %v", err)
	}
	return out
}

func credentialListed(report map[string]interface{}, state, name string) bool {
	creds, _ := report["credentials"].(map[string]interface{})
	list, _ := creds[state].([]interface{})
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}
