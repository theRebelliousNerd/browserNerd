package browser

import (
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

func requirePayloadFact(t *testing.T, facts []mangle.Fact, field, kind string) {
	t.Helper()
	for _, fact := range facts {
		if fact.Predicate != "request_payload_field" || len(fact.Args) != 4 {
			continue
		}
		if fact.Args[2] == field && fact.Args[3] == kind {
			return
		}
	}
	t.Fatalf("expected payload fact %s=%s, got %+v", field, kind, facts)
}

func TestExtractRequestPayloadFactsJSON(t *testing.T) {
	facts := extractRequestPayloadFacts(
		"session-1",
		"req-1",
		`{"name":"alpha","count":3,"enabled":true,"tags":["a"],"meta":{"owner":"ops"}}`,
		proto.NetworkHeaders{
			"content-type": gson.New("application/json"),
		},
		time.UnixMilli(1000),
	)

	if len(facts) != 5 {
		t.Fatalf("expected 5 payload facts, got %d", len(facts))
	}
	requirePayloadFact(t, facts, "name", "string")
	requirePayloadFact(t, facts, "count", "number")
	requirePayloadFact(t, facts, "enabled", "boolean")
	requirePayloadFact(t, facts, "tags", "array")
	requirePayloadFact(t, facts, "meta", "object")
}

func TestExtractRequestPayloadFactsFormEncoded(t *testing.T) {
	facts := extractRequestPayloadFacts(
		"session-1",
		"req-2",
		"email=test%40example.com&token=abc123",
		proto.NetworkHeaders{
			"content-type": gson.New("application/x-www-form-urlencoded"),
		},
		time.UnixMilli(2000),
	)

	if len(facts) != 2 {
		t.Fatalf("expected 2 payload facts, got %d", len(facts))
	}
	requirePayloadFact(t, facts, "email", "string")
	requirePayloadFact(t, facts, "token", "string")
}
