// Command smoke verifies the BrowserNERD stdio MCP surface with the official
// Go client. Product tests themselves are authored and run through browser-test.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type options struct {
	mode       string
	serverPath string
	configPath string
	url        string
	timeout    time.Duration
}

func main() {
	var opts options
	flag.StringVar(&opts.mode, "mode", "smoke", "verification mode: list or smoke")
	flag.StringVar(&opts.serverPath, "server", defaultServerPath(), "BrowserNERD server binary")
	flag.StringVar(&opts.configPath, "config", "config.yaml", "BrowserNERD configuration")
	flag.StringVar(&opts.url, "url", "https://example.com/", "URL for smoke mode")
	flag.DurationVar(&opts.timeout, "timeout", 90*time.Second, "overall timeout")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "browsernerd smoke:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	serverPath, err := filepath.Abs(opts.serverPath)
	if err != nil {
		return fmt.Errorf("resolve server path: %w", err)
	}
	configPath, err := filepath.Abs(opts.configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(serverPath); err != nil {
		return fmt.Errorf("server binary %s: %w", serverPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	client, err := mcpclient.NewStdioMCPClient(serverPath, os.Environ(), "--config", configPath)
	if err != nil {
		return fmt.Errorf("create MCP client: %w", err)
	}
	defer client.Close()
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start MCP client: %w", err)
	}
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "browsernerd-go-smoke", Version: "1.0.0"},
			Capabilities:    mcp.ClientCapabilities{},
		},
	}); err != nil {
		return fmt.Errorf("initialize MCP client: %w", err)
	}

	switch opts.mode {
	case "list":
		return listTools(ctx, client)
	case "smoke":
		return smoke(ctx, client, opts.url)
	default:
		return fmt.Errorf("mode must be list or smoke")
	}
}

func listTools(ctx context.Context, client *mcpclient.Client) error {
	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return writeJSON(map[string]interface{}{"tool_count": len(names), "tools": names})
}

func smoke(ctx context.Context, client *mcpclient.Client, targetURL string) error {
	if _, err := callJSON(ctx, client, "launch-browser", map[string]interface{}{}); err != nil {
		return err
	}
	act, err := callJSON(ctx, client, "browser-act", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "session_create", "url": targetURL},
		},
		"view":          "full",
		"include_specs": false,
	})
	if err != nil {
		return err
	}
	sessionID := sessionIDFromAct(act)
	if sessionID == "" {
		return fmt.Errorf("browser-act did not return a created session: %+v", act)
	}
	defer callJSON(context.Background(), client, "close-session", map[string]interface{}{"session_id": sessionID})
	if _, err := callJSON(ctx, client, "browser-act", map[string]interface{}{
		"session_id": sessionID,
		"operations": []interface{}{
			// Navigate once after session creation so the installed CDP stream
			// emits the current_url fact used by the declarative assertion.
			map[string]interface{}{"type": "navigate", "url": targetURL},
			map[string]interface{}{"type": "await_stable", "timeout_ms": 15000},
		},
		"view":          "summary",
		"include_specs": false,
	}); err != nil {
		return err
	}

	observe, err := callJSON(ctx, client, "browser-observe", map[string]interface{}{
		"session_id":    sessionID,
		"intent":        "quick_status",
		"view":          "summary",
		"include_specs": false,
	})
	if err != nil {
		return err
	}
	testResult, err := callJSON(ctx, client, "browser-test", map[string]interface{}{
		"operation": "run",
		"view":      "summary",
		"test": map[string]interface{}{
			"name":       "page established current state",
			"session_id": sessionID,
			"assertions": []interface{}{
				map[string]interface{}{
					"name":   "current URL captured",
					"query":  "current_url(S, U)",
					"expect": "present",
					"scope":  "current",
				},
			},
		},
	})
	if err != nil {
		return err
	}
	if passed, _ := testResult["passed"].(bool); !passed {
		return fmt.Errorf("declarative MCP smoke assertion failed: %+v", testResult)
	}
	return writeJSON(map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"observe":    observe,
		"test":       testResult,
	})
}

func callJSON(ctx context.Context, client *mcpclient.Client, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: name, Arguments: arguments},
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("%s returned MCP error: %+v", name, result.Content)
	}
	for _, content := range result.Content {
		text, ok := mcp.AsTextContent(content)
		if !ok {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
			return nil, fmt.Errorf("%s returned invalid JSON: %w", name, err)
		}
		if success, exists := payload["success"].(bool); exists && !success {
			if detail, ok := payload["error"]; ok && detail != nil {
				return nil, fmt.Errorf("%s failed: %v", name, detail)
			}
			return nil, fmt.Errorf("%s failed: %+v", name, payload)
		}
		return payload, nil
	}
	return nil, errors.New(name + " returned no text payload")
}

func sessionIDFromAct(result map[string]interface{}) string {
	rows, _ := result["results"].([]interface{})
	for _, raw := range rows {
		row, _ := raw.(map[string]interface{})
		operationResult, _ := row["result"].(map[string]interface{})
		session, _ := operationResult["session"].(map[string]interface{})
		if id, _ := session["id"].(string); id != "" {
			return id
		}
	}
	return ""
}

func writeJSON(value interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func defaultServerPath() string {
	if _, err := os.Stat(filepath.Join("bin", "browsernerd.exe")); err == nil {
		return filepath.Join("bin", "browsernerd.exe")
	}
	return filepath.Join("bin", "browsernerd")
}
