package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/mangle"
)

func addScopeSuccessToast(probe *probeFacts, timestamp int64) {
	probe.add("toast_notification", timestamp, "Saved successfully", "success", "native", probe.at(timestamp).UnixMilli())
}

func requireScopeContradiction(t *testing.T, evidence *browserEvidence, failedRequests, successToasts int) {
	t.Helper()
	actualFailures := 0
	for _, request := range evidence.calls {
		if request.failed() {
			actualFailures++
		}
	}
	actualSuccesses := 0
	for _, toast := range evidence.toasts {
		if toast.level == "success" {
			actualSuccesses++
		}
	}
	if actualFailures != failedRequests || actualSuccesses != successToasts {
		t.Fatalf("scoped evidence failures/toasts = %d/%d, want %d/%d", actualFailures, actualSuccesses, failedRequests, successToasts)
	}
	if failedRequests == 0 || successToasts == 0 {
		if len(evidence.contradictions) != 0 {
			t.Fatalf("no coincident failure and success toast: %v", evidence.contradictions)
		}
		return
	}
	if len(evidence.contradictions) != 1 {
		t.Fatalf("want one contradiction, got %v", evidence.contradictions)
	}
	contradiction := evidence.contradictions[0]
	if contradiction["type"] != "success_toast_with_failed_requests" ||
		asInt(contradiction["failed_request_count"]) != failedRequests ||
		asInt(contradiction["success_toast_count"]) != successToasts ||
		contradiction["confidence_impact_delta"] != -0.25 {
		t.Fatalf("incorrect scoped contradiction: %v", contradiction)
	}
}

func TestReasonContradictionCollectorScope(t *testing.T) {
	testCases := []struct {
		name         string
		populate     func(*probeFacts)
		unbounded    bool
		nonStrict    bool
		failedCount  int
		successCount int
	}{
		{
			name: "historical_failure_fresh_toast",
			populate: func(probe *probeFacts) {
				probe.request("old", "POST", probeOrigin+"/api/save", 1000, 500, 5)
				addScopeSuccessToast(probe, 4000)
			},
			successCount: 1,
		},
		{
			name: "fresh_failure_historical_toast",
			populate: func(probe *probeFacts) {
				probe.request("fresh", "POST", probeOrigin+"/api/save", 4000, 500, 5)
				addScopeSuccessToast(probe, 1000)
			},
			failedCount: 1,
		},
		{
			name: "both_fresh",
			populate: func(probe *probeFacts) {
				probe.request("fresh", "POST", probeOrigin+"/api/save", 4000, 500, 5)
				addScopeSuccessToast(probe, 4010)
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "inclusive_boundary",
			populate: func(probe *probeFacts) {
				probe.request("boundary", "POST", probeOrigin+"/api/save", 3000, 422, 5)
				addScopeSuccessToast(probe, 3000)
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "request_started_before_boundary_failed_after",
			populate: func(probe *probeFacts) {
				probe.request("prior", "POST", probeOrigin+"/api/save", 1000, 500, 3100)
				addScopeSuccessToast(probe, 4200)
			},
			successCount: 1,
		},
		{
			name: "all_history_retains_legitimate_contradiction",
			populate: func(probe *probeFacts) {
				probe.request("old", "POST", probeOrigin+"/api/save", 1000, 500, 5)
				addScopeSuccessToast(probe, 4000)
			},
			unbounded: true, failedCount: 1, successCount: 1,
		},
		{
			name: "canceled_network_request_is_not_failure",
			populate: func(probe *probeFacts) {
				probe.request("cancel", "GET", probeOrigin+"/api/search", 4000, 0, 0)
				probe.add("net_loading_failed", 4005, "cancel", "net::ERR_ABORTED", true, probe.at(4005).UnixMilli())
				addScopeSuccessToast(probe, 4010)
			},
			successCount: 1,
		},
		{
			name: "canceled_request_with_http_error_still_failed",
			populate: func(probe *probeFacts) {
				probe.request("cancel", "GET", probeOrigin+"/api/search", 4000, 500, 5)
				probe.add("net_loading_failed", 4006, "cancel", "net::ERR_ABORTED", true, probe.at(4006).UnixMilli())
				addScopeSuccessToast(probe, 4010)
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "uncanceled_network_error_without_response",
			populate: func(probe *probeFacts) {
				probe.request("refused", "GET", probeOrigin+"/api/save", 4000, 0, 0)
				probe.add("net_loading_failed", 4005, "refused", "net::ERR_CONNECTION_REFUSED", false, probe.at(4005).UnixMilli())
				addScopeSuccessToast(probe, 4010)
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "body_failure_after_success_response",
			populate: func(probe *probeFacts) {
				probe.request("stream", "GET", probeOrigin+"/api/stream", 4000, 200, 5)
				probe.add("net_loading_failed", 4006, "stream", "net::ERR_HTTP2_PROTOCOL_ERROR", false, probe.at(4006).UnixMilli())
				addScopeSuccessToast(probe, 4010)
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "successful_request_and_toast",
			populate: func(probe *probeFacts) {
				probe.request("saved", "POST", probeOrigin+"/api/save", 4000, 200, 5)
				addScopeSuccessToast(probe, 4010)
			},
			successCount: 1,
		},
		{
			name: "warning_toast_not_success",
			populate: func(probe *probeFacts) {
				probe.request("failed", "POST", probeOrigin+"/api/save", 4000, 422, 5)
				probe.add("toast_notification", 4010, "Validation warning", "warning", "native", probe.at(4010).UnixMilli())
			},
			failedCount: 1,
		},
		{
			name:         "untimestamped_derived_failure_strict",
			populate:     addUntimestampedScopeFailure,
			successCount: 1,
		},
		{
			name:      "untimestamped_derived_failure_non_strict",
			populate:  addUntimestampedScopeFailure,
			nonStrict: true, failedCount: 1, successCount: 1,
		},
		{
			name:      "untimestamped_derived_failure_all_history",
			populate:  addUntimestampedScopeFailure,
			unbounded: true, failedCount: 1, successCount: 1,
		},
		{
			name:        "untimestamped_toast_strict",
			populate:    addUntimestampedScopeToast,
			failedCount: 1,
		},
		{
			name:      "untimestamped_toast_non_strict",
			populate:  addUntimestampedScopeToast,
			nonStrict: true, failedCount: 1, successCount: 1,
		},
		{
			name: "toast_timestamp_buffer_fallback_and_normalization",
			populate: func(probe *probeFacts) {
				probe.request("failed", "POST", probeOrigin+"/api/save", 4000, 500, 5)
				probe.add("toast_notification", 4010, "Saved successfully", " SUCCESS ", "native", int64(0))
			},
			failedCount: 1, successCount: 1,
		},
		{
			name: "other_session_failures_and_toasts_isolated",
			populate: func(probe *probeFacts) {
				probe.request("shared-id", "POST", probeOrigin+"/api/save", 4000, 200, 5)
				addScopeSuccessToast(probe, 4010)
				other := newProbeFacts("another-session")
				other.base = probe.base
				other.request("shared-id", "POST", probeOrigin+"/api/save", 4000, 500, 5)
				addScopeSuccessToast(other, 4010)
				probe.facts = append(probe.facts, other.facts...)
			},
			successCount: 1,
		},
		{
			name: "duplicate_raw_and_derived_failures_deduplicated",
			populate: func(probe *probeFacts) {
				probe.request("same", "POST", probeOrigin+"/api/save", 4000, 500, 5)
				probe.add("failed_request", 4005, "same", probeOrigin+"/api/save", 502)
				probe.add("failed_request_at", 4005, "same", probeOrigin+"/api/save", 502, probe.at(4000).UnixMilli())
				probe.add("failed_request", 4005, "derived-only", probeOrigin+"/api/other", 422)
				probe.add("failed_request_at", 4005, "derived-only", probeOrigin+"/api/other", 422, probe.at(4000).UnixMilli())
				addScopeSuccessToast(probe, 4010)
				addScopeSuccessToast(probe, 4020)
			},
			failedCount: 2, successCount: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := testMangleEngineForProgressive(t)
			probe := newProbeFacts("contradiction-scope")
			testCase.populate(probe)
			loadFacts(t, engine, probe)
			if strings.HasPrefix(testCase.name, "duplicate_") {
				rows, err := engine.Query(context.Background(), fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", probe.sid))
				if err != nil || len(rows) < 3 {
					t.Fatalf("fixture must expose duplicate derived failures before joining: rows=%v err=%v", rows, err)
				}
			}
			scope := reasonScope{sessionID: probe.sid, sinceMs: probe.at(3000).UnixMilli(), strict: !testCase.nonStrict}
			if testCase.unbounded {
				scope.sinceMs = 0
			}
			evidence := collectBrowserEvidence(context.Background(), engine, scope)
			requireScopeContradiction(t, evidence, testCase.failedCount, testCase.successCount)
		})
	}
}

func addUntimestampedScopeFailure(probe *probeFacts) {
	probe.facts = append(probe.facts, mangle.Fact{
		Predicate: "failed_request",
		Args:      []interface{}{probe.sid, "unknown-time", probeOrigin + "/api/save", 500},
	})
	addScopeSuccessToast(probe, 4010)
}

func addUntimestampedScopeToast(probe *probeFacts) {
	probe.request("fresh", "POST", probeOrigin+"/api/save", 4000, 500, 5)
	probe.facts = append(probe.facts, mangle.Fact{
		Predicate: "toast_notification",
		Args:      []interface{}{probe.sid, "Saved successfully", "success", "native", int64(0)},
	})
}

func TestReasonContradictionHealthMatchesActionScope(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		failureAt         int64
		toastAt           int64
		sinceAction       bool
		wantFailure       int
		wantContradiction bool
	}{
		{name: "old_failure_fresh_toast", failureAt: 1000, toastAt: 4010, sinceAction: true},
		{name: "fresh_failure_old_toast", failureAt: 4000, toastAt: 1000, sinceAction: true, wantFailure: 1},
		{name: "fresh_both", failureAt: 4000, toastAt: 4010, sinceAction: true, wantFailure: 1, wantContradiction: true},
		{name: "all_history", failureAt: 1000, toastAt: 4010, wantFailure: 1, wantContradiction: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			engine := testMangleEngineForProgressive(t)
			probe := newProbeFacts("health-contradiction")
			probe.request("failed", "POST", probeOrigin+"/api/save", testCase.failureAt, 500, 5)
			probe.add("click_event", 3000, "save-button", probe.at(3000).UnixMilli())
			addScopeSuccessToast(probe, testCase.toastAt)
			loadFacts(t, engine, probe)
			result := runReason(t, &BrowserReasonTool{engine: engine}, map[string]interface{}{
				"session_id": probe.sid, "topic": "health", "view": "full",
				"since_action": testCase.sinceAction, "time_window_ms": 0,
				"expand_handles": []interface{}{"reason:contradictions"},
			})
			score := asMap(result["scorecard"])
			if asInt(score["failed_5xx"]) != testCase.wantFailure || asInt(score["failed_4xx"]) != 0 || asInt(score["failed_network"]) != 0 {
				t.Fatalf("health score must match scoped failures: %v", score)
			}
			contradictions := toMapSlice(asMap(result["data"])["contradictions"])
			if (len(contradictions) > 0) != testCase.wantContradiction {
				t.Fatalf("expanded contradictions disagree with scope: %v", result)
			}
			if strings.Contains(fmt.Sprintf("%v", result["issues"]), "success toast shown while") != testCase.wantContradiction {
				t.Fatalf("health issues disagree with scoped contradictions: %v", result["issues"])
			}
			if testCase.wantFailure == 0 {
				if result["status"] != "ok" || result["confidence"] != 0.95 {
					t.Fatalf("historical failures must not poison current health or confidence: %v", result)
				}
			} else if result["status"] != "error" {
				t.Fatalf("genuine scoped failure must remain an error: %v", result)
			}
			if testCase.sinceAction && asInt64(result["action_since_ms"]) != probe.at(3000).UnixMilli()-actionScopeSlackMs {
				t.Fatalf("actual browser-reason action cutoff changed: %v", result["action_since_ms"])
			}
		})
	}
}

func TestReasonContradictionNilEvidence(t *testing.T) {
	for _, evidence := range []*browserEvidence{nil, {}, {calls: []*netCall{nil}, toasts: []toastRecord{{level: "success"}}}} {
		if contradictions := detectContradictions(evidence); len(contradictions) != 0 {
			t.Fatalf("no failed request must mean no contradiction: %v", contradictions)
		}
	}
}
