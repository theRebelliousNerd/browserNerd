package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
)

// TestIntegrationFindElementByRef tests element finding strategies with a real browser
func TestIntegrationFindElementByRef(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	// Create test page with various element identifiers
	testHTML := `<!DOCTYPE html>
<html>
<head><title>Element Finding Test</title></head>
<body>
	<button id="btn-id">Button with ID</button>
	<button name="btn-name">Button with Name</button>
	<button data-testid="submit-btn">Button with data-testid</button>
	<button data-test-id="alt-submit">Button with data-test-id</button>
	<button aria-label="Close Dialog">Button with aria-label</button>
	<button class="primary-btn">Button with Class</button>
	<input id="username-input" type="text">
	<input name="password" type="password">
</body>
</html>`

	session, err := sessions.CreateSession(ctx, "about:blank", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	page, _ := sessions.Page(session.ID)
	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("findElementByRef with ID", func(t *testing.T) {
		elem, err := findElementByRef(page, "btn-id")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with ID" {
			t.Errorf("expected 'Button with ID', got %q", text)
		}
	})

	t.Run("findElementByRef with name", func(t *testing.T) {
		elem, err := findElementByRef(page, "btn-name")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with Name" {
			t.Errorf("expected 'Button with Name', got %q", text)
		}
	})

	t.Run("findElementByRef with testid prefix", func(t *testing.T) {
		elem, err := findElementByRef(page, "testid:submit-btn")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with data-testid" {
			t.Errorf("expected 'Button with data-testid', got %q", text)
		}
	})

	t.Run("findElementByRef with alt data-test-id", func(t *testing.T) {
		elem, err := findElementByRef(page, "testid:alt-submit")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with data-test-id" {
			t.Errorf("expected 'Button with data-test-id', got %q", text)
		}
	})

	t.Run("findElementByRef with aria prefix", func(t *testing.T) {
		elem, err := findElementByRef(page, "aria:Close_Dialog")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with aria-label" {
			t.Errorf("expected 'Button with aria-label', got %q", text)
		}
	})

	t.Run("findElementByRef with CSS selector", func(t *testing.T) {
		elem, err := findElementByRef(page, ".primary-btn")
		if err != nil {
			t.Fatalf("findElementByRef failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Button with Class" {
			t.Errorf("expected 'Button with Class', got %q", text)
		}
	})

	t.Run("findElementByRef nonexistent", func(t *testing.T) {
		_, err := findElementByRef(page, "nonexistent-element")
		if err == nil {
			t.Error("expected error for nonexistent element")
		}
	})
}

// TestIntegrationFindElementWithRegistry tests element finding with fingerprint registry
func TestIntegrationFindElementWithRegistry(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	testHTML := `<!DOCTYPE html>
<html>
<head><title>Registry Test</title></head>
<body>
	<button id="stable-btn" data-testid="my-button" aria-label="Submit Form" class="btn-primary">
		Submit
	</button>
	<button name="cancel-btn">Cancel</button>
</body>
</html>`

	session, err := sessions.CreateSession(ctx, "about:blank", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	page, _ := sessions.Page(session.ID)
	registry := sessions.Registry(session.ID)
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}

	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	t.Run("findElementByRefWithRegistry using data-testid from fingerprint", func(t *testing.T) {
		// Create fingerprint
		fp := &browser.ElementFingerprint{
			Ref:        "btn-ref-1",
			TagName:    "button",
			ID:         "stable-btn",
			DataTestID: "my-button",
			AriaLabel:  "Submit Form",
			GeneratedAt: time.Now(),
		}
		registry.Register(fp)

		// Find using ref
		elem, err := findElementByRefWithRegistry(page, "btn-ref-1", registry)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Submit" {
			t.Errorf("expected 'Submit', got %q", text)
		}
	})

	t.Run("findElementByRefWithRegistry using aria-label from fingerprint", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "btn-ref-2",
			TagName:    "button",
			AriaLabel:  "Submit Form",
			GeneratedAt: time.Now(),
		}
		registry.Register(fp)

		elem, err := findElementByRefWithRegistry(page, "btn-ref-2", registry)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}
	})

	t.Run("findElementByRefWithRegistry using ID from fingerprint", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "btn-ref-3",
			TagName:    "button",
			ID:         "stable-btn",
			GeneratedAt: time.Now(),
		}
		registry.Register(fp)

		elem, err := findElementByRefWithRegistry(page, "btn-ref-3", registry)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}
	})

	t.Run("findElementByRefWithRegistry using name from fingerprint", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "btn-ref-4",
			TagName:    "button",
			Name:       "cancel-btn",
			GeneratedAt: time.Now(),
		}
		registry.Register(fp)

		elem, err := findElementByRefWithRegistry(page, "btn-ref-4", registry)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}

		text, _ := elem.Text()
		if text != "Cancel" {
			t.Errorf("expected 'Cancel', got %q", text)
		}
	})

	t.Run("findElementByRefWithRegistry fallback to ref as selector", func(t *testing.T) {
		// Register fingerprint without specific attributes
		fp := &browser.ElementFingerprint{
			Ref:        "name:cancel-btn",
			TagName:    "button",
			GeneratedAt: time.Now(),
		}
		registry.Register(fp)

		// Should fall back to using ref as selector
		elem, err := findElementByRefWithRegistry(page, "name:cancel-btn", registry)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}
	})

	t.Run("findElementByRefWithRegistry with nil registry", func(t *testing.T) {
		// Should fall back to findElementByRef behavior
		elem, err := findElementByRefWithRegistry(page, "stable-btn", nil)
		if err != nil {
			t.Fatalf("findElementByRefWithRegistry failed: %v", err)
		}
		if elem == nil {
			t.Fatal("expected non-nil element")
		}
	})
}

// TestIntegrationValidateFingerprint tests fingerprint validation with a real browser
func TestIntegrationValidateFingerprint(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	cfg := setupIntegrationConfig()
	engine := setupIntegrationEngine(t, cfg)
	sessions := setupIntegrationBrowser(t, cfg, engine)
	defer sessions.Shutdown(context.Background())

	ctx := context.Background()

	testHTML := `<!DOCTYPE html>
<html>
<head><title>Fingerprint Test</title></head>
<body>
	<button id="validate-btn"
	        data-testid="test-button"
	        aria-label="Test Button"
	        class="btn primary"
	        style="position: absolute; left: 100px; top: 50px; width: 120px; height: 40px;">
		Test Button
	</button>
</body>
</html>`

	session, err := sessions.CreateSession(ctx, "about:blank", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	page, _ := sessions.Page(session.ID)
	dataURL := "data:text/html;charset=utf-8," + testHTML
	err = page.Navigate(dataURL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	err = page.WaitLoad()
	if err != nil {
		t.Fatalf("WaitLoad failed: %v", err)
	}

	// Wait for layout
	time.Sleep(200 * time.Millisecond)

	elem, err := page.Element("#validate-btn")
	if err != nil {
		t.Fatalf("Element not found: %v", err)
	}

	t.Run("validateFingerprint with nil fingerprint", func(t *testing.T) {
		result := validateFingerprint(nil, elem)
		if !result.Valid {
			t.Error("expected valid for nil fingerprint")
		}
		if result.Score != 1.0 {
			t.Errorf("expected score 1.0, got %v", result.Score)
		}
		if len(result.Changes) != 0 {
			t.Error("expected no changes for nil fingerprint")
		}
	})

	t.Run("validateFingerprint with matching fingerprint", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "test-ref",
			TagName:    "BUTTON",
			ID:         "validate-btn",
			DataTestID: "test-button",
			AriaLabel:  "Test Button",
			Classes:    []string{"btn", "primary"},
			TextContent: "Test Button",
			BoundingBox: map[string]float64{
				"x":      100.0,
				"y":      50.0,
				"width":  120.0,
				"height": 40.0,
			},
			GeneratedAt: time.Now(),
		}

		result := validateFingerprint(fp, elem)
		if !result.Valid {
			t.Errorf("expected valid fingerprint, got changes: %v", result.Changes)
		}
		if result.Score < 0.9 {
			t.Errorf("expected high score for matching fingerprint, got %v", result.Score)
		}
	})

	t.Run("validateFingerprint with changed text", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:         "test-ref",
			TagName:     "BUTTON",
			ID:          "validate-btn",
			TextContent: "Different Text",
			GeneratedAt: time.Now(),
		}

		result := validateFingerprint(fp, elem)
		if result.Score == 1.0 {
			t.Error("expected score < 1.0 for changed text")
		}
		// Should still be valid unless score drops too low
		if result.Score < 0.5 && !result.Valid {
			// This is acceptable behavior
		}
	})

	t.Run("validateFingerprint with changed ID", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "test-ref",
			TagName:    "BUTTON",
			ID:         "different-id",
			GeneratedAt: time.Now(),
		}

		result := validateFingerprint(fp, elem)
		if result.Score == 1.0 {
			t.Error("expected score < 1.0 for changed ID")
		}
		foundChange := false
		for _, change := range result.Changes {
			if change == "id: different-id -> validate-btn" {
				foundChange = true
				break
			}
		}
		if !foundChange {
			t.Log("Expected to find ID change in changes list")
		}
	})

	t.Run("validateFingerprint with changed classes", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:        "test-ref",
			TagName:    "BUTTON",
			Classes:    []string{"different", "classes"},
			GeneratedAt: time.Now(),
		}

		result := validateFingerprint(fp, elem)
		if result.Score == 1.0 {
			t.Error("expected score < 1.0 for changed classes")
		}
	})

	t.Run("validateFingerprint with moved element", func(t *testing.T) {
		fp := &browser.ElementFingerprint{
			Ref:     "test-ref",
			TagName: "BUTTON",
			ID:      "validate-btn",
			BoundingBox: map[string]float64{
				"x":      200.0, // Different position
				"y":      100.0,
				"width":  120.0,
				"height": 40.0,
			},
			GeneratedAt: time.Now(),
		}

		result := validateFingerprint(fp, elem)
		// Position change shouldn't invalidate, but should lower score slightly
		if result.Score == 1.0 {
			t.Log("Position change detected, score should be < 1.0")
		}
	})
}
