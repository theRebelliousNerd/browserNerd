package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/security"
)

func TestStructuredSnapshotSecretsAndMarkers(t *testing.T) {
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "OpaqueFixtureCredential_4919")
	t.Setenv("GENERIC_PROVIDER_API_KEY", "Q7z")
	r := security.NewRedactor(nil)
	secrets := snapshotEnvironmentSecrets(r)
	input := map[string]interface{}{"props": map[string]interface{}{"value": "calendar", "onValueChange": map[string]interface{}{"type": "function"}, "password": "not-visible", "other": "OpaqueFixtureCredential_4919", "OpaqueFixtureCredential_4919": "key", "provider": "Q7z"}}
	out := cleanSnapshotValue(r.Sanitize(input), secrets)
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "not-visible") || strings.Contains(string(raw), "OpaqueFixtureCredential_4919") || strings.Contains(string(raw), "Q7z") {
		t.Fatalf("secret escaped redaction: %s", raw)
	}
	if !strings.Contains(string(raw), `"value":"calendar"`) || !strings.Contains(string(raw), `"type":"function"`) {
		t.Fatalf("safe evidence lost: %s", raw)
	}
}

func TestStructuredSnapshotFactArityAndNoFunctionSource(t *testing.T) {
	node := map[string]interface{}{"id": "f1", "name": "Tabs", "parent": "", "dom_id": "tabs", "props": map[string]interface{}{"onValueChange": map[string]interface{}{"type": "function"}, "value": "calendar"}, "state": map[string]interface{}{"0": "calendar"}}
	facts := snapshotFacts("own", []map[string]interface{}{node}, true)
	for _, fact := range facts {
		if len(fact.Args) < 1 || fact.Args[0] != "own" {
			t.Fatalf("fact lost session scope: %+v", fact)
		}
		if fact.Predicate == "dom_mapping" && len(fact.Args) != 3 {
			t.Fatalf("invalid mapping arity: %+v", fact)
		}
	}
	for _, o := range []SnapshotOptions{{MaxNodes: 501}, {MaxNodes: -1}, {MaxDepth: 51}} {
		if _, err := o.normalized(); err == nil {
			t.Fatalf("capture accepted unbounded options: %+v", o)
		}
	}
}
