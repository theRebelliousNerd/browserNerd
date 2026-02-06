package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// TestLiveServerWithBrowser runs comprehensive tests with a real browser.
func TestLiveServerWithBrowser(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	_, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Browser: config.BrowserConfig{
			Headless:              boolPtr(true),
			EnableDOMIngestion:    true,
			EnableHeaderIngestion: true,
			EventThrottleMs:       50,
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 10000,
		},
		Docker: config.DockerConfig{
			Enabled: false,
		},
	}

	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)

	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Launch browser
	t.Run("LaunchBrowser", func(t *testing.T) {
		result, err := server.ExecuteTool("launch-browser", map[string]interface{}{})
		if err != nil {
			t.Fatalf("launch-browser failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Fatalf("launch-browser success=false: %v", resultMap["error"])
		}
	})

	defer func() {
		server.ExecuteTool("shutdown-browser", map[string]interface{}{})
	}()

	var sessionID string

	t.Run("CreateSession", func(t *testing.T) {
		result, err := server.ExecuteTool("create-session", map[string]interface{}{
			"url": "about:blank",
		})
		if err != nil {
			t.Fatalf("create-session failed: %v", err)
		}
		resultMap := result.(*browser.Session)
		if resultMap.ID == "" {
			t.Error("Expected session ID")
		}
		sessionID = resultMap.ID
	})

	t.Run("ListSessions", func(t *testing.T) {
		result, err := server.ExecuteTool("list-sessions", map[string]interface{}{})
		if err != nil {
			t.Fatalf("list-sessions failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		sessions := resultMap["sessions"].([]browser.Session)
		if len(sessions) == 0 {
			t.Error("Expected at least one session")
		}
	})

	t.Run("NavigateURL", func(t *testing.T) {
		result, err := server.ExecuteTool("navigate-url", map[string]interface{}{
			"session_id": sessionID,
			"url":        "data:text/html,<html><head><title>Test Page</title></head><body><h1 id='heading'>Hello World</h1><button id='btn1'>Click Me</button><input type='text' id='input1' name='username'/><a href='#link1' id='link1'>Link 1</a></body></html>",
		})
		if err != nil {
			t.Fatalf("navigate-url failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("navigate-url success=false: %v", resultMap["error"])
		}
	})

	// Wait for page to load
	time.Sleep(500 * time.Millisecond)

	t.Run("GetPageState", func(t *testing.T) {
		result, err := server.ExecuteTool("get-page-state", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("get-page-state failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["title"] == nil {
			t.Error("Expected title in result")
		}
		if resultMap["url"] == nil {
			t.Error("Expected url in result")
		}
	})

	t.Run("GetInteractiveElements", func(t *testing.T) {
		result, err := server.ExecuteTool("get-interactive-elements", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("get-interactive-elements failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["elements"] == nil {
			t.Error("Expected elements in result")
		}
		// Should find the button and input
		elements := resultMap["elements"]
		if elements != nil {
			t.Logf("Found %d elements", len(elements.([]interface{})))
		}
	})

	t.Run("GetNavigationLinks", func(t *testing.T) {
		result, err := server.ExecuteTool("get-navigation-links", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("get-navigation-links failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["links"] == nil {
			t.Error("Expected links in result")
		}
	})

	t.Run("InteractClick", func(t *testing.T) {
		result, err := server.ExecuteTool("interact", map[string]interface{}{
			"session_id": sessionID,
			"ref":        "btn1",
			"action":     "click",
		})
		if err != nil {
			t.Fatalf("interact failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Logf("interact click success=false: %v (may be expected)", resultMap["error"])
		}
	})

	t.Run("InteractType", func(t *testing.T) {
		result, err := server.ExecuteTool("interact", map[string]interface{}{
			"session_id": sessionID,
			"ref":        "input1",
			"action":     "type",
			"value":      "test input",
		})
		if err != nil {
			t.Fatalf("interact failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Logf("interact type success=false: %v (may be expected)", resultMap["error"])
		}
	})

	t.Run("FillForm", func(t *testing.T) {
		result, err := server.ExecuteTool("fill-form", map[string]interface{}{
			"session_id": sessionID,
			"fields": []interface{}{
				map[string]interface{}{
					"ref":   "input1",
					"value": "filled value",
				},
			},
		})
		if err != nil {
			t.Fatalf("fill-form failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		t.Logf("fill-form result: %v", resultMap)
	})

	t.Run("PressKey", func(t *testing.T) {
		result, err := server.ExecuteTool("press-key", map[string]interface{}{
			"session_id": sessionID,
			"key":        "Tab",
		})
		if err != nil {
			t.Fatalf("press-key failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Logf("press-key success=false: %v", resultMap["error"])
		}
	})

	t.Run("EvaluateJS", func(t *testing.T) {
		result, err := server.ExecuteTool("evaluate-js", map[string]interface{}{
			"session_id":  sessionID,
			"script":      "document.title",
			"gate_reason": "explicit_user_intent",
		})
		if err != nil {
			t.Fatalf("evaluate-js failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("evaluate-js success=false: %v", resultMap["error"])
		}
	})

	t.Run("EvaluateJSWithError", func(t *testing.T) {
		result, err := server.ExecuteTool("evaluate-js", map[string]interface{}{
			"session_id":  sessionID,
			"script":      "undefinedVariable.property",
			"gate_reason": "explicit_user_intent",
		})
		if err != nil {
			t.Fatalf("evaluate-js failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		// Should fail with JS error
		if resultMap["success"].(bool) {
			t.Error("Expected success=false for undefined variable")
		}
	})

	t.Run("Screenshot", func(t *testing.T) {
		result, err := server.ExecuteTool("screenshot", map[string]interface{}{
			"session_id": sessionID,
			"format":     "png",
		})
		if err != nil {
			t.Fatalf("screenshot failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("screenshot success=false: %v", resultMap["error"])
		}
		if resultMap["data"] == nil || resultMap["data"] == "" {
			t.Error("Expected screenshot data")
		}
	})

	t.Run("BrowserHistory", func(t *testing.T) {
		result, err := server.ExecuteTool("browser-history", map[string]interface{}{
			"session_id": sessionID,
			"action":     "reload",
		})
		if err != nil {
			t.Fatalf("browser-history failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("browser-history success=false: %v", resultMap["error"])
		}
	})

	t.Run("SnapshotDOM", func(t *testing.T) {
		result, err := server.ExecuteTool("snapshot-dom", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("snapshot-dom failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("snapshot-dom success=false: %v", resultMap["error"])
		}
	})

	t.Run("ReifyReact", func(t *testing.T) {
		result, err := server.ExecuteTool("reify-react", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("reify-react failed: %v", err)
		}
		// This may return empty for non-React pages
		t.Logf("reify-react result: %T", result)
	})

	t.Run("DiscoverHiddenContent", func(t *testing.T) {
		result, err := server.ExecuteTool("discover-hidden-content", map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("discover-hidden-content failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Logf("discover-hidden-content success=false: %v", resultMap["error"])
		}
	})

	t.Run("ForkSession", func(t *testing.T) {
		result, err := server.ExecuteTool("fork-session", map[string]interface{}{
			"session_id": sessionID,
			"url":        "about:blank",
		})
		if err != nil {
			t.Fatalf("fork-session failed: %v", err)
		}
		forked := result.(*browser.Session)
		if forked.ID == "" {
			t.Error("Expected forked session ID")
		}
		if forked.ID == sessionID {
			t.Error("Forked session should have different ID")
		}
	})

	// Mangle/fact tools
	t.Run("PushFacts", func(t *testing.T) {
		result, err := server.ExecuteTool("push-facts", map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "test_fact",
					"args":      []interface{}{"value1", "value2"},
				},
				map[string]interface{}{
					"predicate": "navigation_event",
					"args":      []interface{}{sessionID, "http://example.com", 123456789},
				},
			},
		})
		if err != nil {
			t.Fatalf("push-facts failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["accepted"].(int) != 2 {
			t.Errorf("Expected 2 facts accepted, got %v", resultMap["accepted"])
		}
	})

	t.Run("ReadFacts", func(t *testing.T) {
		result, err := server.ExecuteTool("read-facts", map[string]interface{}{
			"limit": 100,
		})
		if err != nil {
			t.Fatalf("read-facts failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		facts := resultMap["facts"].([]mangle.Fact)
		if len(facts) == 0 {
			t.Log("No facts returned (buffer may be empty)")
		} else {
			t.Logf("Read %d facts", len(facts))
		}
	})

	t.Run("QueryFacts", func(t *testing.T) {
		result, err := server.ExecuteTool("query-facts", map[string]interface{}{
			"query": "test_fact(X, Y).",
		})
		if err != nil {
			t.Fatalf("query-facts failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["matches"] == nil {
			t.Error("Expected matches in result")
		}
	})

	t.Run("QueryTemporal", func(t *testing.T) {
		result, err := server.ExecuteTool("query-temporal", map[string]interface{}{
			"predicate":  "test_fact",
			"window_sec": 60,
		})
		if err != nil {
			t.Fatalf("query-temporal failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["facts"] == nil {
			t.Error("Expected facts in result")
		}
	})

	t.Run("SubmitRule", func(t *testing.T) {
		result, err := server.ExecuteTool("submit-rule", map[string]interface{}{
			"rule": "derived_fact(X) :- test_fact(X, _).",
		})
		if err != nil {
			t.Fatalf("submit-rule failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Logf("submit-rule success=false: %v", resultMap["error"])
		}
	})

	t.Run("EvaluateRule", func(t *testing.T) {
		result, err := server.ExecuteTool("evaluate-rule", map[string]interface{}{
			"predicate": "test_fact",
		})
		if err != nil {
			t.Fatalf("evaluate-rule failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["results"] == nil {
			t.Error("Expected results in result")
		}
	})

	t.Run("AwaitFact", func(t *testing.T) {
		// Push a fact first
		server.ExecuteTool("push-facts", map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "await_test",
					"args":      []interface{}{"ready"},
				},
			},
		})

		result, err := server.ExecuteTool("await-fact", map[string]interface{}{
			"predicate":  "await_test",
			"args":       []interface{}{"ready"},
			"timeout_ms": 1000,
		})
		if err != nil {
			t.Fatalf("await-fact failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["matched"].(bool) {
			t.Error("Expected matched=true")
		}
	})

	t.Run("AwaitConditions", func(t *testing.T) {
		result, err := server.ExecuteTool("await-conditions", map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"predicate": "await_test",
					"args":      []interface{}{"ready"},
				},
			},
			"timeout_ms": 1000,
			"match_all":  true,
		})
		if err != nil {
			t.Fatalf("await-conditions failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		t.Logf("await-conditions result: success=%v", resultMap["success"])
	})

	t.Run("WaitForCondition", func(t *testing.T) {
		result, err := server.ExecuteTool("wait-for-condition", map[string]interface{}{
			"session_id": sessionID,
			"predicate":  "current_url",
			"timeout_ms": 1000,
		})
		if err != nil {
			t.Fatalf("wait-for-condition failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		t.Logf("wait-for-condition result: status=%v", resultMap["status"])
	})

	t.Run("DiagnosePage", func(t *testing.T) {
		result, err := server.ExecuteTool("diagnose-page", map[string]interface{}{})
		if err != nil {
			t.Fatalf("diagnose-page failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["status"] == nil {
			t.Error("Expected status in result")
		}
	})

	t.Run("AwaitStableState", func(t *testing.T) {
		result, err := server.ExecuteTool("await-stable-state", map[string]interface{}{
			"timeout_ms":      500,
			"network_idle_ms": 100,
		})
		if err != nil {
			t.Fatalf("await-stable-state failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		if status != "stable" && status != "timeout" {
			t.Errorf("Unexpected status: %v", status)
		}
	})

	t.Run("GetConsoleErrors", func(t *testing.T) {
		result, err := server.ExecuteTool("get-console-errors", map[string]interface{}{})
		if err != nil {
			t.Fatalf("get-console-errors failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Error("Expected success=true")
		}
	})

	t.Run("GetToastNotifications", func(t *testing.T) {
		result, err := server.ExecuteTool("get-toast-notifications", map[string]interface{}{})
		if err != nil {
			t.Fatalf("get-toast-notifications failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["status"] == nil {
			t.Error("Expected status in result")
		}
	})

	t.Run("SubscribeRule", func(t *testing.T) {
		result, err := server.ExecuteTool("subscribe-rule", map[string]interface{}{
			"predicate": "test_fact",
			"poll_ms":   100,
			"max_polls": 2,
		})
		if err != nil {
			t.Fatalf("subscribe-rule failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		t.Logf("subscribe-rule result: %v", resultMap)
	})

	t.Run("ExecutePlan", func(t *testing.T) {
		result, err := server.ExecuteTool("execute-plan", map[string]interface{}{
			"session_id": sessionID,
			"steps": []interface{}{
				map[string]interface{}{
					"action":  "evaluate_js",
					"payload": "1 + 1",
				},
			},
		})
		if err != nil {
			t.Fatalf("execute-plan failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		t.Logf("execute-plan result: success=%v", resultMap["success"])
	})

	t.Run("ShutdownBrowser", func(t *testing.T) {
		result, err := server.ExecuteTool("shutdown-browser", map[string]interface{}{})
		if err != nil {
			t.Fatalf("shutdown-browser failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Error("Expected success=true")
		}
	})
}

// TestLiveAttachSession tests attaching to an existing session.
func TestLiveAttachSession(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Browser: config.BrowserConfig{
			Headless: boolPtr(true),
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		},
	}

	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Launch browser
	if _, err := server.ExecuteTool("launch-browser", nil); err != nil {
		t.Fatalf("launch-browser failed: %v", err)
	}
	defer server.ExecuteTool("shutdown-browser", nil)

	// Create session
	result, _ := server.ExecuteTool("create-session", map[string]interface{}{
		"url": "about:blank",
	})
	session := result.(*browser.Session)

	// Attach to the same target
	_ = ctx // Use context
	attachResult, err := server.ExecuteTool("attach-session", map[string]interface{}{
		"target_id": session.TargetID,
	})
	if err != nil {
		t.Fatalf("attach-session failed: %v", err)
	}

	attached := attachResult.(*browser.Session)
	if attached.Status != "attached" {
		t.Errorf("Expected status 'attached', got %q", attached.Status)
	}
}

// TestWrapToolWithError tests the wrapTool function error path.
func TestWrapToolWithError(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		},
	}

	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Try to execute a tool that will fail
	ctx := context.Background()
	tool := server.tools["reify-react"]
	_, err = tool.Execute(ctx, map[string]interface{}{
		"session_id": "nonexistent-session",
	})
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func boolPtr(b bool) *bool {
	return &b
}
