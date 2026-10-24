package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/buildinfo"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/logs"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/recorder"
	"browsernerd-mcp-server/internal/security"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server wires the MCP runtime, Rod session manager, and Mangle fact buffer.
type Server struct {
	cfg          config.Config
	sessions     *browser.SessionManager
	engine       *mangle.Engine
	dockerClient *docker.Client
	logSource    logs.Source
	recorder     *recorder.Recorder
	redactor     *security.Redactor
	pathPolicy   *security.PathPolicy
	tools        map[string]Tool
	mcpServer    *mcpserver.MCPServer
	calls        *callTracker
	// staleWarning names source newer than this binary, computed once at start.
	staleWarning string
}

// Tool describes the contract for MCP tool implementations.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]interface{}
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// NewServer constructs the BrowserNERD MCP server and registers all tools.
func NewServer(cfg config.Config, sessions *browser.SessionManager, engine *mangle.Engine) (*Server, error) {
	build := buildinfo.Read()
	serverVersion := cfg.Server.Version
	if build.Revision != "" {
		serverVersion += "+" + build.Revision
	}
	mcpSrv := mcpserver.NewMCPServer(
		cfg.Server.Name,
		serverVersion,
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithPromptCapabilities(false),
		mcpserver.WithRecovery(),
	)

	// Credential redaction is a mandatory trust boundary. Configuration can
	// tune additional keys, but cannot disable protection of built-in secrets.
	redactor := security.NewRedactor(cfg.Security.ExtraSensitiveKeys)
	pathPolicy, err := security.NewPathPolicy(cfg.Security.BaseDir, cfg.Security.WritableRoots)
	if err != nil {
		return nil, fmt.Errorf("initialize write path policy: %w", err)
	}

	logSource, dockerClient := NewLogSources(cfg)

	var flightRecorder *recorder.Recorder
	if cfg.Recorder.Enabled {
		flightRecorder, err = recorder.NewRecorderWithSecurity(cfg.Recorder.TraceDir, cfg.Recorder.MaxRotatedFiles, redactor)
		if err != nil {
			return nil, fmt.Errorf("initialize recorder: %w", err)
		}
		if err := flightRecorder.Start("mcp"); err != nil {
			return nil, fmt.Errorf("start recorder trace: %w", err)
		}
		flightRecorder.Log("server_start", "", map[string]interface{}{
			"server_name":    cfg.Server.Name,
			"server_version": cfg.Server.Version,
			"progressive":    cfg.MCP.IsProgressiveOnly(),
		})
		log.Printf("flight recorder enabled: %s", flightRecorder.CurrentPath())
	}

	server := &Server{
		cfg:          cfg,
		sessions:     sessions,
		engine:       engine,
		dockerClient: dockerClient,
		logSource:    logSource,
		recorder:     flightRecorder,
		redactor:     redactor,
		pathPolicy:   pathPolicy,
		tools:        make(map[string]Tool),
		mcpServer:    mcpSrv,
		calls:        newCallTracker(),
	}
	if stale, _ := buildinfo.StaleSource(buildinfo.SourceRootForBinary()); stale != "" {
		server.staleWarning = fmt.Sprintf("this BrowserNERD binary is older than %s; rebuild with go run ./cmd/rebuild and reconnect the MCP server", filepath.Base(stale))
		log.Printf("WARNING: %s", server.staleWarning)
	}

	server.registerAllTools()
	server.registerAllResources()
	return server, nil
}

// Start launches the stdio server (Claude/Gemini CLI default).
func (s *Server) Start(ctx context.Context) error {
	stdio := mcpserver.NewStdioServer(s.mcpServer)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// httpEndpointPath is where StartHTTP serves MCP.
const httpEndpointPath = "/mcp"

// StartHTTP serves MCP over Streamable HTTP at http://host:port/mcp until ctx
// ends. host is the interface to bind (config mcp.http_host, default
// 127.0.0.1): a server that drives a browser and reads its facts is reachable
// from other machines only when asked to be. mcp-go rejects loopback requests
// whose Host header is not localhost, which closes the DNS-rebinding route
// from a web page to this server.
func (s *Server) StartHTTP(ctx context.Context, host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP on %s: %w", addr, err)
	}
	mcpHTTP := mcpserver.NewStreamableHTTPServer(s.mcpServer, mcpserver.WithEndpointPath(httpEndpointPath))
	mux := http.NewServeMux()
	mux.Handle(httpEndpointPath, mcpHTTP)
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Printf("HTTP server shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mcpHTTP.Shutdown(shutdownCtx); err != nil {
			log.Printf("closing MCP HTTP sessions: %v", err)
		}
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Close releases optional recorder resources and shuts down active sessions.
func (s *Server) Close() error {
	var firstErr error
	if s.sessions != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.sessions.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.recorder != nil {
		s.recorder.Log("server_stop", "", map[string]interface{}{
			"server_name": s.cfg.Server.Name,
		})
		if err := s.recorder.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ExecuteTool executes a tool directly (used by demos/tests).
func (s *Server) ExecuteTool(name string, args map[string]interface{}) (interface{}, error) {
	tool, exists := s.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool.Execute(context.Background(), args)
}

func (s *Server) registerAllTools() {
	disableUnsafeJavaScript := !s.cfg.Security.AllowsUnsafeJavaScript()
	// Always register lifecycle tools (cannot consolidate)
	s.registerTool(&LaunchBrowserTool{sessions: s.sessions})
	s.registerTool(&ShutdownBrowserTool{sessions: s.sessions})
	s.registerTool(&CloseSessionTool{sessions: s.sessions})

	// Always register progressive disclosure tools
	s.registerTool(&BrowserObserveTool{sessions: s.sessions, engine: s.engine, pathPolicy: s.pathPolicy, specsCfg: s.cfg.Specs, doctor: s.doctorReport})
	s.registerTool(&BrowserActTool{
		sessions:                s.sessions,
		engine:                  s.engine,
		redactor:                s.redactor,
		specsCfg:                s.cfg.Specs,
		disableUnsafeJavaScript: disableUnsafeJavaScript,
		pathPolicy:              s.pathPolicy,
	})
	s.registerTool(&BrowserReasonTool{engine: s.engine, sessions: s.sessions, backendLogs: s.logSource})
	s.registerTool(&BrowserAuditTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&GetSpecsTool{engine: s.engine, specsCfg: s.cfg.Specs})
	s.registerTool(&CheckSpecsTool{engine: s.engine, specsCfg: s.cfg.Specs})
	s.registerTool(&BrowserTestTool{
		sessions:                s.sessions,
		engine:                  s.engine,
		disableUnsafeJavaScript: disableUnsafeJavaScript,
		redactor:                s.redactor,
		pathPolicy:              s.pathPolicy,
	})
	s.registerTool(&BrowserMangleTool{
		engine:           s.engine,
		logSource:        s.logSource,
		recorder:         s.recorder,
		redactor:         s.redactor,
		pathPolicy:       s.pathPolicy,
		defaultTraceDir:  s.cfg.Recorder.TraceDir,
		defaultLogWindow: serverLogWindow(s.logSource),
	})

	// When progressive_only is false, also register all individual tools
	if !s.cfg.MCP.IsProgressiveOnly() {
		s.registerIndividualTools()
	}
}

func (s *Server) registerIndividualTools() {
	disableUnsafeJavaScript := !s.cfg.Security.AllowsUnsafeJavaScript()
	// Browser session management
	s.registerTool(&ListSessionsTool{sessions: s.sessions})
	s.registerTool(&ListBrowsersTool{sessions: s.sessions})
	s.registerTool(&CreateSessionTool{sessions: s.sessions})
	s.registerTool(&AttachSessionTool{sessions: s.sessions})
	s.registerTool(&FocusSessionTool{sessions: s.sessions})
	s.registerTool(&ForkSessionTool{sessions: s.sessions})
	s.registerTool(&ReifyReactTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&SnapshotDOMTool{sessions: s.sessions, engine: s.engine})

	// Basic fact operations
	s.registerTool(&PushFactsTool{engine: s.engine})
	s.registerTool(&ReadFactsTool{engine: s.engine})
	s.registerTool(&QueryFactsTool{engine: s.engine})
	s.registerTool(&SubmitRuleTool{engine: s.engine})
	s.registerTool(&QueryTemporalTool{engine: s.engine})
	s.registerTool(&EvaluateRuleTool{engine: s.engine})
	s.registerTool(&SubscribeRuleTool{engine: s.engine}) // Watch Mode (PRD 5.2)

	// Awaiting conditions (polling-based)
	s.registerTool(&AwaitFactTool{engine: s.engine})
	s.registerTool(&AwaitConditionsTool{engine: s.engine})

	// Diagnostic tools
	s.registerTool(&GetConsoleErrorsTool{engine: s.engine, logSource: s.logSource, dockerClient: s.dockerClient, redactor: s.redactor})
	s.registerTool(&GetToastNotificationsTool{engine: s.engine})

	// Navigation tools - Token-efficient interaction
	s.registerTool(&GetNavigationLinksTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&GetInteractiveElementsTool{sessions: s.sessions, engine: s.engine, redactor: s.redactor})
	s.registerTool(&DiscoverGridsTool{sessions: s.sessions})
	s.registerTool(&DiscoverHiddenContentTool{sessions: s.sessions})
	s.registerTool(&InteractTool{sessions: s.sessions, engine: s.engine, redactor: s.redactor})
	s.registerTool(&GetPageStateTool{sessions: s.sessions})
	s.registerTool(&NavigateURLTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&PressKeyTool{sessions: s.sessions, engine: s.engine})

	// Advanced tools - Screenshots, JS eval, batch operations
	s.registerTool(&ScreenshotTool{sessions: s.sessions, engine: s.engine, pathPolicy: s.pathPolicy})
	s.registerTool(&BrowserHistoryTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&EvaluateJSTool{
		sessions:                s.sessions,
		engine:                  s.engine,
		redactor:                s.redactor,
		disableUnsafeJavaScript: disableUnsafeJavaScript,
	})
	s.registerTool(&FillFormTool{sessions: s.sessions, engine: s.engine})

	// Mangle-driven automation
	s.registerTool(&ExecutePlanTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&WaitForConditionTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&DiagnosePageTool{engine: s.engine})
	s.registerTool(&AwaitStableStateTool{engine: s.engine})

	// Declarative testing
	s.registerTool(&RunTestTool{
		sessions:                s.sessions,
		engine:                  s.engine,
		disableUnsafeJavaScript: disableUnsafeJavaScript,
		redactor:                s.redactor,
		pathPolicy:              s.pathPolicy,
	})
	s.registerTool(&GenerateTestTool{engine: s.engine})

}

func (s *Server) registerTool(tool Tool) {
	s.tools[tool.Name()] = tool

	schema, err := json.Marshal(tool.InputSchema())
	if err != nil {
		schema = json.RawMessage(`{"type":"object"}`)
	}

	mcpTool := mcp.NewToolWithRawSchema(tool.Name(), tool.Description(), schema)
	s.mcpServer.AddTool(mcpTool, s.wrapTool(tool))
}

func (s *Server) wrapTool(tool Tool) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		args := request.GetArguments()
		if args == nil {
			args = map[string]interface{}{}
		}
		sessionID := ""
		if rawSessionID, ok := args["session_id"]; ok {
			sessionID = fmt.Sprintf("%v", rawSessionID)
		}
		if s.recorder != nil {
			s.recorder.Log("tool_call", sessionID, map[string]interface{}{
				"tool": tool.Name(),
				"args": args,
			})
		}

		detail := callDetail(args)
		callID := s.calls.begin(tool.Name(), sessionID, detail)
		timeout := s.cfg.MCP.GetToolTimeout() + declaredWaitBudget(args)
		result, err, timedOut := s.executeWithDeadline(ctx, tool, args, timeout)
		if timedOut {
			s.calls.timedOut(callID)
			log.Printf("tool %s (%s) exceeded %s on session %q; its work is still running", tool.Name(), detail, timeout, sessionID)
			if s.recorder != nil {
				s.recorder.Log("tool_result", sessionID, map[string]interface{}{
					"tool":        tool.Name(),
					"success":     false,
					"timed_out":   true,
					"duration_ms": time.Since(start).Milliseconds(),
				})
			}
			payload := sanitizeToolPayload(marshalToolPayload(tool.Name(), timeoutResult(tool.Name(), detail, sessionID, timeout)), s.redactor)
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(string(payload))},
				IsError: true,
			}, nil
		}
		s.calls.end(callID, err == nil && !resultReportsFailure(result), errorText(err, result))
		if err == nil && tool.Name() == "launch-browser" && s.staleWarning != "" {
			if resultMap, ok := result.(map[string]interface{}); ok {
				resultMap["warning"] = s.staleWarning
			}
		}
		if err != nil {
			if s.recorder != nil {
				s.recorder.Log("tool_result", sessionID, map[string]interface{}{
					"tool":        tool.Name(),
					"success":     false,
					"error":       err.Error(),
					"duration_ms": time.Since(start).Milliseconds(),
				})
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(s.redactor.SanitizeString(fmt.Sprintf("tool %s failed: %v", tool.Name(), err)))},
				IsError: true,
			}, nil
		}

		payload := sanitizeToolPayload(marshalToolPayload(tool.Name(), result), s.redactor)
		if s.recorder != nil {
			payloadString := string(payload)
			payloadTruncated := false
			if len(payloadString) > 65536 {
				payloadString = payloadString[:65536]
				payloadTruncated = true
			}
			s.recorder.Log("tool_result", sessionID, map[string]interface{}{
				"tool":              tool.Name(),
				"success":           true,
				"duration_ms":       time.Since(start).Milliseconds(),
				"result_json":       payloadString,
				"result_truncated":  payloadTruncated,
				"result_size_bytes": len(payload),
			})
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(payload))},
			IsError: false,
		}, nil
	}
}

func sanitizeToolPayload(payload []byte, redactor *security.Redactor) []byte {
	if redactor == nil || len(payload) == 0 {
		return payload
	}
	var decoded interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return payload
	}
	safe, err := json.Marshal(redactor.Sanitize(decoded))
	if err != nil {
		return payload
	}
	return safe
}

func marshalToolPayload(toolName string, result interface{}) []byte {
	payload, marshalErr := json.Marshal(result)
	if marshalErr == nil {
		return payload
	}

	fallback := map[string]interface{}{
		"success": false,
		"error":   fmt.Sprintf("tool %s returned non-serializable payload: %v", toolName, marshalErr),
	}
	payload, fallbackErr := json.Marshal(fallback)
	if fallbackErr == nil {
		return payload
	}

	return []byte(fmt.Sprintf(`{"success":false,"error":"tool %s failed to encode payload"}`, toolName))
}

func serverLogWindow(source logs.Source) time.Duration {
	if source == nil {
		return defaultBackendWindow
	}
	return source.LogWindow()
}
