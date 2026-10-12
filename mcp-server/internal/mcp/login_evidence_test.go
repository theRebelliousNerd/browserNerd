package mcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func TestLoginEvidenceTimestampBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		submitTime  int64
		requestTime int64
		navTime     int64
		status      int64
		want        bool
	}{
		{"same millisecond", 10000, 10000, 10000, 200, true},
		{"next bucket", 14999, 14999, 15000, 200, true},
		{"request before submit", 10000, 9999, 10001, 200, false},
		{"navigation before submit", 10000, 10000, 9999, 200, false},
		{"navigation too late", 10000, 10000, 15000, 200, false},
		{"backend rejected", 10000, 10000, 10001, 401, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			engine := setupTestEngine(t)
			now := time.Now()
			facts := []mangle.Fact{
				{Predicate: "url_before_submit", Args: []interface{}{testSessionID, "/login", scenario.submitTime}, Timestamp: now},
				{Predicate: "form_submitted", Args: []interface{}{testSessionID, int64(2), scenario.submitTime}, Timestamp: now},
				{Predicate: "net_request", Args: []interface{}{testSessionID, "login", "POST", "/api/auth/login", "fetch", scenario.requestTime}, Timestamp: now},
				{Predicate: "net_response", Args: []interface{}{testSessionID, "login", scenario.status, int64(0), int64(1)}, Timestamp: now},
				{Predicate: "navigation_event", Args: []interface{}{testSessionID, "/dashboard", scenario.navTime}, Timestamp: now},
			}
			if err := engine.AddFacts(context.Background(), facts); err != nil {
				t.Fatal(err)
			}
			rows, err := engine.Query(context.Background(), "login_succeeded(S)")
			if err != nil {
				t.Fatal(err)
			}
			if (len(rows) > 0) != scenario.want {
				t.Fatalf("login success = %v, want %v", rows, scenario.want)
			}
			pending, err := engine.Query(context.Background(), "login_attempt_pending(S,T)")
			if err != nil || len(pending) != 1 {
				t.Fatalf("same-timestamp submission must register an attempt: %v / %v", pending, err)
			}
		})
	}
}

func TestLiveFillSubmissionPrecedesClickSideEffects(t *testing.T) {
	probe := startLiveProbe(t)
	probe.eval(t, probe.session, `(() => {
		document.getElementById('f').onsubmit = (event) => {
			event.preventDefault();
			fetch('/api/poll', {method: 'POST'});
			history.pushState({}, '', '/dashboard');
		};
		return 'ready';
	})()`)
	probe.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{
		"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": "3"}},
		"submit_button": "testid:save-btn",
	})})
	submitted := probe.engine.FactsByPredicate("form_submitted")
	before := probe.engine.FactsByPredicate("url_before_submit")
	if len(submitted) != 1 || len(before) != 1 || submitted[0].Args[2] != before[0].Args[2] {
		t.Fatalf("submission must share its pre-dispatch timestamp: %v / %v", submitted, before)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := probe.engine.Query(probe.ctx, "login_succeeded(S)")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("a successful POST and synchronous navigation did not prove login")
}

func TestLiveToastUrgencyDoesNotImplyError(t *testing.T) {
	probe := startLiveProbe(t)
	probe.eval(t, probe.session, `(() => {
		const messages = [
			['Welcome back. Authentication successful. Redirecting to your workspace.', 'alert', '', ''],
			['Your settings are available.', 'alertdialog', '', ''],
			['Login failed. Invalid credentials.', 'alert', '', ''],
			['Warning: check your settings.', 'status', '', ''],
			['Saved successfully.', 'alert', 'error', ''],
			['Update complete.', 'alert', 'success', 'toast-error']
		];
		for (const [text, role, variant, classes] of messages) {
			const element = document.createElement('div');
			element.textContent = text;
			element.setAttribute('role', role);
			element.setAttribute('aria-live', 'assertive');
			if (variant) element.setAttribute('data-variant', variant);
			element.className = classes;
			document.body.appendChild(element);
		}
		return 'ready';
	})()`)
	if err := probe.manager.DrainPageEvents(probe.ctx, probe.session); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Welcome back. Authentication successful. Redirecting to your workspace.": "success",
		"Your settings are available.":                                            "info",
		"Login failed. Invalid credentials.":                                      "error",
		"Warning: check your settings.":                                           "warning",
		"Saved successfully.":                                                     "error",
		"Update complete.":                                                        "success",
	}
	for _, fact := range probe.engine.FactsByPredicate("toast_notification") {
		text := fmt.Sprint(fact.Args[1])
		if expected, exists := want[text]; exists {
			if got := fmt.Sprint(fact.Args[2]); got != expected {
				t.Errorf("%q severity = %s, want %s", text, got, expected)
			}
			delete(want, text)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing toast evidence: %v", want)
	}
}
