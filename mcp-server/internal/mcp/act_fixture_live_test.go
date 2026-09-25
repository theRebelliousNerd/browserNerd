package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func runFixture(t *testing.T, p *liveProbe, fixture map[string]interface{}) map[string]interface{} {
	t.Helper()
	res, err := (&BrowserTestTool{sessions: p.manager, engine: p.engine}).Execute(p.ctx, map[string]interface{}{
		"operation": "run",
		"test":      fixture,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.(map[string]interface{})
}

func TestLiveBrowserTestRunsOperationsFixtures(t *testing.T) {
	p := startLiveProbe(t)
	m := runFixture(t, p, map[string]interface{}{
		"name":       "save quantity",
		"session_id": p.session,
		"operations": []interface{}{
			map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": "9"}}},
			map[string]interface{}{"type": "interact", "action": "click", "text": "Save"},
		},
		"assertions": []interface{}{map[string]interface{}{"name": "clicked", "query": "user_click(S, _, _)", "expect": "present"}},
	})
	if m["passed"] != true || p.status(t) != "saved 9" {
		raw, _ := json.Marshal(m)
		t.Fatalf("an operations fixture must replay its steps: status %q, %s", p.status(t), raw)
	}

	m = runFixture(t, p, map[string]interface{}{
		"session_id": p.session,
		"operations": []interface{}{
			map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Name", "value_env": "BROWSERNERD_UNSET_PROBE_SECRET"}}},
		},
		"assertions": []interface{}{map[string]interface{}{"query": "user_click(S, _, _)"}},
	})
	if m["passed"] == true || !strings.Contains(fmt.Sprint(m["error"]), "BROWSERNERD_UNSET_PROBE_SECRET") {
		t.Fatalf("an unset value_env must fail the run by name: %+v", m)
	}
}

// A value resolved from value_env is typed into the page but never returned:
// here the field truncates it, and the failure that follows must still not
// carry it back.
func TestLiveBrowserTestNeverReturnsAResolvedSecret(t *testing.T) {
	const secret = "probe-secret-4417"
	t.Setenv("BROWSERNERD_PROBE_SECRET", secret)
	p := startLiveProbe(t)
	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "navigate", "url": p.baseURL + "/widgets.html"})})
	m := runFixture(t, p, map[string]interface{}{
		"session_id": p.session,
		"operations": []interface{}{
			map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Code", "value_env": "BROWSERNERD_PROBE_SECRET"}}},
		},
		"assertions": []interface{}{map[string]interface{}{"query": "user_click(S, _, _)", "expect": "absent"}},
	})
	raw, _ := json.Marshal(m)
	if m["passed"] == true {
		t.Fatalf("a field that cannot hold the value must fail the replay: %s", raw)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("the resolved secret leaked into the result: %s", raw)
	}
	t.Logf("replay failure with a secret value (~%d tokens): %s", len(raw)/4, raw)
}
