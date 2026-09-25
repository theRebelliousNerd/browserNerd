package mcp

import "testing"

// TestLiveProbeHarness proves the probe harness drives a real page into the
// engine from its first byte: the 404 the page fetches while it loads must be
// a fact, not only the polls that follow.
func TestLiveProbeHarness(t *testing.T) {
	p := startLiveProbe(t)

	observe := &BrowserObserveTool{sessions: p.manager, engine: p.engine}
	res, err := observe.Execute(p.ctx, map[string]interface{}{"session_id": p.session})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	t.Logf("observe composite compact: ~%d tokens", responseTokens(t, res))

	var saw404 bool
	for _, f := range p.engine.FactsByPredicate("net_response") {
		if len(f.Args) >= 3 && asInt(f.Args[2]) == 404 {
			saw404 = true
		}
	}
	if !saw404 {
		t.Fatalf("the probe page's load-time /api/missing 404 never reached the engine")
	}
}
