package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/config"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"gopkg.in/yaml.v3"
)

const nativeSmokeMarker = "NativeSmokeFailure"

type nativeLogFixture struct {
	dir        string
	configPath string
	app        *httptest.Server
	writeError chan error
}

// prepareNativeLogFixture runs before the MCP process starts, so the actual
// server binary must load and wire the file source from its own configuration.
func prepareNativeLogFixture() (*nativeLogFixture, error) {
	dir, err := os.MkdirTemp("", "browsernerd-native-smoke-")
	if err != nil {
		return nil, err
	}
	fixture := &nativeLogFixture{dir: dir, configPath: filepath.Join(dir, "profile.yaml"), writeError: make(chan error, 1)}
	logPath := filepath.Join(dir, "application.log")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/failure", func(w http.ResponseWriter, r *http.Request) {
		line := fmt.Sprintf("%s ERROR %s %s 500 %s: deterministic native exception\n", time.Now().Format(time.RFC3339Nano), r.Method, r.URL.Path, nativeSmokeMarker)
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			_, err = file.WriteString(line)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			select {
			case fixture.writeError <- err:
			default:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"request failed"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Native log smoke</title></head><body>
<button id="fail" onclick="fetch('/api/failure').then(r => document.getElementById('status').textContent = 'HTTP ' + r.status)">Run failing request</button>
<output id="status">Ready</output></body></html>`))
	})
	fixture.app = httptest.NewServer(mux)
	cfg := config.DefaultConfig()
	cfg.Server.LogFile = filepath.Join(dir, "mcp.log")
	cfg.Browser.AutoStart = false
	headless, multiTab, specsEnabled := true, false, false
	cfg.Browser.Headless = &headless
	cfg.Browser.MultiTabDefault = &multiTab
	cfg.Browser.SessionStore = filepath.Join(dir, "sessions.json")
	cfg.Browser.DownloadDir = filepath.Join(dir, "downloads")
	cfg.Docker.Enabled = false
	cfg.Docker.Containers = nil
	cfg.Logs.Files = []config.LogFileConfig{{Name: "local-api", Path: logPath}}
	cfg.Specs.Enabled = &specsEnabled
	cfg.Recorder.TraceDir = filepath.Join(dir, "traces")
	cfg.Security.WritableRoots = []string{dir}
	raw, err := yaml.Marshal(cfg)
	if err == nil {
		err = os.WriteFile(fixture.configPath, raw, 0o600)
	}
	if err != nil {
		fixture.close()
		return nil, fmt.Errorf("write native smoke profile: %w", err)
	}
	return fixture, nil
}

func (f *nativeLogFixture) close() {
	if f.app != nil {
		f.app.Close()
	}
	_ = os.RemoveAll(f.dir)
}

func smokeNativeLogs(ctx context.Context, client *mcpclient.Client, fixture *nativeLogFixture) error {
	if _, err := callJSON(ctx, client, "launch-browser", map[string]interface{}{}); err != nil {
		return err
	}
	created, err := callJSON(ctx, client, "browser-act", map[string]interface{}{
		"operations": []interface{}{map[string]interface{}{"type": "session_create", "url": fixture.app.URL + "/"}},
		"view":       "full", "include_specs": false,
	})
	if err != nil {
		return err
	}
	sessionID := sessionIDFrom(created)
	if sessionID == "" {
		return fmt.Errorf("native smoke session_create returned no session id: %v", created)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = callJSON(closeCtx, client, "close-session", map[string]interface{}{"session_id": sessionID})
	}()
	started := time.Now()
	_, err = callJSON(ctx, client, "browser-act", map[string]interface{}{
		"session_id": sessionID,
		"operations": []interface{}{
			map[string]interface{}{"type": "interact", "action": "click", "role": "button", "name": "Run failing request"},
			map[string]interface{}{"type": "await_stable", "timeout_ms": 5000},
		},
		"view": "full", "include_specs": false,
	})
	if err != nil {
		return err
	}
	reasonCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var reason map[string]interface{}
	var lines []string
	for {
		reason, err = callJSON(reasonCtx, client, "browser-reason", map[string]interface{}{
			"session_id": sessionID, "topic": "why_failed", "view": "full", "include_specs": false,
		})
		if err != nil {
			return err
		}
		lines = nativeBackendLines(reason)
		if nativeFailureObserved(reason) && nativeMarkerLine(lines) != "" {
			break
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-reasonCtx.Done():
			timer.Stop()
			raw, _ := json.Marshal(reason)
			return fmt.Errorf("native correlation did not report HTTP 500 and file-only marker: %s", clip(string(raw), 1500))
		case <-timer.C:
		}
	}
	select {
	case err := <-fixture.writeError:
		return fmt.Errorf("application log write failed: %w", err)
	default:
	}
	doctor, err := callJSON(ctx, client, "browser-observe", map[string]interface{}{"mode": "doctor", "view": "full"})
	if err != nil {
		return err
	}
	report, _ := doctor["data"].(map[string]interface{})
	resolvedConfig, _ := report["config"].(map[string]interface{})
	if !nativeSourceReported(report) || resolvedConfig["docker"] != false {
		return fmt.Errorf("doctor did not identify native source with Docker disabled: %v", doctor)
	}
	exportPath := filepath.Join(fixture.dir, "flight.jsonl")
	_, err = callJSON(ctx, client, "browser-mangle", map[string]interface{}{
		"operation": "export_flight", "session_id": sessionID, "include_server_logs": true,
		"path": exportPath, "view": "summary", "since_ms": started.Add(-time.Second).UnixMilli(), "until_ms": time.Now().Add(time.Second).UnixMilli(),
	})
	if err != nil {
		return err
	}
	flight, err := os.ReadFile(exportPath)
	if err != nil {
		return fmt.Errorf("read native flight export: %w", err)
	}
	if !nativeFlightEvidence(flight) {
		return fmt.Errorf("flight export omitted native source and file-only marker")
	}
	return writeJSON(map[string]interface{}{
		"success": true, "mode": "native-logs", "http_status": 500,
		"source": "local-api", "backend_log": nativeMarkerLine(lines),
		"doctor_native_source": true, "docker_enabled": false, "flight_export_native_log": true,
	})
}

func nativeFailureObserved(value interface{}) bool {
	switch value := value.(type) {
	case map[string]interface{}:
		url, _ := value["url"].(string)
		if value["status"] == float64(500) && strings.Contains(url, "/api/failure") {
			return true
		}
		for _, child := range value {
			if nativeFailureObserved(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range value {
			if nativeFailureObserved(child) {
				return true
			}
		}
	}
	return false
}

func nativeBackendLines(value interface{}) []string {
	var lines []string
	switch value := value.(type) {
	case map[string]interface{}:
		for key, child := range value {
			if key == "backend_logs" {
				lines = append(lines, nativeStringValues(child)...)
			} else {
				lines = append(lines, nativeBackendLines(child)...)
			}
		}
	case []interface{}:
		for _, child := range value {
			lines = append(lines, nativeBackendLines(child)...)
		}
	}
	return lines
}

func nativeStringValues(value interface{}) []string {
	var values []string
	switch value := value.(type) {
	case string:
		values = append(values, value)
	case []interface{}:
		for _, child := range value {
			values = append(values, nativeStringValues(child)...)
		}
	case map[string]interface{}:
		for _, child := range value {
			values = append(values, nativeStringValues(child)...)
		}
	}
	return values
}

func nativeMarkerLine(lines []string) string {
	for _, line := range lines {
		if strings.Contains(line, "local-api") && strings.Contains(line, nativeSmokeMarker) {
			return line
		}
	}
	return ""
}

func nativeSourceReported(value interface{}) bool {
	switch value := value.(type) {
	case map[string]interface{}:
		if value["type"] == "file" && value["name"] == "local-api" {
			return true
		}
		for _, child := range value {
			if nativeSourceReported(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range value {
			if nativeSourceReported(child) {
				return true
			}
		}
	}
	return false
}

func nativeFlightEvidence(contents []byte) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		var row map[string]interface{}
		if json.Unmarshal([]byte(line), &row) == nil && row["log_source"] == "local-api" && strings.Contains(line, nativeSmokeMarker) {
			return true
		}
	}
	return false
}
