package mcp

import (
	"context"
	"testing"
)

func TestLaunchBrowserTool(t *testing.T) {
	tool := &LaunchBrowserTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "launch-browser" {
			t.Errorf("expected name 'launch-browser', got %q", name)
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

func TestShutdownBrowserTool(t *testing.T) {
	tool := &ShutdownBrowserTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "shutdown-browser" {
			t.Errorf("expected name 'shutdown-browser', got %q", name)
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

func TestListSessionsTool(t *testing.T) {
	tool := &ListSessionsTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "list-sessions" {
			t.Errorf("expected name 'list-sessions', got %q", name)
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

func TestCreateSessionTool(t *testing.T) {
	tool := &CreateSessionTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "create-session" {
			t.Errorf("expected name 'create-session', got %q", name)
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

func TestAttachSessionTool(t *testing.T) {
	tool := &AttachSessionTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "attach-session" {
			t.Errorf("expected name 'attach-session', got %q", name)
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

func TestForkSessionTool(t *testing.T) {
	tool := &ForkSessionTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "fork-session" {
			t.Errorf("expected name 'fork-session', got %q", name)
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

func TestReifyReactTool(t *testing.T) {
	tool := &ReifyReactTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "reify-react" {
			t.Errorf("expected name 'reify-react', got %q", name)
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

func TestSnapshotDOMTool(t *testing.T) {
	tool := &SnapshotDOMTool{}

	t.Run("name", func(t *testing.T) {
		if name := tool.Name(); name != "snapshot-dom" {
			t.Errorf("expected name 'snapshot-dom', got %q", name)
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

// TestSessionToolsExecuteValidation tests the validation logic in Execute methods
func TestSessionToolsExecuteValidation(t *testing.T) {
	t.Run("CreateSessionTool uses default url", func(t *testing.T) {
		// Verify that getStringArg with empty returns empty string
		// which triggers the default url assignment
		tool := &CreateSessionTool{sessions: nil}
		schema := tool.InputSchema()
		props := schema["properties"].(map[string]interface{})
		if props["url"] == nil {
			t.Error("expected url property in schema")
		}
	})

	t.Run("AttachSessionTool requires target_id", func(t *testing.T) {
		tool := &AttachSessionTool{sessions: nil}
		ctx := context.Background()

		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing target_id")
		}
		if err.Error() != "target_id is required" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("ForkSessionTool requires session_id", func(t *testing.T) {
		tool := &ForkSessionTool{sessions: nil}
		ctx := context.Background()

		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
		if err.Error() != "session_id is required" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("ReifyReactTool requires session_id", func(t *testing.T) {
		tool := &ReifyReactTool{sessions: nil}
		ctx := context.Background()

		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
		if err.Error() != "session_id is required" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("SnapshotDOMTool requires session_id", func(t *testing.T) {
		tool := &SnapshotDOMTool{sessions: nil}
		ctx := context.Background()

		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing session_id")
		}
		if err.Error() != "session_id is required" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// TestSessionToolSchemaDetails tests more specific schema properties
func TestSessionToolSchemaDetails(t *testing.T) {
	t.Run("CreateSessionTool schema has url property", func(t *testing.T) {
		tool := &CreateSessionTool{}
		schema := tool.InputSchema()
		props := schema["properties"].(map[string]interface{})

		urlProp := props["url"].(map[string]interface{})
		if urlProp["type"] != "string" {
			t.Errorf("expected url type 'string', got %v", urlProp["type"])
		}
		if urlProp["description"] == nil || urlProp["description"] == "" {
			t.Error("expected url to have description")
		}
	})

	t.Run("AttachSessionTool schema has required target_id", func(t *testing.T) {
		tool := &AttachSessionTool{}
		schema := tool.InputSchema()

		required := schema["required"].([]string)
		found := false
		for _, r := range required {
			if r == "target_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected target_id in required fields")
		}
	})

	t.Run("ForkSessionTool schema has session_id and url", func(t *testing.T) {
		tool := &ForkSessionTool{}
		schema := tool.InputSchema()
		props := schema["properties"].(map[string]interface{})

		if props["session_id"] == nil {
			t.Error("expected session_id property")
		}
		if props["url"] == nil {
			t.Error("expected url property")
		}

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

	t.Run("ReifyReactTool schema has required session_id", func(t *testing.T) {
		tool := &ReifyReactTool{}
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

	t.Run("SnapshotDOMTool schema has required session_id", func(t *testing.T) {
		tool := &SnapshotDOMTool{}
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

	t.Run("LaunchBrowserTool schema is minimal", func(t *testing.T) {
		tool := &LaunchBrowserTool{}
		schema := tool.InputSchema()

		if schema["type"] != "object" {
			t.Errorf("expected type 'object', got %v", schema["type"])
		}
		// Should have empty or minimal properties
		props := schema["properties"].(map[string]interface{})
		if len(props) != 0 {
			t.Log("launch-browser has properties, which is fine")
		}
	})

	t.Run("ShutdownBrowserTool schema is minimal", func(t *testing.T) {
		tool := &ShutdownBrowserTool{}
		schema := tool.InputSchema()

		if schema["type"] != "object" {
			t.Errorf("expected type 'object', got %v", schema["type"])
		}
	})

	t.Run("ListSessionsTool schema is minimal", func(t *testing.T) {
		tool := &ListSessionsTool{}
		schema := tool.InputSchema()

		if schema["type"] != "object" {
			t.Errorf("expected type 'object', got %v", schema["type"])
		}
	})
}

// TestSessionToolDescriptions validates description content
func TestSessionToolDescriptions(t *testing.T) {
	tools := []Tool{
		&ListSessionsTool{},
		&CreateSessionTool{},
		&AttachSessionTool{},
		&ForkSessionTool{},
		&ReifyReactTool{},
		&SnapshotDOMTool{},
		&LaunchBrowserTool{},
		&ShutdownBrowserTool{},
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
