package mcp

import (
	"encoding/json"
	"testing"
)

// A navigate to the URL the tab is already on reloads the page and says so.
// It once returned success with a note and no reload, so a screenshot taken
// to verify a fix showed the page from before the fix.
func TestLiveNavigateToTheCurrentURLReloadsThePage(t *testing.T) {
	p := startLiveProbe(t)
	url := p.eval(t, p.session, "location.href")
	p.eval(t, p.session, "(window.__beforeReload = 'set', 'set')")

	res := p.actOK(t, map[string]interface{}{
		"operations": ops(map[string]interface{}{"type": "navigate", "url": url}),
	})
	raw, _ := json.Marshal(res)
	results, _ := res["results"].([]map[string]interface{})
	if len(results) != 1 || results[0]["reloaded"] != true || results[0]["url"] != url {
		t.Fatalf("the compact entry must say the page was reloaded: %s", raw)
	}
	if got := p.eval(t, p.session, "String(window.__beforeReload)"); got != "undefined" {
		t.Fatalf("the page was not reloaded: a value set before the navigate survived (%q)", got)
	}

	other := p.baseURL + "/?next=1"
	res = p.actOK(t, map[string]interface{}{
		"operations": ops(map[string]interface{}{"type": "navigate", "url": other}),
	})
	results, _ = res["results"].([]map[string]interface{})
	if len(results) != 1 || results[0]["reloaded"] != nil || results[0]["url"] != other {
		raw, _ = json.Marshal(res)
		t.Fatalf("a navigate to another URL is not a reload: %s", raw)
	}
}
