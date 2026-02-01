# Configuration

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


`internal/config/` handles loading and validating YAML configuration for the BrowserNERD MCP server.

## File Overview

| File | Purpose | Key Exports |
|------|---------|-------------|
| **`config.go`** | Configuration types and loading | `Config`, `Load`, `DefaultConfig` |

## Configuration Structure

```go
type Config struct {
    Server  ServerConfig  `yaml:"server"`
    Browser BrowserConfig `yaml:"browser"`
    MCP     MCPConfig     `yaml:"mcp"`
    Mangle  MangleConfig  `yaml:"mangle"`
}
```

## ServerConfig

```yaml
server:
  name: "browsernerd-mcp"
  version: "0.0.1"
  log_file: "browsernerd-mcp.log"  # Required for stdio mode
```

## BrowserConfig

```yaml
browser:
  # Connection
  debugger_url: "ws://localhost:9222"  # Existing Chrome
  launch: ["chrome", "--remote-debugging-port=9222"]  # Or launch new
  auto_start: true
  headless: false

  # Timeouts
  default_navigation_timeout: "15s"
  default_attach_timeout: "10s"

  # Persistence
  session_store: "sessions.json"

  # Event ingestion
  enable_dom_ingestion: true
  enable_header_ingestion: true
  event_logging_level: "normal"  # minimal | normal | verbose
  event_throttle_ms: 0           # 0 = no throttling
```

## MCPConfig

```yaml
mcp:
  sse_port: 0  # 0 = stdio mode, >0 = SSE HTTP server
```

## MangleConfig

```yaml
mangle:
  enable: true
  schema_path: "schemas/browser.mg"
  disable_builtin_rules: false
  fact_buffer_limit: 10000
```

## Loading Configuration

```go
cfg, err := config.Load("config.yaml")
if err != nil {
    log.Fatal(err)
}

// Or use defaults
cfg := config.DefaultConfig()
```

## Default Values

```go
func DefaultConfig() Config {
    return Config{
        Server: ServerConfig{
            Name:    "browsernerd-mcp",
            Version: "0.0.1",
            LogFile: "browsernerd-mcp.log",
        },
        Browser: BrowserConfig{
            AutoStart:                true,
            DefaultNavigationTimeout: "15s",
            DefaultAttachTimeout:     "10s",
            SessionStore:             "sessions.json",
            EnableDOMIngestion:       true,
            EnableHeaderIngestion:    true,
            EventLoggingLevel:        "normal",
        },
        Mangle: MangleConfig{
            Enable:          true,
            SchemaPath:      "schemas/browser.mg",
            FactBufferLimit: 10000,
        },
    }
}
```

## Environment Variables

Configuration can reference environment variables for sensitive values:

- `CHROME_PATH` - Path to Chrome executable
- `DEBUGGER_URL` - WebSocket URL for existing Chrome

## Example config.yaml

```yaml
server:
  name: "browsernerd-mcp"
  version: "1.0.0"
  log_file: "/var/log/browsernerd.log"

browser:
  auto_start: true
  headless: true
  enable_dom_ingestion: true
  enable_header_ingestion: true
  event_throttle_ms: 100

mcp:
  sse_port: 8080  # Enable HTTP mode

mangle:
  enable: true
  schema_path: "schemas/browser.mg"
  fact_buffer_limit: 50000
```
