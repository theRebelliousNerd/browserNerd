# Internal Implementation

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


`internal/` contains the core implementation packages for the BrowserNERD MCP server.

## Package Overview

| Package | Purpose | Key Exports |
|---------|---------|-------------|
| **`browser/`** | Rod session management, CDP events | `SessionManager`, `Session` |
| **`mcp/`** | MCP server and tool implementations | `Server`, `Tool` |
| **`mangle/`** | Mangle fact engine wrapper | `Engine`, `Fact` |
| **`config/`** | Configuration loading | `Config`, `Load` |

## Architecture

```
                    +-----------------+
                    |   cmd/server    |
                    +--------+--------+
                             |
              +--------------+--------------+
              |              |              |
      +-------v-------+  +---v---+  +------v------+
      |    mcp/       |  |config/|  |   browser/  |
      | (MCP tools)   |  +-------+  | (Rod/CDP)   |
      +-------+-------+             +------+------+
              |                            |
      +-------v-------+                    |
      |   mangle/     |<-------------------+
      | (fact engine) |    (event sink)
      +---------------+
```

## Data Flow

1. **CDP Events** - Browser emits events (network, console, DOM)
2. **Session Manager** - Captures and normalizes events
3. **Mangle Engine** - Stores facts, derives rules
4. **MCP Tools** - Query facts, return results to AI

## Key Interfaces

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

### Engine Sink Interface

```go
type EngineSink interface {
    AddFacts(ctx context.Context, facts []mangle.Fact) error
}
```

## Package Details

- [browser/CLAUDE.md](browser/CLAUDE.md) - Session management
- [mcp/CLAUDE.md](mcp/CLAUDE.md) - MCP tools
- [mangle/CLAUDE.md](mangle/CLAUDE.md) - Fact engine
- [config/CLAUDE.md](config/CLAUDE.md) - Configuration

## Testing

```bash
# Run all internal tests
go test ./internal/... -v

# Run specific package tests
go test ./internal/mangle -v
go test ./internal/browser -v
```
