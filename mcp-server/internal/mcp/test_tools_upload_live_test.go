package mcp

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// TestLiveFixtureUploadStepMeetsThePathPolicy proves a browser-test fixture's
// upload step runs under the same path policy as browser-act: a workspace file
// is set on the input, and a dot file is refused.
func TestLiveFixtureUploadStepMeetsThePathPolicy(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	defer func() { _ = manager.Shutdown(context.Background()) }()

	page := `<html><body><input type="file" data-testid="quote-file" onchange="document.getElementById('seen').textContent = this.files[0].name"><p id="seen"></p></body></html>`
	session, err := manager.CreateSession(ctx, "data:text/html,"+url.PathEscape(page))
	if err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "quote.csv"), []byte("sku,qty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := security.NewPathPolicy(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := &BrowserTestTool{sessions: manager, engine: engine, pathPolicy: policy, redactor: security.NewRedactor(nil)}

	run := func(file string) map[string]interface{} {
		res, err := tool.Execute(ctx, map[string]interface{}{
			"operation": "run",
			"test": map[string]interface{}{
				"name":       "upload a quote",
				"session_id": session.ID,
				"operations": []interface{}{
					map[string]interface{}{"type": "upload", "ref": "testid:quote-file", "files": []interface{}{file}},
				},
				"assertions": []interface{}{
					map[string]interface{}{"name": "upload recorded", "query": "user_upload(S, R, N, T)", "expect": "present"},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res.(map[string]interface{})
	}

	if res := run("quote.csv"); res["passed"] != true {
		t.Fatalf("a workspace file must upload through a fixture: %v", res)
	}
	rodPage, _ := manager.Page(session.ID)
	seen, err := rodPage.Timeout(5 * time.Second).Element("#seen")
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := seen.Text(); text != "quote.csv" {
		t.Fatalf("page saw %q, want quote.csv", text)
	}

	if res := run(".env"); res["passed"] == true {
		t.Fatalf("a dot file must be refused inside a fixture too: %v", res)
	}
}
