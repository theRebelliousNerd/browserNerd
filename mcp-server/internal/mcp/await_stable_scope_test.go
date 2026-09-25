package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// TestAwaitStableIgnoresAnotherTabThatPolls: a quiet tab settles while a
// second tab keeps requesting, because only the waiting tab's facts count.
func TestAwaitStableIgnoresAnotherTabThatPolls(t *testing.T) {
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				_ = engine.AddFacts(ctx, []mangle.Fact{{
					Predicate: "net_request",
					Args:      []interface{}{"busy-tab", "r" + time.Now().Format("150405.000000"), "GET", "http://x/api/poll", "", now.UnixMilli()},
					Timestamp: now,
				}})
			}
		}
	}()

	tool := &AwaitStableStateTool{engine: engine}
	res, err := tool.Execute(ctx, map[string]interface{}{"session_id": "quiet-tab", "timeout_ms": 3000, "network_idle_ms": 300})
	if err != nil {
		t.Fatal(err)
	}
	if out := res.(map[string]interface{}); out["status"] != "stable" {
		t.Fatalf("the quiet tab must settle despite the busy one: %v", out)
	}

	res, err = tool.Execute(ctx, map[string]interface{}{"session_id": "busy-tab", "timeout_ms": 800, "network_idle_ms": 300})
	if err != nil {
		t.Fatal(err)
	}
	if out := res.(map[string]interface{}); out["status"] != "timeout" {
		t.Fatalf("the polling tab itself never settles: %v", out)
	}
}
