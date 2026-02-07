package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server wires the MCP runtime, Rod session manager, and Mangle fact buffer.
type Server struct {
	cfg          config.Config
	sessions     *browser.SessionManager
	engine       *mangle.Engine
	dockerClient *docker.Client
	tools        map[string]Tool
	mcpServer    *mcpserver.MCPServer
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
	mcpSrv := mcpserver.NewMCPServer(
		cfg.Server.Name,
		cfg.Server.Version,
		mcpserver.WithResourceCapabilities(true, true),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithLogging(),
		mcpserver.WithPromptCapabilities(false),
		mcpserver.WithRecovery(),
	)

	// Initialize Docker client if enabled
	var dockerClient *docker.Client
	if cfg.Docker.Enabled {
		dockerClient = docker.NewClient(
			cfg.Docker.Containers,
			cfg.Docker.GetLogWindow(),
			cfg.Docker.Host,
		)
		log.Printf("Docker log integration enabled for containers: %v", cfg.Docker.Containers)
	}

	server := &Server{
		cfg:          cfg,
		sessions:     sessions,
		engine:       engine,
		dockerClient: dockerClient,
		tools:        make(map[string]Tool),
		mcpServer:    mcpSrv,
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

// StartSSE hosts the server over HTTP using SSE endpoints with graceful shutdown.
func (s *Server) StartSSE(ctx context.Context, port int) error {
	sseServer := mcpserver.NewSSEServer(s.mcpServer, mcpserver.WithBaseURL("http://localhost:"+strconv.Itoa(port)))

	mux := http.NewServeMux()
	mux.Handle("/sse", sseServer.SSEHandler())
	mux.Handle("/message", sseServer.MessageHandler())

	httpServer := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: mux,
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		log.Printf("SSE server shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
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
	// Browser session management
	s.registerTool(&ListSessionsTool{sessions: s.sessions})
	s.registerTool(&CreateSessionTool{sessions: s.sessions})
	s.registerTool(&AttachSessionTool{sessions: s.sessions})
	s.registerTool(&ForkSessionTool{sessions: s.sessions})
	s.registerTool(&ReifyReactTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&SnapshotDOMTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&LaunchBrowserTool{sessions: s.sessions})
	s.registerTool(&ShutdownBrowserTool{sessions: s.sessions})

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
	s.registerTool(&GetConsoleErrorsTool{engine: s.engine, dockerClient: s.dockerClient})
	s.registerTool(&GetToastNotificationsTool{engine: s.engine})

	// Navigation tools - Token-efficient interaction
	s.registerTool(&GetNavigationLinksTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&GetInteractiveElementsTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&DiscoverHiddenContentTool{sessions: s.sessions})
	s.registerTool(&InteractTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&GetPageStateTool{sessions: s.sessions})
	s.registerTool(&NavigateURLTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&PressKeyTool{sessions: s.sessions, engine: s.engine})

	// Progressive disclosure consolidated tools (dual-run with existing tools)
	s.registerTool(&BrowserObserveTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&BrowserActTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&BrowserReasonTool{engine: s.engine, dockerClient: s.dockerClient})

	// Advanced tools - Screenshots, JS eval, batch operations
	s.registerTool(&ScreenshotTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&BrowserHistoryTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&EvaluateJSTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&FillFormTool{sessions: s.sessions, engine: s.engine})

	// Mangle-driven automation - THE TOKEN EFFICIENCY TOOLS
	s.registerTool(&ExecutePlanTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&WaitForConditionTool{sessions: s.sessions, engine: s.engine})
	s.registerTool(&DiagnosePageTool{engine: s.engine})
	s.registerTool(&AwaitStableStateTool{engine: s.engine})
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
		args := request.GetArguments()
		if args == nil {
			args = map[string]interface{}{}
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("tool %s failed: %v", tool.Name(), err))},
				IsError: true,
			}, nil
		}

		payload := marshalToolPayload(tool.Name(), result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(payload))},
			IsError: false,
		}, nil
	}
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
