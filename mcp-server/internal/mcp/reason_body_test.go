package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBodyShapeNamesAnEmptyListInOneLine(t *testing.T) {
	shape := summarizeBody(`{"items": [], "total": 0, "page": {"next": null, "cursor": "abc"}}`)
	raw, _ := json.Marshal(shape)
	got := string(raw)
	for _, want := range []string{`"items":"array[0]"`, `"total":0`, `"next":null`} {
		if !strings.Contains(got, want) {
			t.Fatalf("shape %s lacks %s", got, want)
		}
	}
}

func TestBodyShapeKeepsStructureAndDropsBulk(t *testing.T) {
	rows := make([]map[string]interface{}, 500)
	for i := range rows {
		rows[i] = map[string]interface{}{"id": i, "name": strings.Repeat("x", 400), "access_token": "eyJ-secret"}
	}
	raw, _ := json.Marshal(map[string]interface{}{"data": rows})
	shapeJSON, _ := json.Marshal(summarizeBody(string(raw)))
	got := string(shapeJSON)
	if !strings.Contains(got, `"array":500`) {
		t.Fatalf("array length lost: %s", got)
	}
	if strings.Contains(got, "eyJ-secret") {
		t.Fatalf("token value echoed: %s", got)
	}
	if len(got) > 400 {
		t.Fatalf("a 500-row body shaped to %d bytes; the shape must not scale with rows", len(got))
	}
}

func TestBodyShapeBoundsDepthAndKeys(t *testing.T) {
	deep := map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": map[string]interface{}{"d": 1, "e": 2}}}}
	raw, _ := json.Marshal(deep)
	shapeJSON, _ := json.Marshal(summarizeBody(string(raw)))
	if !strings.Contains(string(shapeJSON), "object{d,e}") {
		t.Fatalf("depth limit did not name the keys: %s", shapeJSON)
	}

	wide := map[string]interface{}{}
	for i := 0; i < 40; i++ {
		wide[strings.Repeat("k", 1)+string(rune('a'+i%26))+string(rune('a'+i/26))] = i
	}
	raw, _ = json.Marshal(wide)
	shapeJSON, _ = json.Marshal(summarizeBody(string(raw)))
	if !strings.Contains(string(shapeJSON), "+28 keys") {
		t.Fatalf("key cap not reported: %s", shapeJSON)
	}
}

func TestBodyShapeOfTextIsItsFirstWords(t *testing.T) {
	got, _ := summarizeBody("<html>\n  <body>   Service Unavailable  </body></html>").(string)
	if got != "<html> <body> Service Unavailable </body></html>" {
		t.Fatalf("text body: %q", got)
	}
	if summarizeBody("   ") != "empty body" {
		t.Fatal("an empty body must say so")
	}
}

// TestLiveNetworkBodyReadsWhatTheAPIReturned proves the body path through
// Chrome: the probe's poll answers {"ok": true}, and network+expand+body
// returns that shape for the latest poll.
func TestLiveNetworkBodyReadsWhatTheAPIReturned(t *testing.T) {
	p := startLiveProbe(t)
	time.Sleep(500 * time.Millisecond)

	reason := &BrowserReasonTool{engine: p.engine, sessions: p.manager}
	res, err := reason.Execute(p.ctx, map[string]interface{}{
		"session_id": p.session,
		"topic":      "network",
		"expand":     "/api/poll",
		"body":       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]interface{})
	t.Logf("network expand body: ~%d tokens", responseTokens(t, out))
	rows, _ := out["calls"].([]map[string]interface{})
	shaped := 0
	for _, row := range rows {
		if shape, ok := row["body_shape"].(map[string]interface{}); ok {
			if shape["ok"] != true {
				t.Fatalf("poll body shape = %v, want ok:true", shape)
			}
			shaped++
		}
	}
	if shaped == 0 || shaped > maxBodiesPerCall {
		t.Fatalf("want 1..%d shaped bodies, got %d in %v", maxBodiesPerCall, shaped, rows)
	}
}
