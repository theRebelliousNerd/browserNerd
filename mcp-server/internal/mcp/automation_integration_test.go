package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

// TestIntegrationExecutePlan tests multi-step automation with a real browser
func TestIntegrationExecutePlan(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Create test page with form
	testHTML := `<!DOCTYPE html>
<html>
<head><title>Automation Test</title></head>
<body>
	<h1>Multi-Step Form</h1>
	<input id="username" type="text" placeholder="Username">
	<input id="email" type="email" placeholder="Email">
	<select id="country">
		<option value="">Select Country</option>
		<option value="us">United States</option>
		<option value="uk">United Kingdom</option>
		<option value="ca">Canada</option>
	</select>
	<button id="submit-btn">Submit</button>
	<div id="status"></div>
	<script>
		document.getElementById('submit-btn').addEventListener('click', () => {
			const username = document.getElementById('username').value;
			const email = document.getElementById('email').value;
			const country = document.getElementById('country').value;
			document.getElementById('status').textContent =
				'Submitted: ' + username + ', ' + email + ', ' + country;
		});
	</script>
</body>
</html>`

	session, err := sessions.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionID := session.ID
	jsHandle := "reason:" + sessionID + ":root_causes"
	_ = engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "disclosure_handle",
		Args:      []interface{}{sessionID, jsHandle, "reason", time.Now().UnixMilli()},
		Timestamp: time.Now(),
	}})

	page, _ := sessions.Page(sessionID)
	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("ExecutePlanTool with explicit actions", func(t *testing.T) {
		tool := &ExecutePlanTool{sessions: sessions, engine: engine}

		actions := []interface{}{
			map[string]interface{}{
				"type":  "type",
				"ref":   "username",
				"value": "testuser",
			},
			map[string]interface{}{
				"type":  "type",
				"ref":   "email",
				"value": "test@example.com",
			},
			map[string]interface{}{
				"type":  "select",
				"ref":   "country",
				"value": "us",
			},
			map[string]interface{}{
				"type": "click",
				"ref":  "submit-btn",
			},
		}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":    sessionID,
			"actions":       actions,
			"stop_on_error": true,
			"delay_ms":      100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		executed := resultMap["executed"].(int)
		if executed != 4 {
			t.Errorf("expected 4 actions executed, got %d", executed)
		}

		// Verify form was submitted
		time.Sleep(200 * time.Millisecond)
		evalTool := &EvaluateJSTool{sessions: sessions, engine: engine}
		evalResult, _ := evalTool.Execute(ctx, map[string]interface{}{
			"session_id":         sessionID,
			"script":             "() => document.getElementById('status').textContent",
			"approved_by_handle": jsHandle,
		})
		evalMap := evalResult.(map[string]interface{})
		status := evalMap["result"].(string)
		if status == "" {
			t.Error("expected non-empty status after form submission")
		}
	})

	t.Run("ExecutePlanTool stop on error", func(t *testing.T) {
		tool := &ExecutePlanTool{sessions: sessions, engine: engine}

		actions := []interface{}{
			map[string]interface{}{
				"type":  "type",
				"ref":   "username",
				"value": "user2",
			},
			map[string]interface{}{
				"type": "click",
				"ref":  "nonexistent-element", // This will fail
			},
			map[string]interface{}{
				"type":  "type",
				"ref":   "email",
				"value": "should-not-execute@example.com",
			},
		}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":    sessionID,
			"actions":       actions,
			"stop_on_error": true,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected failure when stop_on_error is true")
		}

		executed := resultMap["executed"].(int)
		if executed >= 3 {
			t.Errorf("expected execution to stop before action 3, but %d were executed", executed)
		}
	})

	t.Run("ExecutePlanTool continue on error", func(t *testing.T) {
		tool := &ExecutePlanTool{sessions: sessions, engine: engine}

		actions := []interface{}{
			map[string]interface{}{
				"type":  "type",
				"ref":   "username",
				"value": "user3",
			},
			map[string]interface{}{
				"type": "click",
				"ref":  "nonexistent-element", // This will fail
			},
			map[string]interface{}{
				"type":  "type",
				"ref":   "email",
				"value": "continue@example.com",
			},
		}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":    sessionID,
			"actions":       actions,
			"stop_on_error": false,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		// Should complete all actions despite error
		executed := resultMap["executed"].(int)
		if executed != 3 {
			t.Errorf("expected all 3 actions to be attempted, got %d", executed)
		}

		failed := resultMap["failed"].(int)
		if failed != 1 {
			t.Errorf("expected 1 failed action, got %d", failed)
		}
	})
}

// TestIntegrationWaitForCondition tests waiting for page conditions
func TestIntegrationWaitForCondition(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Create test page that changes after delay
	testHTML := `<!DOCTYPE html>
<html>
<head><title>Async Test</title></head>
<body>
	<div id="status">Loading...</div>
	<div id="content" style="display:none;"></div>
	<script>
		setTimeout(() => {
			document.getElementById('status').textContent = 'Ready';
			document.getElementById('content').style.display = 'block';
			document.getElementById('content').textContent = 'Content Loaded';
		}, 500);
	</script>
</body>
</html>`

	session, err := sessions.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionID := session.ID

	page, _ := sessions.Page(sessionID)
	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("WaitForConditionTool element exists", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"condition":  "element_exists",
			"selector":   "#status",
			"timeout_ms": 2000,
			"poll_ms":    100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		if resultMap["condition_met"].(bool) != true {
			t.Error("expected condition_met to be true")
		}
	})

	t.Run("WaitForConditionTool element visible", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"condition":  "element_visible",
			"selector":   "#content",
			"timeout_ms": 2000,
			"poll_ms":    100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		if resultMap["condition_met"].(bool) != true {
			t.Error("expected element to become visible")
		}
	})

	t.Run("WaitForConditionTool text contains", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"condition":  "text_contains",
			"selector":   "#status",
			"value":      "Ready",
			"timeout_ms": 2000,
			"poll_ms":    100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		if resultMap["condition_met"].(bool) != true {
			t.Error("expected text to contain 'Ready'")
		}
	})

	t.Run("WaitForConditionTool custom JavaScript", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"condition":  "custom",
			"script":     "() => document.getElementById('status').textContent === 'Ready'",
			"timeout_ms": 2000,
			"poll_ms":    100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		if resultMap["condition_met"].(bool) != true {
			t.Error("expected custom condition to be met")
		}
	})

	t.Run("WaitForConditionTool timeout", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"condition":  "text_contains",
			"selector":   "#status",
			"value":      "NonexistentText",
			"timeout_ms": 500,
			"poll_ms":    100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["condition_met"].(bool) {
			t.Error("expected condition_met to be false on timeout")
		}
		if resultMap["timed_out"].(bool) != true {
			t.Error("expected timed_out to be true")
		}
	})
}

// TestIntegrationSessionTools tests session management tools
func TestIntegrationSessionTools(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	t.Run("LaunchBrowserTool", func(t *testing.T) {
		// Browser should already be started by setupIntegrationBrowser
		// But we can test the tool wrapper
		tool := &LaunchBrowserTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("ListSessionsTool", func(t *testing.T) {
		tool := &ListSessionsTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		sessionsList := resultMap["sessions"]
		if sessionsList == nil {
			t.Error("expected sessions in result")
		}
	})

	t.Run("CreateSessionTool", func(t *testing.T) {
		tool := &CreateSessionTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"url": "about:blank",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		session := resultMap["session"].(*browser.Session)
		if session.ID == "" {
			t.Error("expected non-empty session ID")
		}
		if session.URL != "about:blank" {
			t.Errorf("expected URL 'about:blank', got %q", session.URL)
		}
	})

	t.Run("CreateSessionTool with URL", func(t *testing.T) {
		tool := &CreateSessionTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"url": "about:blank",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		session := resultMap["session"].(*browser.Session)
		if session.ID == "" {
			t.Error("expected non-empty session ID")
		}
	})

	var testSessionID string

	t.Run("Create session for fork test", func(t *testing.T) {
		session, err := sessions.CreateSession(ctx, "about:blank")
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}
		testSessionID = session.ID
	})

	t.Run("ForkSessionTool", func(t *testing.T) {
		tool := &ForkSessionTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": testSessionID,
			"url":        "https://example.com",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		forkedSession := result.(*browser.Session)
		if forkedSession.ID == "" {
			t.Error("expected non-empty forked session ID")
		}
		if forkedSession.ID == testSessionID {
			t.Error("forked session should have different ID")
		}
	})

	t.Run("SnapshotDOMTool", func(t *testing.T) {
		tool := &SnapshotDOMTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": testSessionID,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["session_id"] != testSessionID {
			t.Errorf("expected session_id %q, got %q", testSessionID, resultMap["session_id"])
		}
		if resultMap["status"] != "captured" {
			t.Errorf("expected status 'captured', got %q", resultMap["status"])
		}
	})

	t.Run("ReifyReactTool without React", func(t *testing.T) {
		tool := &ReifyReactTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": testSessionID,
			"emit_facts": false,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		// Should succeed even without React
		if result == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("ShutdownBrowserTool", func(t *testing.T) {
		// Note: This will shut down the browser, so it should be last
		tool := &ShutdownBrowserTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		// Browser should no longer be connected
		if sessions.IsConnected() {
			t.Error("expected browser to be disconnected after shutdown")
		}
	})
}
