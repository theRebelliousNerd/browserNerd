package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/logs"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// The real server must construct the native reader and expose its evidence
// through MCP; injecting a source into BrowserReasonTool would miss that seam.
func TestNativeLogsReachReasonAndExportOverMCP(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "service.log")
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line, err := json.Marshal(map[string]interface{}{"timestamp": time.Now().Format(time.RFC3339Nano), "level": "ERROR", "message": "GET " + r.URL.Path + " RuntimeError: inventory unavailable password=native-secret", "password": "json-native-secret", "api_key": "json-api-secret"})
		if err != nil {
			t.Errorf("encode application log: %v", err)
			return
		}
		if err := os.WriteFile(logPath, append(line, '\n'), 0o600); err != nil {
			t.Errorf("write application log: %v", err)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"inventory unavailable"}`))
	}))
	defer app.Close()
	at := time.Now()
	response, err := http.Get(app.URL + "/items/reserve")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 500 {
		t.Fatalf("fixture returned %d, want 500", response.StatusCode)
	}

	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	cfg.Docker.Enabled = false
	cfg.Security.BaseDir = dir
	cfg.Security.WritableRoots = []string{dir}
	cfg.Logs.Files = []config.LogFileConfig{{Name: "inventory", Path: logPath}}
	port := startHTTPForTest(t, cfg)
	client, err := mcpclient.NewStreamableHttpClient("http://localhost:" + strconv.Itoa(port) + httpEndpointPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Initialize(ctx, mcptypes.InitializeRequest{Params: mcptypes.InitializeParams{
		ProtocolVersion: mcptypes.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcptypes.Implementation{Name: "native-log-test", Version: "1"},
	}}); err != nil {
		t.Fatal(err)
	}
	call := func(name string, args map[string]interface{}) (map[string]interface{}, string) {
		t.Helper()
		result, err := client.CallTool(ctx, mcptypes.CallToolRequest{Params: mcptypes.CallToolParams{Name: name, Arguments: args}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError || len(result.Content) != 1 {
			t.Fatalf("%s failed: %+v", name, result)
		}
		content, ok := result.Content[0].(mcptypes.TextContent)
		if !ok {
			t.Fatalf("unexpected content: %T", result.Content[0])
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(content.Text), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["success"] == false {
			t.Fatalf("%s failed: %s", name, content.Text)
		}
		if strings.Contains(content.Text, "native-secret") || strings.Contains(content.Text, "json-api-secret") {
			t.Fatalf("%s disclosed a log credential: %s", name, content.Text)
		}
		return decoded, content.Text
	}
	call("browser-mangle", map[string]interface{}{"operation": "push", "facts": []interface{}{
		map[string]interface{}{"predicate": "net_request", "args": []interface{}{"native-session", "request-1", "GET", app.URL + "/items/reserve", "", at.UnixMilli()}},
		map[string]interface{}{"predicate": "net_response", "args": []interface{}{"native-session", "request-1", 500, 0, 0}},
	}})
	reason, raw := call("browser-reason", map[string]interface{}{"session_id": "native-session", "topic": "why_failed", "view": "full"})
	if !strings.Contains(raw, "inventory") || !strings.Contains(raw, "RuntimeError: inventory unavailable") || asMap(reason["root_cause"])["backend_logs"] == nil {
		t.Fatalf("native log evidence did not reach the real MCP reason tool: %s", raw)
	}
	exportPath := filepath.Join(dir, "flight.jsonl")
	_, raw = call("browser-mangle", map[string]interface{}{"operation": "export_flight", "include_server_logs": true, "view": "full", "path": exportPath, "since_ms": at.Add(-time.Second).UnixMilli(), "until_ms": time.Now().Add(time.Second).UnixMilli()})
	if !strings.Contains(raw, "server_log_rows") || !strings.Contains(raw, "inventory unavailable") {
		t.Fatalf("native evidence missing from flight export response: %s", raw)
	}
	contents, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"log_source":"inventory"`) || (strings.Contains(string(contents), "native-secret") || strings.Contains(string(contents), "json-api-secret")) {
		t.Fatalf("export must include redacted native source evidence: %s", contents)
	}
	_, raw = call("browser-observe", map[string]interface{}{"mode": "doctor"})
	if !strings.Contains(raw, `"type":"file"`) || !strings.Contains(raw, "inventory") || strings.Contains(raw, "RuntimeError") {
		t.Fatalf("doctor must report configured source identity without log contents: %s", raw)
	}
}

func TestNativeLogsReachRegisteredDiagnosticWithPartialFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.log")
	at := time.Now()
	if err := os.WriteFile(path, []byte(at.Format(time.RFC3339Nano)+" ERROR GET /api/fail WorkerError: retry failed password=diagnostic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	cfg.Docker.Enabled = false
	progressive := false
	cfg.MCP.ProgressiveOnly = &progressive
	cfg.Logs.Files = []config.LogFileConfig{{Name: "worker", Path: path}, {Name: "missing", Path: filepath.Join(dir, "absent.log")}}
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("native-diagnostic")
	p.base = at.Add(-time.Second)
	p.request("request-1", "GET", "http://example.test/api/fail", 1000, 500, 1)
	loadFacts(t, engine, p)
	server, err := NewServer(cfg, browser.NewSessionManager(cfg.Browser, engine), engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	result, err := server.ExecuteTool("get-console-errors", map[string]interface{}{"session_id": "native-diagnostic"})
	if err != nil {
		t.Fatal(err)
	}
	data := throughServerRedactor(t, "get-console-errors", result)
	raw, _ := json.Marshal(data)
	if !strings.Contains(string(raw), "WorkerError") || !strings.Contains(string(raw), "missing") || strings.Contains(string(raw), "diagnostic-secret") || data["server_log_error"] == nil {
		t.Fatalf("diagnostic must preserve evidence plus redacted partial failure: %s", raw)
	}
	reason := server.tools["browser-reason"].(*BrowserReasonTool)
	responseMap := runReason(t, reason, map[string]interface{}{"session_id": "native-diagnostic", "topic": "why_failed"})
	lines := toStringSlice(asMap(responseMap["root_cause"])["backend_logs"])
	if len(lines) < 2 || !strings.Contains(strings.Join(lines, " "), "partial:") {
		t.Fatalf("partial native read must preserve matching lines and limitations: %v", lines)
	}

}

func TestReasonReportsNativeTruncationAlongsideEvidence(t *testing.T) {
	at := time.Now()
	source := &fakeLogs{logs: []logs.Entry{{Source: "worker", Timestamp: at, Level: "ERROR", Message: "GET /api/fail JobError: failed"}}, err: &logs.Issue{Source: "worker", Kind: logs.Truncated}}
	var correlator backendCorrelator
	result := correlator.linesFor(context.Background(), source, &netCall{url: "http://example.test/api/fail", startMs: at.UnixMilli()})
	raw := fmt.Sprint(result)
	if !strings.Contains(raw, "JobError") || !strings.Contains(raw, "truncated") {
		t.Fatalf("bounded source limitation was discarded: %v", result)
	}
}

func TestNativeLogAppendRemainsVisibleAfterEmptyReadAndDockerFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	files := logs.NewFiles([]logs.File{{Name: "worker", Path: path}}, time.Minute, 4096, 10)
	broken := &fakeLogs{err: errors.New("docker logs worker-container: daemon is running? connection refused")}
	source := logs.NewComposite([]logs.Source{files, broken}, time.Minute)
	var correlator backendCorrelator
	at := time.Now()
	request := &netCall{url: "http://example.test/api/reserve", startMs: at.UnixMilli()}
	first := correlator.linesFor(context.Background(), source, request)
	if !strings.Contains(fmt.Sprint(first), "unavailable") {
		t.Fatalf("first collection must disclose the Docker failure: %v", first)
	}
	if err := os.WriteFile(path, []byte(at.Format(time.RFC3339Nano)+" ERROR GET /api/reserve NativeError: available after append\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := correlator.linesFor(context.Background(), source, request)
	if !strings.Contains(fmt.Sprint(second), "NativeError: available after append") || !strings.Contains(fmt.Sprint(second), "partial:") {
		t.Fatalf("a failed transport suppressed newly appended healthy evidence: %v", second)
	}
}

func TestServerLogWindowPreservesLargestConfiguredSource(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	cfg.Docker.Enabled = true
	cfg.Docker.Containers = []string{"generic-container"}
	cfg.Docker.LogWindow = "60s"
	server, err := NewServer(cfg, nil, testMangleEngineForProgressive(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if server.logSource.LogWindow() != time.Minute || server.tools["browser-mangle"].(*BrowserMangleTool).defaultLogWindow != time.Minute {
		t.Fatal("the native default shortened the configured Docker correlation window")
	}
}

func TestSharedDiagnosticKeepsDockerFactsScopedAndRedacted(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	at := time.Now()
	tool := &GetConsoleErrorsTool{engine: engine, dockerClient: docker.NewClient([]string{"container-worker", "unreachable"}, time.Minute, "")}
	result := map[string]interface{}{}
	tool.addDockerDiagnostics(context.Background(), result, []logs.Entry{
		{Source: "container-worker", Timestamp: at, Level: "ERROR", Message: "request_id=job-1 failed password=container-secret"},
		{Source: "native-worker", Timestamp: at, Level: "ERROR", Message: "request_id=job-1 failed password=native-secret"},
	}, "")
	facts := engine.FactsByPredicate("docker_log")
	if len(facts) != 1 || facts[0].Args[0] != "container-worker" || strings.Contains(fmt.Sprint(facts), "container-secret") {
		t.Fatalf("Docker facts must exclude native entries and redact before storage: %+v", facts)
	}
	health := result["container_health"].(map[string]docker.ContainerHealth)
	if health["unreachable"].Status != "unassessed" {
		t.Fatalf("unavailable container was reported healthy: %+v", health)
	}
}
