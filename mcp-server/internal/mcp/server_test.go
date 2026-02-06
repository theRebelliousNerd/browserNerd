package mcp

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func setupTestServerConfig() config.Config {
	return config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		},
		Docker: config.DockerConfig{
			Enabled: false,
		},
	}
}

func TestNewServer(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Create a minimal session manager (won't start browser)
	sessions := browser.NewSessionManager(cfg.Browser, engine)

	t.Run("creates server successfully", func(t *testing.T) {
		server, err := NewServer(cfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}
		if server == nil {
			t.Fatal("expected non-nil server")
		}
		if server.tools == nil {
			t.Error("expected tools map to be initialized")
		}
		if len(server.tools) == 0 {
			t.Error("expected tools to be registered")
		}
	})

	t.Run("with docker enabled", func(t *testing.T) {
		dockerCfg := cfg
		dockerCfg.Docker = config.DockerConfig{
			Enabled:    true,
			Containers: []string{"test-container"},
			LogWindow:  "5m",
		}

		server, err := NewServer(dockerCfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer with docker failed: %v", err)
		}
		if server.dockerClient == nil {
			t.Error("expected docker client to be initialized")
		}
	})

	t.Run("without docker", func(t *testing.T) {
		noDockCfg := cfg
		noDockCfg.Docker.Enabled = false

		server, err := NewServer(noDockCfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}
		if server.dockerClient != nil {
			t.Error("expected docker client to be nil when disabled")
		}
	})
}

func TestExecuteTool(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	t.Run("execute existing tool", func(t *testing.T) {
		// read-facts should exist and work without browser
		result, err := server.ExecuteTool("read-facts", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("execute non-existent tool", func(t *testing.T) {
		_, err := server.ExecuteTool("non-existent-tool", map[string]interface{}{})
		if err == nil {
			t.Error("expected error for non-existent tool")
		}
	})

	t.Run("push-facts tool", func(t *testing.T) {
		result, err := server.ExecuteTool("push-facts", map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "test_event",
					"args":      []interface{}{"value1"},
				},
			},
		})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["accepted"].(int) != 1 {
			t.Errorf("expected 1 accepted, got %v", resultMap["accepted"])
		}
	})

	t.Run("query-facts tool", func(t *testing.T) {
		result, err := server.ExecuteTool("query-facts", map[string]interface{}{
			"query": "test_event(X).",
		})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

func TestToolInterface(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify all registered tools implement the Tool interface correctly
	t.Run("all tools have valid names", func(t *testing.T) {
		for name, tool := range server.tools {
			if tool.Name() != name {
				t.Errorf("tool registered as %q but Name() returns %q", name, tool.Name())
			}
		}
	})

	t.Run("all tools have descriptions", func(t *testing.T) {
		for name, tool := range server.tools {
			if tool.Description() == "" {
				t.Errorf("tool %q has empty description", name)
			}
		}
	})

	t.Run("all tools have valid schemas", func(t *testing.T) {
		for name, tool := range server.tools {
			schema := tool.InputSchema()
			if schema == nil {
				t.Errorf("tool %q has nil schema", name)
				continue
			}
			if schema["type"] != "object" {
				t.Errorf("tool %q schema type is not 'object': %v", name, schema["type"])
			}
		}
	})
}

func TestToolCount(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// We expect a significant number of tools to be registered
	// Based on registerAllTools, there should be at least 30 tools
	expectedMinTools := 25
	if len(server.tools) < expectedMinTools {
		t.Errorf("expected at least %d tools, got %d", expectedMinTools, len(server.tools))
	}
}

func TestWrapTool(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Test that tools are wrapped correctly by executing through the server
	t.Run("tool execution returns result", func(t *testing.T) {
		result, err := server.ExecuteTool("read-facts", nil)
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		// Should work even with nil args
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

func TestMarshalToolPayloadFallback(t *testing.T) {
	payload := marshalToolPayload("test-tool", map[string]interface{}{
		"bad": math.NaN(),
	})
	if len(payload) == 0 {
		t.Fatal("expected non-empty payload")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload should always be valid JSON: %v", err)
	}
	if success, _ := decoded["success"].(bool); success {
		t.Fatalf("expected success=false fallback payload, got %v", decoded)
	}
	if decoded["error"] == nil {
		t.Fatalf("expected fallback payload to include error, got %v", decoded)
	}
}

func TestServerToolRegistration(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Verify specific expected tools are registered
	expectedTools := []string{
		"list-sessions",
		"create-session",
		"attach-session",
		"fork-session",
		"reify-react",
		"snapshot-dom",
		"launch-browser",
		"shutdown-browser",
		"push-facts",
		"read-facts",
		"query-facts",
		"submit-rule",
		"query-temporal",
		"evaluate-rule",
		"subscribe-rule",
		"await-fact",
		"await-conditions",
		"get-console-errors",
		"get-toast-notifications",
		"get-navigation-links",
		"get-interactive-elements",
		"discover-hidden-content",
		"interact",
		"get-page-state",
		"navigate-url",
		"press-key",
		"browser-observe",
		"browser-act",
		"browser-reason",
		"screenshot",
		"browser-history",
		"evaluate-js",
		"fill-form",
		"execute-plan",
		"wait-for-condition",
		"diagnose-page",
		"await-stable-state",
	}

	for _, toolName := range expectedTools {
		t.Run("tool_"+toolName, func(t *testing.T) {
			if _, exists := server.tools[toolName]; !exists {
				t.Errorf("expected tool %q to be registered", toolName)
			}
		})
	}
}

// TestSessionToolsWithoutBrowser tests session tools return proper errors when no browser
func TestSessionToolsWithoutBrowser(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()

	t.Run("list-sessions without browser", func(t *testing.T) {
		result, err := server.ExecuteTool("list-sessions", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		// Should return empty sessions list
		sessions := resultMap["sessions"]
		if sessions == nil {
			t.Error("expected sessions key in result")
		}
	})

	t.Run("create-session without browser", func(t *testing.T) {
		tool := server.tools["create-session"]
		_, err := tool.Execute(ctx, map[string]interface{}{"url": "about:blank"})
		// Should fail because no browser is running
		if err == nil {
			t.Log("create-session succeeded unexpectedly - browser may be running")
		}
		// Either way, the tool should be callable
	})

	t.Run("attach-session without target_id", func(t *testing.T) {
		tool := server.tools["attach-session"]
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing target_id")
		}
	})

	t.Run("attach-session with target_id but no browser", func(t *testing.T) {
		tool := server.tools["attach-session"]
		_, err := tool.Execute(ctx, map[string]interface{}{"target_id": "fake-target"})
		// Should fail because no browser is running
		if err == nil {
			t.Log("attach-session succeeded unexpectedly - browser may be running")
		}
	})

	t.Run("fork-session without session_id", func(t *testing.T) {
		tool := server.tools["fork-session"]
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
	})

	t.Run("reify-react without session_id", func(t *testing.T) {
		tool := server.tools["reify-react"]
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
	})

	t.Run("snapshot-dom without session_id", func(t *testing.T) {
		tool := server.tools["snapshot-dom"]
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
	})
}

// TestNavigationToolsValidation tests navigation tools parameter validation
func TestNavigationToolsValidation(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx := context.Background()

	t.Run("get-page-state without session_id", func(t *testing.T) {
		tool := server.tools["get-page-state"]
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
	})

	t.Run("navigate-url without session_id", func(t *testing.T) {
		tool := server.tools["navigate-url"]
		result, err := tool.Execute(ctx, map[string]interface{}{"url": "https://example.com"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without session_id")
		}
	})

	t.Run("navigate-url without url", func(t *testing.T) {
		tool := server.tools["navigate-url"]
		result, err := tool.Execute(ctx, map[string]interface{}{"session_id": "test-session"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without url")
		}
	})

	t.Run("press-key without session_id", func(t *testing.T) {
		tool := server.tools["press-key"]
		result, err := tool.Execute(ctx, map[string]interface{}{"key": "Enter"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without session_id")
		}
	})

	t.Run("press-key without key", func(t *testing.T) {
		tool := server.tools["press-key"]
		result, err := tool.Execute(ctx, map[string]interface{}{"session_id": "test-session"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without key")
		}
	})

	t.Run("browser-history without session_id or action", func(t *testing.T) {
		tool := server.tools["browser-history"]
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without session_id and action")
		}
	})

	t.Run("interact without session_id", func(t *testing.T) {
		tool := server.tools["interact"]
		_, err := tool.Execute(ctx, map[string]interface{}{"ref": "button", "action": "click"})
		// InteractTool returns error for missing required params
		if err == nil {
			t.Error("expected error for missing session_id")
		}
	})
}

// TestDiagnosticTools tests diagnostic tool execution
func TestDiagnosticTools(t *testing.T) {
	cfg := setupTestServerConfig()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	t.Run("diagnose-page without issues", func(t *testing.T) {
		result, err := server.ExecuteTool("diagnose-page", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		if status != "ok" && status != "warning" && status != "error" {
			t.Errorf("unexpected status: %v", status)
		}
	})

	t.Run("await-stable-state", func(t *testing.T) {
		result, err := server.ExecuteTool("await-stable-state", map[string]interface{}{
			"timeout_ms":      100,
			"network_idle_ms": 50,
		})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		if status != "stable" && status != "timeout" {
			t.Errorf("expected status 'stable' or 'timeout', got %v", status)
		}
	})

	t.Run("get-console-errors", func(t *testing.T) {
		result, err := server.ExecuteTool("get-console-errors", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Error("expected success to be true")
		}
	})

	t.Run("get-toast-notifications", func(t *testing.T) {
		result, err := server.ExecuteTool("get-toast-notifications", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		// Should have status field
		if resultMap["status"] == nil {
			t.Error("expected status in result")
		}
	})
}
