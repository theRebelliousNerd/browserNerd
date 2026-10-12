package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The page moves while no BrowserNERD call is looking - here a route change
// made straight through the page, as the app redirecting itself or a person
// clicking in the visible window would. The next act says so, once; a tab
// found on /projects after the caller left it on /dashboard was once reported
// with nothing to say it had moved.
func TestLiveActSaysTheTabMovedBetweenCalls(t *testing.T) {
	p := startLiveProbe(t)
	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 1})})

	p.eval(t, p.session, "(history.pushState({}, '', '/projects'), location.pathname)")
	deadline := time.Now().Add(3 * time.Second)
	for len(p.manager.UnattendedNavigations(p.session)) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 1}), "view": "summary"})
	raw, _ := json.Marshal(res)
	moved := asMap(res["tab_moved"])
	if moved["to"] != "/projects" || moved["from"] != "/" {
		t.Fatalf("the act must say the tab moved / -> /projects before it: %s", raw)
	}
	if summary, _ := res["summary"].(string); !strings.Contains(summary, "/projects") || !strings.Contains(summary, "on its own") {
		t.Fatalf("the summary view must say it too: %s", raw)
	}

	again := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 1})})
	if again["tab_moved"] != nil {
		raw, _ = json.Marshal(again)
		t.Fatalf("a move is reported once; the call that reported it has seen the tab: %s", raw)
	}

	// The navigation an act makes itself is its own effect, not a move.
	nav := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "navigate", "url": p.baseURL + "/second.html"})})
	next := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 1})})
	if nav["tab_moved"] != nil || next["tab_moved"] != nil {
		raw, _ = json.Marshal(next)
		t.Fatalf("an act's own navigation is not an unattended move: %s", raw)
	}
}
