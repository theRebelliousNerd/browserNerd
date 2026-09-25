package mcp

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

type uploadFactSink struct {
	mu    sync.Mutex
	facts []mangle.Fact
}

func (s *uploadFactSink) AddFacts(_ context.Context, facts []mangle.Fact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = append(s.facts, facts...)
	return nil
}

// TestLiveUploadSetsAFileInput proves the upload op through a real browser: the
// page's own change handler sees the file, by name and size.
func TestLiveUploadSetsAFileInput(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, &uploadFactSink{})
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	defer func() { _ = manager.Shutdown(ctx) }()

	page := `<html><body>
<input type="file" data-testid="quote-file" onchange="document.getElementById('seen').textContent = this.files[0].name + ':' + this.files[0].size">
<input type="text" data-testid="not-a-file">
<p id="seen"></p>
</body></html>`
	session, err := manager.CreateSession(ctx, "data:text/html,"+url.PathEscape(page))
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	workspace := t.TempDir()
	quote := filepath.Join(workspace, "seedData", "vendor-quote.csv")
	if err := os.MkdirAll(filepath.Dir(quote), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(quote, []byte("sku,qty\nPUMP-7,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := security.NewPathPolicy(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := uploadFiles(ctx, manager, nil, policy, session.ID, "testid:quote-file",
		[]interface{}{filepath.Join("seedData", "vendor-quote.csv")})
	if err != nil || result["success"] != true {
		t.Fatalf("upload failed: %v %v", err, result)
	}
	rodPage, _ := manager.Page(session.ID)
	seen, err := rodPage.Timeout(5 * time.Second).Element("#seen")
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := seen.Text(); text != "vendor-quote.csv:17" {
		t.Fatalf("page saw %q, want vendor-quote.csv:17", text)
	}

	refused, _ := uploadFiles(ctx, manager, nil, policy, session.ID, "testid:quote-file", []interface{}{".env"})
	if refused["success"] != false {
		t.Fatalf("expected .env to be refused, got %v", refused)
	}
	notFile, _ := uploadFiles(ctx, manager, nil, policy, session.ID, "testid:not-a-file",
		[]interface{}{filepath.Join("seedData", "vendor-quote.csv")})
	if notFile["success"] != false {
		t.Fatalf("expected a text input to be refused, got %v", notFile)
	}
}
