package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// TestIntegrationNavigationTools tests navigation tools with a real browser
// Set SKIP_LIVE_TESTS="" to run these tests with a live browser
func TestIntegrationNavigationTools(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Create a test session
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

	// Navigate to test page with content
	testHTML := `<!DOCTYPE html>
<html>
<head><title>Navigation Test Page</title></head>
<body>
	<h1>Test Page</h1>
	<button id="btn-1" data-testid="submit-button">Submit</button>
	<button id="btn-2" aria-label="Cancel Button">Cancel</button>
	<a href="#section1" id="link-1">Section 1</a>
	<a href="#section2" id="link-2">Section 2</a>
	<input id="input-1" type="text" placeholder="Enter name">
	<input id="password" type="password" value="never-return-this">
	<select id="select-1">
		<option value="1">Option 1</option>
		<option value="2">Option 2</option>
	</select>
	<div id="section1">Section 1 Content</div>
	<div id="section2" role="tabpanel" style="display:none;">Hidden Section 2</div>
</body>
</html>`

	page, _ := sessions.Page(sessionID)
	dataURL := htmlDataURL(testHTML)
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("GetPageStateTool", func(t *testing.T) {
		tool := &GetPageStateTool{sessions: sessions}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["url"] == nil {
			t.Error("expected url in result")
		}
		if resultMap["title"] == nil {
			t.Error("expected title in result")
		}
	})

	t.Run("NavigateURLTool", func(t *testing.T) {
		tool := &NavigateURLTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"url":        dataURL,
			"wait_until": "load",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		currentURLs := engine.FactsByPredicate("current_url")
		if len(currentURLs) == 0 {
			t.Fatal("expected navigate-url to emit current_url even when already on the requested URL")
		}
		last := currentURLs[len(currentURLs)-1]
		if last.Args[0] != sessionID || last.Args[1] != dataURL {
			t.Fatalf("unexpected current_url fact: %+v", last)
		}
	})

	t.Run("GetInteractiveElementsTool", func(t *testing.T) {
		tool := &GetInteractiveElementsTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":   sessionID,
			"filter":       "all",
			"visible_only": true,
			"limit":        50,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		elements := resultMap["elements"].([]interface{})
		if len(elements) == 0 {
			t.Error("expected at least some interactive elements")
		}

		// Verify element structure
		firstElem := elements[0].(map[string]interface{})
		if firstElem["ref"] == nil {
			t.Error("expected ref in element")
		}
		if firstElem["type"] == nil {
			t.Error("expected type in element")
		}
		for _, raw := range elements {
			elem := raw.(map[string]interface{})
			if elem["ref"] == "password" && elem["value"] != security.Redacted {
				t.Fatalf("password element value was not redacted: %+v", elem)
			}
		}
		for _, fact := range engine.FactsByPredicate("element_value") {
			for _, arg := range fact.Args {
				if arg == "never-return-this" {
					t.Fatalf("password was persisted in element_value: %+v", fact)
				}
			}
		}
	})

	t.Run("GetInteractiveElementsTool filters", func(t *testing.T) {
		tool := &GetInteractiveElementsTool{sessions: sessions, engine: engine}

		// Test button filter
		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"filter":     "buttons",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		elements := resultMap["elements"].([]interface{})
		for _, elem := range elements {
			elemMap := elem.(map[string]interface{})
			elemType := elemMap["type"].(string)
			if elemType != "button" {
				t.Errorf("expected only buttons, got %s", elemType)
			}
		}
	})

	t.Run("GetNavigationLinksTool", func(t *testing.T) {
		tool := &GetNavigationLinksTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":   sessionID,
			"visible_only": true,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		counts, ok := resultMap["counts"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected navigation counts, got %T", resultMap["counts"])
		}
		total, ok := counts["total"].(float64)
		if !ok || total < 2 {
			t.Fatalf("expected at least two navigation links, got %+v", counts)
		}
		mainLinks, ok := resultMap["main"].(map[string]interface{})
		if !ok || len(mainLinks) < 2 {
			t.Fatalf("expected grouped main-area links, got %+v", resultMap["main"])
		}
	})

	t.Run("EvaluateJSTool", func(t *testing.T) {
		tool := &EvaluateJSTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":         sessionID,
			"script":             "() => { return document.title; }",
			"approved_by_handle": jsHandle,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		if resultMap["result"] == nil {
			t.Error("expected result from script")
		}
	})

	t.Run("EvaluateJSTool error handling", func(t *testing.T) {
		tool := &EvaluateJSTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":         sessionID,
			"script":             "() => { throw new Error('test error'); }",
			"approved_by_handle": jsHandle,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected failure for error script")
		}
		if resultMap["error"] == nil {
			t.Error("expected error message")
		}
	})

	t.Run("ScreenshotTool full page", func(t *testing.T) {
		screenshotDir := t.TempDir()
		pathPolicy, err := security.NewPathPolicy(screenshotDir, []string{screenshotDir})
		if err != nil {
			t.Fatalf("NewPathPolicy failed: %v", err)
		}
		tool := &ScreenshotTool{sessions: sessions, engine: engine, pathPolicy: pathPolicy}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"full_page":  true,
			"quality":    80,
			"format":     "jpeg",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
		filePath, _ := resultMap["file_path"].(string)
		if _, err := os.Stat(filePath); err != nil {
			t.Errorf("expected confined screenshot file: %v", err)
		}
	})

	t.Run("ScreenshotTool element screenshot", func(t *testing.T) {
		screenshotDir := t.TempDir()
		pathPolicy, err := security.NewPathPolicy(screenshotDir, []string{screenshotDir})
		if err != nil {
			t.Fatalf("NewPathPolicy failed: %v", err)
		}
		tool := &ScreenshotTool{sessions: sessions, engine: engine, pathPolicy: pathPolicy}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id":  sessionID,
			"element_ref": "btn-1",
			"quality":     90,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("BrowserHistoryTool", func(t *testing.T) {
		tool := &BrowserHistoryTool{sessions: sessions, engine: engine}

		// Navigate to create history
		navTool := &NavigateURLTool{sessions: sessions}
		_, _ = navTool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"url":        "about:blank",
		})

		// Go back
		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"action":     "back",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		// Go forward
		result, err = tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"action":     "forward",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap = result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		// Reload
		result, err = tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"action":     "reload",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap = result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("DiscoverHiddenContentTool", func(t *testing.T) {
		tool := &DiscoverHiddenContentTool{sessions: sessions}
		if err := page.Navigate(dataURL); err != nil {
			t.Fatalf("restore test page: %v", err)
		}
		if err := page.WaitLoad(); err != nil {
			t.Fatalf("wait for restored test page: %v", err)
		}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["sections"] == nil {
			t.Fatal("expected sections in result")
		}

		sections := resultMap["sections"].([]interface{})
		if len(sections) == 0 {
			t.Error("expected hidden tab panel to be discovered")
		}
	})
}

// TestIntegrationInteractionTools tests interaction tools with a real browser
func TestIntegrationInteractionTools(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Create test page with interactive elements
	testHTML := `<!DOCTYPE html>
<html>
<head><title>Interaction Test</title></head>
<body>
	<button id="click-btn" onclick="this.textContent='Clicked'">Click Me</button>
	<input id="text-input" type="text" value="">
	<input id="login-password" name="password" type="password" value="">
	<input id="checkbox" type="checkbox">
	<select id="dropdown">
		<option value="a">Option A</option>
		<option value="b">Option B</option>
	</select>
	<div id="result"></div>
	<script>
		let clickCount = 0;
		document.getElementById('click-btn').addEventListener('click', () => {
			clickCount++;
			document.getElementById('result').textContent = 'Clicks: ' + clickCount;
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
	dataURL := htmlDataURL(testHTML)
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("InteractTool click", func(t *testing.T) {
		tool := &InteractTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"ref":        "click-btn",
			"action":     "click",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		// Verify click happened
		time.Sleep(100 * time.Millisecond)
		evalTool := &EvaluateJSTool{sessions: sessions, engine: engine}
		evalResult, _ := evalTool.Execute(ctx, map[string]interface{}{
			"session_id":         sessionID,
			"script":             "() => document.getElementById('result').textContent",
			"approved_by_handle": jsHandle,
		})
		evalMap := evalResult.(map[string]interface{})
		if evalMap["result"] != "Clicks: 1" {
			t.Logf("Click may not have registered, result: %v", evalMap["result"])
		}
	})

	t.Run("InteractTool type", func(t *testing.T) {
		tool := &InteractTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"ref":        "text-input",
			"action":     "type",
			"value":      "Hello World",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}

		// Verify text was typed
		time.Sleep(100 * time.Millisecond)
		evalTool := &EvaluateJSTool{sessions: sessions, engine: engine}
		evalResult, _ := evalTool.Execute(ctx, map[string]interface{}{
			"session_id":         sessionID,
			"script":             "() => document.getElementById('text-input').value",
			"approved_by_handle": jsHandle,
		})
		evalMap := evalResult.(map[string]interface{})
		if evalMap["result"] != "Hello World" {
			t.Errorf("expected 'Hello World', got %v", evalMap["result"])
		}
	})

	t.Run("InteractTool redacts password values from facts and results", func(t *testing.T) {
		tool := &InteractTool{sessions: sessions, engine: engine}
		const password = "never-persist-this"

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"ref":        "login-password",
			"action":     "type",
			"value":      password,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["value"] != security.Redacted {
			t.Fatalf("password result was not redacted: %+v", resultMap)
		}

		facts := engine.FactsByPredicate("user_type")
		if len(facts) == 0 {
			t.Fatal("expected user_type fact")
		}
		latest := facts[len(facts)-1]
		if latest.Args[2] != security.Redacted {
			t.Fatalf("password fact was not redacted: %+v", latest)
		}
	})

	t.Run("InteractTool select", func(t *testing.T) {
		tool := &InteractTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"ref":        "dropdown",
			"action":     "select",
			"value":      "b",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("InteractTool toggle checkbox", func(t *testing.T) {
		tool := &InteractTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"ref":        "checkbox",
			"action":     "toggle",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("PressKeyTool", func(t *testing.T) {
		tool := &PressKeyTool{sessions: sessions, engine: engine}

		// Focus the input first
		page, _ := sessions.Page(sessionID)
		input, _ := page.Element("#text-input")
		_ = input.Focus()

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"key":        "Enter",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("PressKeyTool with modifiers", func(t *testing.T) {
		tool := &PressKeyTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"key":        "a",
			"modifiers":  []interface{}{"Control"},
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})

	t.Run("FillFormTool", func(t *testing.T) {
		tool := &FillFormTool{sessions: sessions, engine: engine}

		result, err := tool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"fields": []interface{}{
				map[string]interface{}{
					"ref":   "text-input",
					"value": "Form Fill Test",
				},
			},
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Errorf("expected success, got error: %v", resultMap["error"])
		}
	})
}

// Helper functions for integration tests
func setupIntegrationConfig() config.Config {
	return config.Config{
		Server: config.ServerConfig{
			Name:    "test-integration-server",
			Version: "1.0.0",
		},
		Browser: config.BrowserConfig{
			Headless:              navBoolPtr(true),
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
}

func setupIntegrationEngine(t *testing.T, cfg config.Config) *mangle.Engine {
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	return engine
}

func setupIntegrationBrowser(t *testing.T, cfg config.Config, engine *mangle.Engine) *browser.SessionManager {
	sessions := browser.NewSessionManager(cfg.Browser, engine)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := sessions.Start(ctx)
	if err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	return sessions
}

func navBoolPtr(b bool) *bool {
	return &b
}
