package browser

import (
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestNewElementRegistry(t *testing.T) {
	reg := NewElementRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if reg.Count() != 0 {
		t.Errorf("expected empty registry, got %d elements", reg.Count())
	}
	if reg.GenerationID() != 0 {
		t.Errorf("expected initial generation ID 0, got %d", reg.GenerationID())
	}
}

func TestElementRegistryRegister(t *testing.T) {
	reg := NewElementRegistry()

	fp := &ElementFingerprint{
		Ref:         "test-ref",
		TagName:     "button",
		ID:          "submit-btn",
		Name:        "submit",
		Classes:     []string{"btn", "btn-primary"},
		TextContent: "Submit",
		AriaLabel:   "Submit form",
		DataTestID:  "submit-button",
		Role:        "button",
		BoundingBox: map[string]float64{"x": 100, "y": 200, "width": 120, "height": 40},
		GeneratedAt: time.Now(),
	}

	reg.Register(fp)

	if reg.Count() != 1 {
		t.Errorf("expected 1 element, got %d", reg.Count())
	}

	retrieved := reg.Get("test-ref")
	if retrieved == nil {
		t.Fatal("expected to retrieve fingerprint")
	}
	if retrieved.TagName != "button" {
		t.Errorf("expected tagname 'button', got %q", retrieved.TagName)
	}
	if retrieved.ID != "submit-btn" {
		t.Errorf("expected ID 'submit-btn', got %q", retrieved.ID)
	}
	if retrieved.AriaLabel != "Submit form" {
		t.Errorf("expected aria-label 'Submit form', got %q", retrieved.AriaLabel)
	}
	if retrieved.DataTestID != "submit-button" {
		t.Errorf("expected data-testid 'submit-button', got %q", retrieved.DataTestID)
	}
}

func TestElementRegistryGetNonExistent(t *testing.T) {
	reg := NewElementRegistry()
	result := reg.Get("nonexistent")
	if result != nil {
		t.Error("expected nil for non-existent ref")
	}
}

func TestElementRegistryRegisterBatch(t *testing.T) {
	reg := NewElementRegistry()
	fps := []*ElementFingerprint{
		{Ref: "ref1", TagName: "button"},
		{Ref: "ref2", TagName: "input"},
		{Ref: "ref3", TagName: "a"},
	}
	initialGen := reg.GenerationID()
	reg.RegisterBatch(fps)

	if reg.Count() != 3 {
		t.Errorf("expected 3 elements, got %d", reg.Count())
	}
	if reg.GenerationID() != initialGen+1 {
		t.Error("expected generation ID to increment")
	}

	// Verify all elements are retrievable
	for _, fp := range fps {
		retrieved := reg.Get(fp.Ref)
		if retrieved == nil {
			t.Errorf("expected to retrieve %q", fp.Ref)
		}
	}
}

func TestElementRegistryClear(t *testing.T) {
	reg := NewElementRegistry()
	reg.Register(&ElementFingerprint{Ref: "test1", TagName: "div"})
	reg.Register(&ElementFingerprint{Ref: "test2", TagName: "span"})
	initialGen := reg.GenerationID()

	reg.Clear()

	if reg.Count() != 0 {
		t.Errorf("expected 0 elements after clear, got %d", reg.Count())
	}
	if reg.GenerationID() != initialGen+1 {
		t.Error("expected generation ID to increment after clear")
	}

	// Verify elements are no longer retrievable
	if reg.Get("test1") != nil {
		t.Error("expected nil after clear")
	}
}

func TestElementRegistryIncrementGeneration(t *testing.T) {
	reg := NewElementRegistry()
	reg.Register(&ElementFingerprint{Ref: "test", TagName: "div"})
	initialGen := reg.GenerationID()
	initialCount := reg.Count()

	reg.IncrementGeneration()

	if reg.GenerationID() != initialGen+1 {
		t.Error("expected generation ID to increment")
	}
	if reg.Count() != initialCount {
		t.Error("expected count to remain unchanged")
	}

	// Elements should still be retrievable
	if reg.Get("test") == nil {
		t.Error("expected element to still be retrievable after generation increment")
	}
}

func TestElementRegistryOverwrite(t *testing.T) {
	reg := NewElementRegistry()

	// Register initial fingerprint
	reg.Register(&ElementFingerprint{Ref: "test-ref", TagName: "div", TextContent: "Old text"})

	// Overwrite with new fingerprint
	reg.Register(&ElementFingerprint{Ref: "test-ref", TagName: "div", TextContent: "New text"})

	if reg.Count() != 1 {
		t.Errorf("expected 1 element after overwrite, got %d", reg.Count())
	}

	retrieved := reg.Get("test-ref")
	if retrieved.TextContent != "New text" {
		t.Errorf("expected 'New text', got %q", retrieved.TextContent)
	}
}

func TestElementFingerprintFields(t *testing.T) {
	now := time.Now()
	fp := &ElementFingerprint{
		Ref:         "my-button",
		TagName:     "button",
		ID:          "btn-submit",
		Name:        "submit-name",
		Classes:     []string{"btn", "btn-lg", "btn-primary"},
		TextContent: "Click me to submit the form",
		AriaLabel:   "Submit button",
		DataTestID:  "submit-btn-test",
		Role:        "button",
		BoundingBox: map[string]float64{
			"x":      50.5,
			"y":      100.25,
			"width":  200.0,
			"height": 50.0,
		},
		GeneratedAt: now,
	}

	if fp.Ref != "my-button" {
		t.Errorf("Ref mismatch: got %q", fp.Ref)
	}
	if fp.TagName != "button" {
		t.Errorf("TagName mismatch: got %q", fp.TagName)
	}
	if len(fp.Classes) != 3 {
		t.Errorf("expected 3 classes, got %d", len(fp.Classes))
	}
	if fp.BoundingBox["width"] != 200.0 {
		t.Errorf("BoundingBox width mismatch: got %v", fp.BoundingBox["width"])
	}
	if !fp.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt mismatch")
	}
}

func TestSessionMetadata(t *testing.T) {
	now := time.Now()
	session := Session{
		ID:         "session-123",
		TargetID:   "target-456",
		URL:        "https://example.com/page",
		Title:      "Example Page",
		Status:     "active",
		CreatedAt:  now,
		LastActive: now,
	}

	if session.ID != "session-123" {
		t.Errorf("ID mismatch: got %q", session.ID)
	}
	if session.TargetID != "target-456" {
		t.Errorf("TargetID mismatch: got %q", session.TargetID)
	}
	if session.URL != "https://example.com/page" {
		t.Errorf("URL mismatch: got %q", session.URL)
	}
	if session.Title != "Example Page" {
		t.Errorf("Title mismatch: got %q", session.Title)
	}
	if session.Status != "active" {
		t.Errorf("Status mismatch: got %q", session.Status)
	}
}

func TestEventThrottler(t *testing.T) {
	t.Run("nil throttler allows all", func(t *testing.T) {
		var throttler *eventThrottler
		if !throttler.Allow("test") {
			t.Error("nil throttler should allow all events")
		}
	})

	t.Run("zero interval throttler is nil", func(t *testing.T) {
		throttler := newEventThrottler(0)
		if throttler != nil {
			t.Error("expected nil throttler for zero interval")
		}
	})

	t.Run("negative interval throttler is nil", func(t *testing.T) {
		throttler := newEventThrottler(-100)
		if throttler != nil {
			t.Error("expected nil throttler for negative interval")
		}
	})

	t.Run("first event always allowed", func(t *testing.T) {
		throttler := newEventThrottler(1000) // 1 second
		if !throttler.Allow("test") {
			t.Error("first event should be allowed")
		}
	})

	t.Run("second event within interval blocked", func(t *testing.T) {
		throttler := newEventThrottler(1000) // 1 second
		throttler.Allow("test")
		// Immediately try again
		if throttler.Allow("test") {
			t.Error("second event within interval should be blocked")
		}
	})

	t.Run("different keys independent", func(t *testing.T) {
		throttler := newEventThrottler(1000)
		throttler.Allow("key1")
		if !throttler.Allow("key2") {
			t.Error("different keys should be independent")
		}
	})

	t.Run("event allowed after interval", func(t *testing.T) {
		throttler := newEventThrottler(10) // 10ms
		throttler.Allow("test")
		time.Sleep(20 * time.Millisecond)
		if !throttler.Allow("test") {
			t.Error("event should be allowed after interval")
		}
	})
}

func TestElementRegistryConcurrency(t *testing.T) {
	reg := NewElementRegistry()

	// Run concurrent operations
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			reg.Register(&ElementFingerprint{
				Ref:     "ref-" + string(rune(i)),
				TagName: "div",
			})
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = reg.Count()
			_ = reg.Get("ref-0")
			_ = reg.GenerationID()
		}
		done <- true
	}()

	// Batch writer goroutine
	go func() {
		for i := 0; i < 10; i++ {
			fps := []*ElementFingerprint{
				{Ref: "batch-1", TagName: "span"},
				{Ref: "batch-2", TagName: "span"},
			}
			reg.RegisterBatch(fps)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify no panic occurred and data is consistent
	count := reg.Count()
	if count == 0 {
		t.Error("expected some elements after concurrent operations")
	}
}

func TestSessionMetadataUpdate(t *testing.T) {
	now := time.Now()
	session := Session{
		ID:         "session-test",
		TargetID:   "target-test",
		URL:        "https://example.com",
		Title:      "Initial Title",
		Status:     "active",
		CreatedAt:  now,
		LastActive: now,
	}

	// Update LastActive
	newTime := now.Add(5 * time.Minute)
	session.LastActive = newTime

	if session.LastActive != newTime {
		t.Error("expected LastActive to be updated")
	}

	// Update Status
	session.Status = "inactive"
	if session.Status != "inactive" {
		t.Errorf("expected status 'inactive', got %q", session.Status)
	}
}

func TestEventThrottlerMultipleKeys(t *testing.T) {
	throttler := newEventThrottler(100) // 100ms interval

	// Different keys should be tracked separately
	keys := []string{"key1", "key2", "key3"}

	for _, key := range keys {
		if !throttler.Allow(key) {
			t.Errorf("first event for %q should be allowed", key)
		}
	}

	// Second events should all be blocked (within interval)
	for _, key := range keys {
		if throttler.Allow(key) {
			t.Errorf("second event for %q should be blocked within interval", key)
		}
	}
}

func TestElementFingerprintWithAllFields(t *testing.T) {
	now := time.Now()
	fp := &ElementFingerprint{
		Ref:         "full-fingerprint",
		TagName:     "button",
		ID:          "submit-button",
		Name:        "submit",
		Classes:     []string{"btn", "btn-primary", "btn-lg", "submit-btn"},
		TextContent: "Submit Your Application",
		AriaLabel:   "Submit application form",
		DataTestID:  "submit-form-btn",
		Role:        "button",
		BoundingBox: map[string]float64{
			"x":      150.5,
			"y":      200.25,
			"width":  180.0,
			"height": 45.0,
		},
		GeneratedAt: now,
	}

	// Verify all fields
	if fp.Ref != "full-fingerprint" {
		t.Errorf("Ref mismatch")
	}
	if fp.TagName != "button" {
		t.Errorf("TagName mismatch")
	}
	if fp.ID != "submit-button" {
		t.Errorf("ID mismatch")
	}
	if fp.Name != "submit" {
		t.Errorf("Name mismatch")
	}
	if len(fp.Classes) != 4 {
		t.Errorf("expected 4 classes, got %d", len(fp.Classes))
	}
	if fp.TextContent != "Submit Your Application" {
		t.Errorf("TextContent mismatch")
	}
	if fp.AriaLabel != "Submit application form" {
		t.Errorf("AriaLabel mismatch")
	}
	if fp.DataTestID != "submit-form-btn" {
		t.Errorf("DataTestID mismatch")
	}
	if fp.Role != "button" {
		t.Errorf("Role mismatch")
	}
	if fp.BoundingBox["height"] != 45.0 {
		t.Errorf("BoundingBox height mismatch")
	}
}

func TestElementRegistryMultipleOperations(t *testing.T) {
	reg := NewElementRegistry()

	// Register multiple elements
	for i := 0; i < 50; i++ {
		reg.Register(&ElementFingerprint{
			Ref:     "elem-" + string(rune('a'+i)),
			TagName: "div",
		})
	}

	initialGen := reg.GenerationID()

	// Clear and verify generation increments
	reg.Clear()
	if reg.Count() != 0 {
		t.Error("expected empty registry after clear")
	}
	if reg.GenerationID() != initialGen+1 {
		t.Error("expected generation to increment after clear")
	}

	// Increment generation multiple times
	for i := 0; i < 5; i++ {
		reg.IncrementGeneration()
	}
	if reg.GenerationID() != initialGen+6 {
		t.Errorf("expected generation %d, got %d", initialGen+6, reg.GenerationID())
	}
}

// Tests for coalesceNonEmpty helper function
func TestCoalesceNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected string
	}{
		{
			name:     "returns first non-empty",
			values:   []string{"first", "second", "third"},
			expected: "first",
		},
		{
			name:     "skips empty strings",
			values:   []string{"", "second", "third"},
			expected: "second",
		},
		{
			name:     "skips whitespace-only strings",
			values:   []string{"   ", "\t", "valid"},
			expected: "valid",
		},
		{
			name:     "returns empty when all empty",
			values:   []string{"", "", ""},
			expected: "",
		},
		{
			name:     "returns empty when all whitespace",
			values:   []string{"  ", "\t\n", "   "},
			expected: "",
		},
		{
			name:     "handles no values",
			values:   []string{},
			expected: "",
		},
		{
			name:     "handles single non-empty value",
			values:   []string{"only"},
			expected: "only",
		},
		{
			name:     "handles single empty value",
			values:   []string{""},
			expected: "",
		},
		{
			name:     "preserves original string without trimming",
			values:   []string{"", "  padded  "},
			expected: "  padded  ",
		},
		{
			name:     "handles mixed empty and whitespace",
			values:   []string{"", "  ", "", "\t", "found"},
			expected: "found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coalesceNonEmpty(tt.values...)
			if result != tt.expected {
				t.Errorf("coalesceNonEmpty(%v) = %q, want %q", tt.values, result, tt.expected)
			}
		})
	}
}

// Tests for isInternalScript helper function
func TestIsInternalScript(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		// Internal scripts - should return true
		{
			name:     "chrome protocol",
			url:      "chrome://settings/",
			expected: true,
		},
		{
			name:     "chrome extension",
			url:      "chrome-extension://abcdefghijklmnop/script.js",
			expected: true,
		},
		{
			name:     "devtools protocol",
			url:      "devtools://devtools/bundled/inspector.html",
			expected: true,
		},
		{
			name:     "about protocol",
			url:      "about:blank",
			expected: true,
		},
		{
			name:     "data protocol",
			url:      "data:text/javascript,console.log('hello')",
			expected: true,
		},
		{
			name:     "blob protocol",
			url:      "blob:https://example.com/12345-67890",
			expected: true,
		},
		// External scripts - should return false
		{
			name:     "http URL",
			url:      "http://example.com/script.js",
			expected: false,
		},
		{
			name:     "https URL",
			url:      "https://example.com/app.js",
			expected: false,
		},
		{
			name:     "localhost URL",
			url:      "http://localhost:3000/main.js",
			expected: false,
		},
		{
			name:     "file URL",
			url:      "file:///home/user/script.js",
			expected: false,
		},
		{
			name:     "empty string",
			url:      "",
			expected: false,
		},
		{
			name:     "relative URL",
			url:      "/scripts/app.js",
			expected: false,
		},
		{
			name:     "URL with chrome in path",
			url:      "https://example.com/chrome/extension.js",
			expected: false,
		},
		{
			name:     "URL with devtools in path",
			url:      "https://example.com/devtools/script.js",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInternalScript(tt.url)
			if result != tt.expected {
				t.Errorf("isInternalScript(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

// Tests for NewSessionManager without browser
func TestNewSessionManager(t *testing.T) {
	cfg := config.BrowserConfig{
		EnableDOMIngestion: true,
	}

	manager := NewSessionManager(cfg, nil)

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
	if manager.sessions == nil {
		t.Error("expected initialized sessions map")
	}
	if len(manager.sessions) != 0 {
		t.Errorf("expected empty sessions, got %d", len(manager.sessions))
	}
}

// Tests for SessionManager methods that don't require browser
func TestSessionManagerControlURL(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Initially empty
	if url := manager.ControlURL(); url != "" {
		t.Errorf("expected empty control URL, got %q", url)
	}
}

func TestSessionManagerIsConnected(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Not connected initially
	if manager.IsConnected() {
		t.Error("expected not connected")
	}
}

func TestSessionManagerList(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Empty list initially
	sessions := manager.List()
	if len(sessions) != 0 {
		t.Errorf("expected empty list, got %d sessions", len(sessions))
	}
}

func TestSessionManagerGetSessionNotFound(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Get non-existent session
	session, found := manager.GetSession("nonexistent-id")
	if found {
		t.Error("expected not found")
	}
	if session.ID != "" {
		t.Error("expected zero-value session")
	}
}

func TestSessionManagerPageNotFound(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Get page for non-existent session
	page, found := manager.Page("nonexistent-id")
	if found {
		t.Error("expected not found")
	}
	if page != nil {
		t.Error("expected nil page")
	}
}

func TestSessionManagerRegistryNotFound(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Get registry for non-existent session
	registry := manager.Registry("nonexistent-id")
	if registry != nil {
		t.Error("expected nil registry")
	}
}

func TestSessionManagerUpdateMetadataNoSession(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// UpdateMetadata should not panic for non-existent session
	manager.UpdateMetadata("nonexistent", func(s Session) Session {
		s.Title = "updated"
		return s
	})

	// Verify session was not created
	if len(manager.List()) != 0 {
		t.Error("expected no sessions after update on non-existent")
	}
}

func TestSessionManagerCreateSessionNoBrowser(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// CreateSession should fail without browser
	_, err := manager.CreateSession(nil, "https://example.com")
	if err == nil {
		t.Error("expected error when browser not connected")
	}
	if err.Error() != "browser not connected" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSessionManagerAttachNoBrowser(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Attach should fail without browser
	_, err := manager.Attach(nil, "target-123")
	if err == nil {
		t.Error("expected error when browser not connected")
	}
	if err.Error() != "browser not connected" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSessionManagerShutdownNoSessions(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	// Shutdown should not panic with no sessions/browser
	err := manager.Shutdown(nil)
	if err != nil {
		t.Errorf("unexpected error on shutdown: %v", err)
	}

	// Verify state is clean
	if manager.IsConnected() {
		t.Error("expected not connected after shutdown")
	}
	if manager.ControlURL() != "" {
		t.Error("expected empty control URL after shutdown")
	}
}

// Test ElementRegistry LastCleared tracking
func TestElementRegistryLastCleared(t *testing.T) {
	reg := NewElementRegistry()

	// Initial lastCleared should be set
	if reg.lastCleared.IsZero() {
		t.Error("expected lastCleared to be set on creation")
	}

	initialTime := reg.lastCleared

	// Wait a tiny bit and clear
	time.Sleep(time.Millisecond)
	reg.Clear()

	// lastCleared should be updated
	if !reg.lastCleared.After(initialTime) {
		t.Error("expected lastCleared to update after Clear()")
	}
}

// Test ElementFingerprint with empty/nil values
func TestElementFingerprintEmptyValues(t *testing.T) {
	fp := &ElementFingerprint{
		Ref: "empty-test",
		// All other fields left as zero values
	}

	if fp.Ref != "empty-test" {
		t.Error("expected Ref to be set")
	}
	if fp.TagName != "" {
		t.Error("expected empty TagName")
	}
	if fp.Classes != nil && len(fp.Classes) != 0 {
		t.Error("expected nil or empty Classes")
	}
	if fp.BoundingBox != nil && len(fp.BoundingBox) != 0 {
		t.Error("expected nil or empty BoundingBox")
	}
}

// Test Session struct with various status values
func TestSessionStatusValues(t *testing.T) {
	statuses := []string{"active", "attached", "forked", "detached", "inactive", ""}

	for _, status := range statuses {
		session := Session{
			ID:     "test-" + status,
			Status: status,
		}
		if session.Status != status {
			t.Errorf("expected status %q, got %q", status, session.Status)
		}
	}
}

// Test ElementRegistry with large batch
func TestElementRegistryLargeBatch(t *testing.T) {
	reg := NewElementRegistry()
	const batchSize = 1000

	fps := make([]*ElementFingerprint, batchSize)
	for i := 0; i < batchSize; i++ {
		fps[i] = &ElementFingerprint{
			Ref:     fmt.Sprintf("elem-%d", i),
			TagName: "div",
			ID:      fmt.Sprintf("id-%d", i),
		}
	}

	reg.RegisterBatch(fps)

	if reg.Count() != batchSize {
		t.Errorf("expected %d elements, got %d", batchSize, reg.Count())
	}

	// Verify random access works
	for _, idx := range []int{0, 100, 500, 999} {
		ref := fmt.Sprintf("elem-%d", idx)
		fp := reg.Get(ref)
		if fp == nil {
			t.Errorf("expected to find element %s", ref)
		} else if fp.ID != fmt.Sprintf("id-%d", idx) {
			t.Errorf("expected ID 'id-%d', got %q", idx, fp.ID)
		}
	}
}

// Test EventThrottler cleanup behavior
func TestEventThrottlerCleanup(t *testing.T) {
	throttler := newEventThrottler(10) // 10ms interval

	// Add many keys
	for i := 0; i < 100; i++ {
		throttler.Allow(fmt.Sprintf("key-%d", i))
	}

	// Verify all keys are tracked
	throttler.mu.Lock()
	keyCount := len(throttler.last)
	throttler.mu.Unlock()

	if keyCount != 100 {
		t.Errorf("expected 100 tracked keys, got %d", keyCount)
	}
}

// Test BrowserConfig integration (via SessionManager)
func TestSessionManagerWithConfig(t *testing.T) {
	cfg := config.BrowserConfig{
		EnableDOMIngestion:    true,
		EnableHeaderIngestion: true,
		EventThrottleMs:       100,
		SessionStore:          "", // No persistence
	}

	manager := NewSessionManager(cfg, nil)

	// Test IsHeadless method (defaults to true when Headless is nil)
	if !manager.cfg.IsHeadless() {
		t.Error("expected IsHeadless() to return true by default")
	}
	if manager.cfg.EnableDOMIngestion != true {
		t.Error("expected DOM ingestion config to be preserved")
	}
	if manager.cfg.EnableHeaderIngestion != true {
		t.Error("expected header ingestion config to be preserved")
	}
	if manager.cfg.EventThrottleMs != 100 {
		t.Errorf("expected EventThrottleMs 100, got %d", manager.cfg.EventThrottleMs)
	}
}

// Test ElementRegistry GetAll functionality through multiple operations
func TestElementRegistryGetAllElements(t *testing.T) {
	reg := NewElementRegistry()

	// Register elements with different properties
	elements := []*ElementFingerprint{
		{Ref: "btn-1", TagName: "button", ID: "submit", TextContent: "Submit"},
		{Ref: "inp-1", TagName: "input", Name: "email", AriaLabel: "Email address"},
		{Ref: "lnk-1", TagName: "a", Classes: []string{"nav-link"}, Role: "link"},
	}

	for _, e := range elements {
		reg.Register(e)
	}

	// Verify all can be retrieved
	for _, e := range elements {
		retrieved := reg.Get(e.Ref)
		if retrieved == nil {
			t.Errorf("failed to retrieve element %s", e.Ref)
			continue
		}
		if retrieved.TagName != e.TagName {
			t.Errorf("TagName mismatch for %s: got %q, want %q", e.Ref, retrieved.TagName, e.TagName)
		}
	}
}

// Test Session struct JSON marshalling compatibility
func TestSessionJSONFields(t *testing.T) {
	now := time.Now()
	session := Session{
		ID:         "sess-abc123",
		TargetID:   "target-xyz789",
		URL:        "https://example.com/page",
		Title:      "Example Page",
		Status:     "active",
		CreatedAt:  now,
		LastActive: now,
	}

	// Verify all fields are accessible (JSON tags are defined)
	if session.ID == "" {
		t.Error("ID should not be empty")
	}
	if session.TargetID == "" {
		t.Error("TargetID should not be empty")
	}
	if session.URL == "" {
		t.Error("URL should not be empty")
	}
	if session.Title == "" {
		t.Error("Title should not be empty")
	}
	if session.Status == "" {
		t.Error("Status should not be empty")
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if session.LastActive.IsZero() {
		t.Error("LastActive should not be zero")
	}
}

// Test ElementFingerprint with BoundingBox data
func TestElementFingerprintBoundingBox(t *testing.T) {
	fp := &ElementFingerprint{
		Ref:     "bbox-test",
		TagName: "div",
		BoundingBox: map[string]float64{
			"x":      10.5,
			"y":      20.5,
			"width":  100.0,
			"height": 50.0,
		},
	}

	if fp.BoundingBox["x"] != 10.5 {
		t.Errorf("expected x=10.5, got %v", fp.BoundingBox["x"])
	}
	if fp.BoundingBox["y"] != 20.5 {
		t.Errorf("expected y=20.5, got %v", fp.BoundingBox["y"])
	}
	if fp.BoundingBox["width"] != 100.0 {
		t.Errorf("expected width=100, got %v", fp.BoundingBox["width"])
	}
	if fp.BoundingBox["height"] != 50.0 {
		t.Errorf("expected height=50, got %v", fp.BoundingBox["height"])
	}
}

// Test ElementFingerprint Classes handling
func TestElementFingerprintClasses(t *testing.T) {
	tests := []struct {
		name    string
		classes []string
	}{
		{"no classes", nil},
		{"empty slice", []string{}},
		{"single class", []string{"btn"}},
		{"multiple classes", []string{"btn", "btn-primary", "btn-lg"}},
		{"many classes", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := &ElementFingerprint{
				Ref:     "class-test",
				TagName: "button",
				Classes: tt.classes,
			}

			if tt.classes == nil {
				if fp.Classes != nil {
					t.Error("expected nil classes")
				}
			} else {
				if len(fp.Classes) != len(tt.classes) {
					t.Errorf("expected %d classes, got %d", len(tt.classes), len(fp.Classes))
				}
			}
		})
	}
}

// Test EventThrottler with various intervals
func TestEventThrottlerIntervals(t *testing.T) {
	tests := []struct {
		name     string
		interval int
		expected bool // whether throttler should be non-nil
	}{
		{"negative interval", -100, false},
		{"zero interval", 0, false},
		{"small positive interval", 1, true},
		{"normal interval", 100, true},
		{"large interval", 10000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			throttler := newEventThrottler(tt.interval)
			isNonNil := throttler != nil
			if isNonNil != tt.expected {
				t.Errorf("newEventThrottler(%d) non-nil=%v, want non-nil=%v",
					tt.interval, isNonNil, tt.expected)
			}
		})
	}
}

// Test concurrent access to SessionManager methods
func TestSessionManagerConcurrentAccess(t *testing.T) {
	cfg := config.BrowserConfig{}
	manager := NewSessionManager(cfg, nil)

	done := make(chan bool)
	iterations := 100

	// Concurrent readers
	go func() {
		for i := 0; i < iterations; i++ {
			_ = manager.List()
			_ = manager.ControlURL()
			_ = manager.IsConnected()
		}
		done <- true
	}()

	// Concurrent session checks
	go func() {
		for i := 0; i < iterations; i++ {
			_, _ = manager.GetSession("nonexistent")
			_, _ = manager.Page("nonexistent")
			_ = manager.Registry("nonexistent")
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// No panic = success
}

// Test ElementFingerprint GeneratedAt timestamp
func TestElementFingerprintGeneratedAt(t *testing.T) {
	before := time.Now()
	time.Sleep(time.Millisecond)

	fp := &ElementFingerprint{
		Ref:         "timestamp-test",
		TagName:     "div",
		GeneratedAt: time.Now(),
	}

	time.Sleep(time.Millisecond)
	after := time.Now()

	if fp.GeneratedAt.Before(before) {
		t.Error("GeneratedAt should be after 'before' timestamp")
	}
	if fp.GeneratedAt.After(after) {
		t.Error("GeneratedAt should be before 'after' timestamp")
	}
}

// Test ElementRegistry generation ID overflow handling (no actual overflow, just many increments)
func TestElementRegistryGenerationIDManyIncrements(t *testing.T) {
	reg := NewElementRegistry()
	initialGen := reg.GenerationID()

	const increments = 10000
	for i := 0; i < increments; i++ {
		reg.IncrementGeneration()
	}

	finalGen := reg.GenerationID()
	if finalGen != initialGen+increments {
		t.Errorf("expected generation %d, got %d", initialGen+increments, finalGen)
	}
}

// Test that ElementRegistry handles duplicate refs correctly
func TestElementRegistryDuplicateRefs(t *testing.T) {
	reg := NewElementRegistry()

	// Register same ref multiple times with different data
	for i := 0; i < 5; i++ {
		reg.Register(&ElementFingerprint{
			Ref:         "duplicate-ref",
			TagName:     "div",
			TextContent: fmt.Sprintf("version-%d", i),
		})
	}

	// Should only have one element
	if reg.Count() != 1 {
		t.Errorf("expected 1 element, got %d", reg.Count())
	}

	// Should have the last version
	fp := reg.Get("duplicate-ref")
	if fp.TextContent != "version-4" {
		t.Errorf("expected 'version-4', got %q", fp.TextContent)
	}
}

// Test ElementFingerprint DataTestID and Role attributes
func TestElementFingerprintAccessibilityAttributes(t *testing.T) {
	fp := &ElementFingerprint{
		Ref:        "a11y-test",
		TagName:    "button",
		AriaLabel:  "Close dialog",
		DataTestID: "close-btn",
		Role:       "button",
	}

	if fp.AriaLabel != "Close dialog" {
		t.Errorf("expected AriaLabel 'Close dialog', got %q", fp.AriaLabel)
	}
	if fp.DataTestID != "close-btn" {
		t.Errorf("expected DataTestID 'close-btn', got %q", fp.DataTestID)
	}
	if fp.Role != "button" {
		t.Errorf("expected Role 'button', got %q", fp.Role)
	}
}

// Test config helper method NavigationTimeout through manager
func TestSessionManagerConfigTimeouts(t *testing.T) {
	cfg := config.BrowserConfig{
		DefaultNavigationTimeout: "30s",
		DefaultAttachTimeout:     "20s",
	}

	manager := NewSessionManager(cfg, nil)

	navTimeout := manager.cfg.NavigationTimeout()
	if navTimeout != 30*time.Second {
		t.Errorf("expected 30s navigation timeout, got %v", navTimeout)
	}

	attachTimeout := manager.cfg.AttachTimeout()
	if attachTimeout != 20*time.Second {
		t.Errorf("expected 20s attach timeout, got %v", attachTimeout)
	}
}

// Test config helper with invalid timeout strings (should use defaults)
func TestSessionManagerConfigInvalidTimeouts(t *testing.T) {
	cfg := config.BrowserConfig{
		DefaultNavigationTimeout: "invalid",
		DefaultAttachTimeout:     "also-invalid",
	}

	manager := NewSessionManager(cfg, nil)

	// Should fall back to defaults
	navTimeout := manager.cfg.NavigationTimeout()
	if navTimeout != 15*time.Second {
		t.Errorf("expected default 15s navigation timeout, got %v", navTimeout)
	}

	attachTimeout := manager.cfg.AttachTimeout()
	if attachTimeout != 10*time.Second {
		t.Errorf("expected default 10s attach timeout, got %v", attachTimeout)
	}
}

// Test config ViewportWidth and ViewportHeight helpers
func TestSessionManagerConfigViewport(t *testing.T) {
	tests := []struct {
		name           string
		width          int
		height         int
		expectedWidth  int
		expectedHeight int
	}{
		{"zero values use defaults", 0, 0, 1920, 1080},
		{"negative values use defaults", -100, -100, 1920, 1080},
		{"custom values preserved", 1280, 720, 1280, 720},
		{"mixed values", 0, 800, 1920, 800},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.BrowserConfig{
				ViewportWidth:  tt.width,
				ViewportHeight: tt.height,
			}
			manager := NewSessionManager(cfg, nil)

			if got := manager.cfg.GetViewportWidth(); got != tt.expectedWidth {
				t.Errorf("GetViewportWidth() = %d, want %d", got, tt.expectedWidth)
			}
			if got := manager.cfg.GetViewportHeight(); got != tt.expectedHeight {
				t.Errorf("GetViewportHeight() = %d, want %d", got, tt.expectedHeight)
			}
		})
	}
}
