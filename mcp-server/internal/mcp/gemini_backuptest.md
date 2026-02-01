# MCP Server and Tools

# ⚠️ CRITICAL ARCHITECTURAL BOUNDARY ⚠️

**ABSOLUTE ISOLATION REQUIREMENT**

This code is part of `dev_tools/` - development utilities for BUILDING SymbioGen.

**NEVER EVER EVER:**
- ❌ Import from `ai_engine/`
- ❌ Import from `api/`
- ❌ Import from `common/`
- ❌ Import from `workers/`
- ❌ Import from `ingestion/`
- ❌ Import from `graph_rag_service/`
- ❌ Import from `frontend/`
- ❌ Use SymbioGen's ArangoDB (localhost:8529, database: symbiogen_db)
- ❌ Use SymbioGen's database credentials
- ❌ Call SymbioGen's API endpoints (localhost:8000)
- ❌ Share any SymbioGen runtime resources

**WHY:** `dev_tools/` is scaffolding. Once built, the scaffolding comes down. The cathedral (SymbioGen) stands alone. They MUST remain completely isolated.

**IF YOU NEED SHARED CODE:** Extract it to a standalone library or duplicate it. Never create dependencies on SymbioGen runtime code.

**dev_tools has its own resources:**
- Separate ArangoDB instance (localhost:8530, database: code_graph)
- Separate credentials
- Standalone Go binaries
- Independent MCP servers

---


`internal/mcp/` implements the Model Context Protocol server and all browser automation tools.

## File Overview

| File | Purpose | Key Exports |
|------|---------|-------------|
| **`server.go`** | MCP server initialization, tool registration | `Server`, `NewServer` |
| **`tools.go`** | Tool implementations | Session, interaction, Mangle tools |

## Server Architecture

```go
type Server struct {
    cfg       config.Config
    sessions  *browser.SessionManager
    engine    *mangle.Engine
    tools     map[string]Tool
    mcpServer *mcpserver.MCPServer
}

type Tool interface {
    Name() string
    Description() string
    InputSchema() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

## Tool Categories

### Session Management

| Tool | Description |
|------|-------------|
| `launch-browser` | Start Chrome via Rod launcher |
| `shutdown-browser` | Close Chrome and clear sessions |
| `list-sessions` | List active browser sessions |
| `create-session` | Open new incognito page |
| `attach-session` | Attach to existing CDP target |
| `fork-session` | Clone session with auth state |

### Page Interaction

| Tool | Description |
|------|-------------|
| `navigate-url` | Navigate to URL |
| `browser-history` | Back, forward, reload |
| `get-page-state` | URL, title, loading status |
| `get-interactive-elements` | Buttons, inputs, links, selects |
| `interact` | Click, type, select, toggle |
| `fill-form` | Fill multiple form fields |
| `press-key` | Keyboard input |
| `screenshot` | Capture page/element image |

### Intelligence Extraction

| Tool | Description |
|------|-------------|
| `reify-react` | Extract React Fiber tree |
| `snapshot-dom` | DOM snapshot to facts |
| `get-console-errors` | Console errors with causality |
| `evaluate-js` | Execute JavaScript |

### Mangle Logic

| Tool | Description |
|------|-------------|
| `read-facts` | Recent buffered facts |
| `push-facts` | Add facts to buffer |
| `query-facts` | Execute Mangle query |
| `query-temporal` | Query facts in time window |
| `submit-rule` | Add Mangle rule |
| `evaluate-rule` | Evaluate derived predicate |
| `await-fact` | Wait for fact to appear |
| `await-conditions` | Wait for multiple conditions |
| `wait-for-condition` | Poll until predicate matches |
| `subscribe-rule` | Watch mode subscription |
| `execute-plan` | Run Mangle-derived action plan |

## Tool Implementation Pattern

```go
type CreateSessionTool struct {
    sessions *browser.SessionManager
}

func (t *CreateSessionTool) Name() string { return "create-session" }

func (t *CreateSessionTool) Description() string {
    return `Create a new browser session...`
}

func (t *CreateSessionTool) InputSchema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "url": map[string]interface{}{
                "type":        "string",
                "description": "Optional URL to navigate",
            },
        },
    }
}

func (t *CreateSessionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    url := getStringArg(args, "url")
    return t.sessions.CreateSession(ctx, url)
}
```

## Server Modes

### Stdio (Default)

```go
server.Start(ctx) // Reads stdin, writes stdout
```

### SSE (HTTP)

```go
server.StartSSE(ctx, 8080) // HTTP server with /sse and /message endpoints
```

## Dependencies

- `github.com/mark3labs/mcp-go/mcp` - MCP types
- `github.com/mark3labs/mcp-go/server` - MCP server implementation
