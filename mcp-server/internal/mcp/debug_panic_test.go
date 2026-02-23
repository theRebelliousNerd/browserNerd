package mcp

import (
	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"context"
	"testing"
	"time"
)

func TestPanicHunt_ObserveSession(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 2000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create mangle engine: %v", err)
	}

	sessions := browser.NewSessionManager(config.BrowserConfig{}, engine)

	tool := &BrowserObserveTool{
		sessions: sessions,
		engine:   engine,
	}

	ctx := context.Background()

	actTool := &BrowserActTool{
		sessions: sessions,
		engine:   engine,
	}

	actRes, err := actTool.Execute(ctx, map[string]interface{}{
		"session_id": "",
		"operations": []interface{}{
			map[string]interface{}{
				"type": "session_create",
				"url":  "https://en.wikipedia.org/wiki/Mangle_(machine_learning)",
			},
		},
	})
	if err != nil {
		t.Fatalf("session create failed: %v", err)
	}

	var sessionID string
	if actMap, ok := actRes.(map[string]interface{}); ok {
		if results, ok := actMap["results"].([]interface{}); ok && len(results) > 0 {
			if resMap, ok := results[0].(map[string]interface{}); ok {
				if id, ok := resMap["session_id"].(string); ok {
					sessionID = id
				}
			}
		}
	}

	if sessionID == "" {
		// fallback to pulling from check_sessions if needed, but normally act returns it
		obsRes, _ := tool.Execute(ctx, map[string]interface{}{
			"session_id": "",
			"intent":     "check_sessions",
		})
		if obsMap, ok := obsRes.(map[string]interface{}); ok {
			if data, ok := obsMap["data"].(map[string]interface{}); ok {
				if sessList, ok := data["sessions"].([]map[string]interface{}); ok && len(sessList) > 0 {
					sessionID = sessList[0]["id"].(string)
				}
			}
		}
	}

	t.Logf("Created session: %s", sessionID)

	// Wait for the page to fully load and emit facts
	time.Sleep(2500 * time.Millisecond)

	_, err = tool.Execute(ctx, map[string]interface{}{
		"session_id": sessionID,
		"intent":     "quick_status",
	})

	if err != nil {
		t.Logf("Expected initial observe error: %v", err)
	}
}
