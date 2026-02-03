package mcp

import (
	"testing"
)

func TestGetPageStateTool(t *testing.T) {
	tool := &GetPageStateTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "get-page-state" {
			t.Errorf("expected name 'get-page-state', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})
}

func TestNavigateURLTool(t *testing.T) {
	tool := &NavigateURLTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "navigate-url" {
			t.Errorf("expected name 'navigate-url', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Error("expected properties in schema")
		}
		if props["url"] == nil {
			t.Error("expected url property in schema")
		}
	})
}

func TestBrowserHistoryTool(t *testing.T) {
	tool := &BrowserHistoryTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "browser-history" {
			t.Errorf("expected name 'browser-history', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})
}

func TestInteractTool(t *testing.T) {
	tool := &InteractTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "interact" {
			t.Errorf("expected name 'interact', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})
}

func TestFillFormTool(t *testing.T) {
	tool := &FillFormTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "fill-form" {
			t.Errorf("expected name 'fill-form', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})
}

func TestPressKeyTool(t *testing.T) {
	tool := &PressKeyTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "press-key" {
			t.Errorf("expected name 'press-key', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})
}

func TestGetInteractiveElementsTool(t *testing.T) {
	tool := &GetInteractiveElementsTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "get-interactive-elements" {
			t.Errorf("expected name 'get-interactive-elements', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})
}

func TestGetNavigationLinksTool(t *testing.T) {
	tool := &GetNavigationLinksTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "get-navigation-links" {
			t.Errorf("expected name 'get-navigation-links', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})
}

func TestEvaluateJSTool(t *testing.T) {
	tool := &EvaluateJSTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "evaluate-js" {
			t.Errorf("expected name 'evaluate-js', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})
}

func TestScreenshotTool(t *testing.T) {
	tool := &ScreenshotTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "screenshot" {
			t.Errorf("expected name 'screenshot', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})
}

func TestDiscoverHiddenContentTool(t *testing.T) {
	tool := &DiscoverHiddenContentTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "discover-hidden-content" {
			t.Errorf("expected name 'discover-hidden-content', got %q", name)
		}
	})

	t.Run("description", func(t *testing.T) {
		if desc := tool.Description(); desc == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})
}

// TestNavigationToolSchemaDetails tests specific schema properties of navigation tools
func TestNavigationToolSchemaDetails(t *testing.T) {
	t.Run("GetPageStateTool requires session_id", func(t *testing.T) {
		tool := &GetPageStateTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		found := false
		for _, r := range required {
			if r == "session_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected session_id in required fields")
		}
	})

	t.Run("NavigateURLTool requires session_id and url", func(t *testing.T) {
		tool := &NavigateURLTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		foundSessionID := false
		foundURL := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
			if r == "url" {
				foundURL = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
		if !foundURL {
			t.Error("expected url in required fields")
		}

		// Check wait_until enum
		props := schema["properties"].(map[string]interface{})
		waitUntil := props["wait_until"].(map[string]interface{})
		if waitUntil["enum"] == nil {
			t.Error("expected enum for wait_until")
		}
	})

	t.Run("BrowserHistoryTool requires session_id and action", func(t *testing.T) {
		tool := &BrowserHistoryTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		foundSessionID := false
		foundAction := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
			if r == "action" {
				foundAction = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
		if !foundAction {
			t.Error("expected action in required fields")
		}

		// Check action enum
		props := schema["properties"].(map[string]interface{})
		action := props["action"].(map[string]interface{})
		if action["enum"] == nil {
			t.Error("expected enum for action")
		}
	})

	t.Run("PressKeyTool requires session_id and key", func(t *testing.T) {
		tool := &PressKeyTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		foundSessionID := false
		foundKey := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
			if r == "key" {
				foundKey = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
		if !foundKey {
			t.Error("expected key in required fields")
		}

		// Check modifiers property
		props := schema["properties"].(map[string]interface{})
		modifiers := props["modifiers"].(map[string]interface{})
		if modifiers["type"] != "array" {
			t.Errorf("expected modifiers type 'array', got %v", modifiers["type"])
		}
	})

	t.Run("InteractTool requires session_id and ref", func(t *testing.T) {
		tool := &InteractTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		foundSessionID := false
		foundRef := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
			if r == "ref" {
				foundRef = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
		if !foundRef {
			t.Error("expected ref in required fields")
		}
	})

	t.Run("FillFormTool has fields property", func(t *testing.T) {
		tool := &FillFormTool{}
		schema := tool.InputSchema()

		props := schema["properties"].(map[string]interface{})
		if props["session_id"] == nil {
			t.Error("expected session_id property")
		}
		if props["fields"] == nil {
			t.Error("expected fields property")
		}
	})

	t.Run("EvaluateJSTool requires session_id and script", func(t *testing.T) {
		tool := &EvaluateJSTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		foundSessionID := false
		foundScript := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
			if r == "script" {
				foundScript = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
		if !foundScript {
			t.Error("expected script in required fields")
		}
	})

	t.Run("ScreenshotTool has all expected properties", func(t *testing.T) {
		tool := &ScreenshotTool{}
		schema := tool.InputSchema()

		props := schema["properties"].(map[string]interface{})
		// Based on actual schema: session_id, element_ref, full_page, quality, format, save_path, return_base64
		expectedProps := []string{"session_id", "element_ref", "quality", "format"}
		for _, prop := range expectedProps {
			if props[prop] == nil {
				t.Errorf("expected %s property in schema", prop)
			}
		}

		// Verify required field
		required := schema["required"].([]string)
		foundSessionID := false
		for _, r := range required {
			if r == "session_id" {
				foundSessionID = true
			}
		}
		if !foundSessionID {
			t.Error("expected session_id in required fields")
		}
	})

	t.Run("GetInteractiveElementsTool has session_id", func(t *testing.T) {
		tool := &GetInteractiveElementsTool{}
		schema := tool.InputSchema()

		props := schema["properties"].(map[string]interface{})
		if props["session_id"] == nil {
			t.Error("expected session_id property")
		}
	})

	t.Run("GetNavigationLinksTool has session_id", func(t *testing.T) {
		tool := &GetNavigationLinksTool{}
		schema := tool.InputSchema()

		props := schema["properties"].(map[string]interface{})
		if props["session_id"] == nil {
			t.Error("expected session_id property")
		}
	})
}

// TestNavigationToolDescriptions validates description content quality
func TestNavigationToolDescriptions(t *testing.T) {
	tools := []Tool{
		&GetPageStateTool{},
		&NavigateURLTool{},
		&BrowserHistoryTool{},
		&InteractTool{},
		&FillFormTool{},
		&PressKeyTool{},
		&GetInteractiveElementsTool{},
		&GetNavigationLinksTool{},
		&EvaluateJSTool{},
		&ScreenshotTool{},
		&DiscoverHiddenContentTool{},
	}

	for _, tool := range tools {
		t.Run(tool.Name()+"_description", func(t *testing.T) {
			desc := tool.Description()
			if len(desc) < 20 {
				t.Errorf("description too short for tool %s: %q", tool.Name(), desc)
			}
		})
	}
}

// Note: Execute() error validation tests are skipped because they require a real
// session manager and browser. These tests would cause panics with nil sessions.
// Error validation is covered by integration tests with live browsers.
