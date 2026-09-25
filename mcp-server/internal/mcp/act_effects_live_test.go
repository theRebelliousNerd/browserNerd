package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLiveSessionCreateEffectsNameWhatBrokeAtLoad proves the digest of a
// batch that opens a tab: the page's load-time 404 and WebSocket failure are
// named by path, while the favicon 404 and a status region's passing
// "loading" text - neither of them something to fix - are not.
func TestLiveSessionCreateEffectsNameWhatBrokeAtLoad(t *testing.T) {
	p := startLiveProbe(t)
	act := &BrowserActTool{sessions: p.manager, engine: p.engine}
	res, err := act.Execute(p.ctx, map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "session_create", "url": p.baseURL + "/index.html"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]interface{})
	if id, _ := out["session_id"].(string); id != "" {
		defer func() { _, _ = p.manager.CloseSession(id) }()
	}
	raw, _ := json.Marshal(out["effects"])
	effects := string(raw)
	t.Logf("session_create: ~%d tokens, effects %s", responseTokens(t, out), effects)

	if !strings.Contains(effects, "GET /api/missing 404") {
		t.Fatalf("the load-time 404 must be named by path: %s", effects)
	}
	if !strings.Contains(effects, "ws://127.0.0.1:1/hmr") {
		t.Fatalf("the failed WebSocket must be named: %s", effects)
	}
	for _, noise := range []string{"favicon", p.baseURL, `"loading"`} {
		if strings.Contains(effects, noise) {
			t.Fatalf("effects carry noise %q: %s", noise, effects)
		}
	}
}
