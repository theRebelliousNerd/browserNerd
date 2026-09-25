package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// probeStep is one tool call of the probe tour and what it cost.
type probeStep struct {
	Name    string                 `json:"name"`
	Tool    string                 `json:"tool"`
	Tokens  int                    `json:"tokens"`
	Millis  int64                  `json:"ms"`
	OK      bool                   `json:"ok"`
	Summary string                 `json:"summary,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Payload map[string]interface{} `json:"-"`
}

// probe drives testdata/probe through the real server binary over MCP stdio -
// the same transport an agent uses - and reports every call's token cost
// (response bytes / 4). It is the end-to-end proof for changes that unit and
// live package tests cover only in-process.
func probe(ctx context.Context, client *mcpclient.Client, verbose bool) error {
	baseURL, stop, err := serveProbe()
	if err != nil {
		return err
	}
	defer stop()

	steps := make([]probeStep, 0, 16)
	call := func(name, tool string, args map[string]interface{}) probeStep {
		step := measure(ctx, client, name, tool, args)
		steps = append(steps, step)
		if verbose {
			raw, _ := json.MarshalIndent(step.Payload, "", "  ")
			fmt.Fprintf(os.Stderr, "=== %s (%s, ~%d tokens)\n%s\n", name, tool, step.Tokens, raw)
		}
		return step
	}

	call("launch", "launch-browser", map[string]interface{}{})
	created := call("session_create", "browser-act", map[string]interface{}{
		"operations": []interface{}{map[string]interface{}{"type": "session_create", "url": baseURL + "/index.html"}},
	})
	sessionID := sessionIDFrom(created.Payload)
	if sessionID == "" {
		// A server that does not return the id: find the tab by its URL.
		listed, _ := callJSON(ctx, client, "browser-observe", map[string]interface{}{"mode": "sessions", "view": "full", "include_detached": true})
		sessionID = sessionByURL(listed, baseURL)
	}
	if sessionID == "" {
		return fmt.Errorf("session_create returned no session id: %v", created.Payload)
	}
	defer callJSON(context.Background(), client, "close-session", map[string]interface{}{"session_id": sessionID})
	time.Sleep(1500 * time.Millisecond)

	sid := func(args map[string]interface{}) map[string]interface{} {
		args["session_id"] = sessionID
		return args
	}
	call("observe default", "browser-observe", sid(map[string]interface{}{}))
	call("observe text", "browser-observe", sid(map[string]interface{}{"mode": "text"}))
	call("observe screenshot", "browser-observe", sid(map[string]interface{}{"mode": "screenshot"}))
	call("observe sessions", "browser-observe", map[string]interface{}{"mode": "sessions"})
	call("observe storage", "browser-observe", sid(map[string]interface{}{"mode": "storage"}))
	call("act fill+select+confirm+fail", "browser-act", sid(map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"ref": "qty", "value": "7"}}},
			map[string]interface{}{"type": "interact", "action": "select", "ref": "combo", "value": "Green"},
			map[string]interface{}{"type": "interact", "action": "click", "ref": "del"},
			map[string]interface{}{"type": "interact", "action": "click", "ref": "failbtn"},
			map[string]interface{}{"type": "sleep", "duration_ms": 800},
		},
		"stop_on_error": false,
	}))
	call("reason why_failed", "browser-reason", sid(map[string]interface{}{"topic": "why_failed"}))
	call("reason network", "browser-reason", sid(map[string]interface{}{"topic": "network", "view": "summary"}))
	call("reason health", "browser-reason", sid(map[string]interface{}{"topic": "health"}))
	call("mangle schema", "browser-mangle", map[string]interface{}{"operation": "schema", "filter": "net_"})
	call("mangle join", "browser-mangle", map[string]interface{}{
		"operation": "query",
		"query":     "net_request(S, R, M, U, I, T), net_response(S, R, 500, L, D).",
	})
	call("observe doctor", "browser-observe", map[string]interface{}{"mode": "doctor"})

	total := 0
	for _, s := range steps {
		total += s.Tokens
	}
	return writeJSON(map[string]interface{}{
		"base_url":     baseURL,
		"session_id":   sessionID,
		"steps":        steps,
		"total_tokens": total,
	})
}

func measure(ctx context.Context, client *mcpclient.Client, name, tool string, args map[string]interface{}) probeStep {
	started := time.Now()
	step := probeStep{Name: name, Tool: tool}
	result, err := client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool, Arguments: args}})
	step.Millis = time.Since(started).Milliseconds()
	if err != nil {
		step.Error = err.Error()
		return step
	}
	for _, content := range result.Content {
		text, ok := mcp.AsTextContent(content)
		if !ok {
			continue
		}
		step.Tokens += len(text.Text) / 4
		var payload map[string]interface{}
		if json.Unmarshal([]byte(text.Text), &payload) == nil {
			step.Payload = payload
		}
	}
	step.OK = !result.IsError
	if step.Payload != nil {
		if s, ok := step.Payload["success"].(bool); ok && !s {
			step.OK = false
		}
		if s, ok := step.Payload["summary"].(string); ok {
			step.Summary = clip(s, 160)
		}
		if e, ok := step.Payload["error"].(string); ok {
			step.Error = clip(e, 200)
		}
	}
	return step
}

// sessionIDFrom finds the session a browser-act session_create opened.
func sessionIDFrom(payload map[string]interface{}) string {
	if id, _ := payload["session_id"].(string); id != "" {
		return id
	}
	if id := sessionIDFromAct(payload); id != "" {
		return id
	}
	rows, _ := payload["results"].([]interface{})
	for _, raw := range rows {
		row, _ := raw.(map[string]interface{})
		if id, _ := row["session_id"].(string); id != "" {
			return id
		}
		res, _ := row["result"].(map[string]interface{})
		if id, _ := res["session_id"].(string); id != "" {
			return id
		}
	}
	return ""
}

// sessionByURL finds a listed session whose URL starts with prefix.
func sessionByURL(payload map[string]interface{}, prefix string) string {
	var sessions []interface{}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		sessions, _ = data["sessions"].([]interface{})
	}
	for _, raw := range sessions {
		s, _ := raw.(map[string]interface{})
		if url, _ := s["url"].(string); strings.HasPrefix(url, prefix) {
			if id, _ := s["id"].(string); id != "" {
				return id
			}
		}
	}
	return ""
}

// serveProbe serves testdata/probe with the endpoints the page calls.
func serveProbe() (string, func(), error) {
	dir, err := filepath.Abs(filepath.Join("testdata", "probe"))
	if err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return "", nil, fmt.Errorf("probe page not found (run from mcp-server/): %w", err)
	}
	writeJSONResponse := func(w http.ResponseWriter, status int, body interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/poll", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	})
	mux.HandleFunc("/api/fail", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusInternalServerError, map[string]interface{}{"error": "boom", "detail": "database unavailable"})
	})
	mux.HandleFunc("/api/missing", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})
	})
	mux.HandleFunc("/api/slow", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2500 * time.Millisecond)
		writeJSONResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	})
	mux.HandleFunc("/download.csv", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="report.csv"`)
		_, _ = w.Write([]byte("a,b\n1,2\n"))
	})
	mux.Handle("/", http.FileServer(http.Dir(dir)))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() { _ = srv.Close() }, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
