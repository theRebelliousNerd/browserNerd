package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

// TestFixtureDiagnosisBlamesOnlyWhatItsRunCaused: a 500 already on the page
// before the test started, and another tab's 500, are not the test's
// failures; the 500 its own run triggered is.
func TestFixtureDiagnosisBlamesOnlyWhatItsRunCaused(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	started := time.Now()
	before := started.Add(-30 * time.Second)
	during := started.Add(200 * time.Millisecond)
	failure := func(session, id, url string, at time.Time) []mangle.Fact {
		return []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{session, id, "GET", url, "", at.UnixMilli()}, Timestamp: at},
			{Predicate: "net_response", Args: []interface{}{session, id, 500, 1, 2}, Timestamp: at.Add(2 * time.Millisecond)},
			{Predicate: "net_http_error", Args: []interface{}{session, id, url, 500, "Fetch", at.Add(2 * time.Millisecond).UnixMilli()}, Timestamp: at.Add(2 * time.Millisecond)},
		}
	}
	var facts []mangle.Fact
	facts = append(facts, failure("tab", "old", "http://app/api/old", before)...)
	facts = append(facts, failure("other-tab", "elsewhere", "http://app/api/elsewhere", during)...)
	facts = append(facts, failure("tab", "new", "http://app/api/new", during)...)
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}

	diag := causalDiagnosis(context.Background(), engine, "tab", started.UnixMilli())
	failed, _ := diag["failed_request"].(map[string]interface{})
	if failed == nil || failed["count"] != 1 {
		t.Fatalf("want exactly the run's own failure, got %v", diag["failed_request"])
	}
	if got := fmt.Sprint(failed["samples"]); !strings.Contains(got, "/api/new") || strings.Contains(got, "/api/old") || strings.Contains(got, "/api/elsewhere") {
		t.Fatalf("diagnosis blamed the wrong request: %s", got)
	}

	// With no window (an assertion-only test), the session's history counts.
	all := causalDiagnosis(context.Background(), engine, "tab", 0)
	if f, _ := all["failed_request"].(map[string]interface{}); f == nil || f["count"] != 2 {
		t.Fatalf("unscoped diagnosis should see both of the tab's failures, got %v", all["failed_request"])
	}
}
