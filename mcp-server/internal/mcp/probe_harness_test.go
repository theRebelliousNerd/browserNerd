package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// probeServer serves testdata/probe with the API endpoints the probe page
// calls: a steady 200 poll, a 500 with a JSON body, a 404, a 2.5 s response,
// and a CSV attachment.
func probeServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "probe"))
	if err != nil {
		t.Fatal(err)
	}
	writeJSON := func(w http.ResponseWriter, status int, body interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/poll", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	})
	mux.HandleFunc("/api/fail", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "boom", "detail": "database unavailable"})
	})
	mux.HandleFunc("/api/missing", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})
	})
	mux.HandleFunc("/api/slow", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2500 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	})
	mux.HandleFunc("/download.csv", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="report.csv"`)
		_, _ = w.Write([]byte("a,b\n1,2\n"))
	})
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// liveProbe is a headless browser, a real Mangle engine fed by its event
// stream, and one session open on the probe page.
type liveProbe struct {
	ctx      context.Context
	manager  *browser.SessionManager
	engine   *mangle.Engine
	session  string
	baseURL  string
	serverFn *httptest.Server
}

// startLiveProbe opens the probe page in headless Chrome. It skips when
// SKIP_LIVE_TESTS is set or Chrome cannot start.
func startLiveProbe(t *testing.T) *liveProbe {
	t.Helper()
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	engine, err := mangle.NewEngine(config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 4096,
	})
	if err != nil {
		t.Fatalf("mangle engine: %v", err)
	}
	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	srv := probeServer(t)
	session, err := manager.CreateSession(ctx, srv.URL+"/index.html")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	// Let the page run its load-time fetches and first polls.
	time.Sleep(1500 * time.Millisecond)
	return &liveProbe{ctx: ctx, manager: manager, engine: engine, session: session.ID, baseURL: srv.URL, serverFn: srv}
}

// responseTokens estimates the tokens a tool result costs an agent: its JSON
// size over four.
func responseTokens(t *testing.T, result interface{}) int {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return len(raw) / 4
}
