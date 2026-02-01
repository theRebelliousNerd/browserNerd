# Command Entry Point

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


`cmd/server/` contains the main entry point for the BrowserNERD MCP server.

## File Overview

| File | Purpose |
|------|---------|
| **`main.go`** | Server initialization, signal handling, mode selection |

## Startup Flow

```
1. Parse flags (--config, --sse-port)
2. Load configuration from YAML
3. Redirect logs to file (for stdio mode)
4. Initialize Mangle engine with schema
5. Create SessionManager (Rod browser)
6. Create MCP Server with tools
7. Start in stdio or SSE mode
8. Wait for shutdown signal
```

## Command Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to configuration file |
| `--sse-port` | 0 (disabled) | Port for SSE HTTP server |

## Running Modes

### Stdio Mode (Default)

For Claude Desktop, Cursor, and other MCP clients:

```bash
./bin/browsernerd --config config.yaml
```

Logs are written to `log_file` to avoid interfering with MCP protocol on stdout.

### SSE Mode

For HTTP-based clients and debugging:

```bash
./bin/browsernerd --config config.yaml --sse-port 8080
```

Endpoints:
- `GET /sse` - Server-sent events stream
- `POST /message` - Send MCP messages

## Signal Handling

The server handles `SIGINT` and `SIGTERM` for graceful shutdown:

1. Cancel context
2. Close browser connections
3. Flush session state
4. Exit cleanly

## Building

```bash
# From mcp-server directory
go build -o bin/browsernerd ./cmd/server

# With version info
go build -ldflags "-X main.version=1.0.0" -o bin/browsernerd ./cmd/server
```
