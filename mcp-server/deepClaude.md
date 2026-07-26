---
title: BrowserNERD MCP Server
created: 2026-02-07
last_updated: 2026-05-21
doc_type: reference
subsystem: infra
read_when: Reference material for infra
indexes: []
---

# BrowserNERD MCP Server

# ⚠️ CRITICAL ARCHITECTURAL BOUNDARY ⚠️

**ABSOLUTE ISOLATION REQUIREMENT**

This code is part of `dev_tools/` - development utilities for BUILDING Cross-Thread™.

**NEVER EVER EVER:**
- ❌ Import from `ai_engine/`
- ❌ Import from `api/`
- ❌ Import from `common/`
- ❌ Import from `workers/`
- ❌ Import from `ingestion/`
- ❌ Import from `graph_rag_service/`
- ❌ Import from `frontend/`
- ❌ Use Cross-Thread™'s Vectryx (localhost:8529, database: cross-thread_db)
- ❌ Use Cross-Thread™'s database credentials
- ❌ Call Cross-Thread™'s API endpoints (localhost:8000)
- ❌ Share any Cross-Thread™ runtime resources

**WHY:** `dev_tools/` is scaffolding. Once built, the scaffolding comes down. The cathedral (Cross-Thread™) stands alone. They MUST remain completely isolated.

**IF YOU NEED SHARED CODE:** Extract it to a standalone library or duplicate it. Never create dependencies on Cross-Thread™ runtime code.

**dev_tools has its own resources:**
- Separate Vectryx instance (localhost:8530, database: code_graph)
- Separate credentials
- Standalone Go binaries
- Independent MCP servers

---


`mcp-server/` is the Go implementation of the BrowserNERD Model Context Protocol server, providing browser automation and intelligence extraction via Rod and Mangle.

## Purpose

MCP server that exposes browser automation capabilities to AI assistants (Claude, Cursor, etc.) with:

- Chrome DevTools Protocol (CDP) integration via Rod
- Session management (create, attach, fork)
- React Fiber tree extraction
- DOM snapshotting
- Logic-based assertions via Mangle
- Event streaming (network, console, navigation)

## Directory Structure

| Directory | Purpose | CLAUDE.md |
|-----------|---------|-----------|
| **`cmd/server/`** | Entry point (`main.go`) | [cmd/CLAUDE.md](cmd/CLAUDE.md) |
| **`internal/`** | Core implementation | [internal/CLAUDE.md](internal/CLAUDE.md) |
| **`schemas/`** | Mangle schema definitions | [schemas/CLAUDE.md](schemas/CLAUDE.md) |
| **`docs/`** | Implementation guides | [docs/CLAUDE.md](docs/CLAUDE.md) |
| **`bin/`** | Compiled binaries | - |
| **`data/`** | Runtime data (sessions) | - |

## Quick Start

```bash
# Build
go build -o bin/browsernerd ./cmd/server

# Run (stdio mode - for Claude Desktop)
./bin/browsernerd --config config.yaml

# Run (SSE mode - for HTTP clients)
./bin/browsernerd --config config.yaml --sse-port 8080
```

## Configuration

```yaml
server:
  name: "browsernerd-mcp"
  version: "0.0.4"
  log_file: "browsernerd-mcp.log"

browser:
  auto_start: true
  headless: false
  enable_dom_ingestion: true
  enable_header_ingestion: true

mangle:
  enable: true
  schema_path: "schemas/browser.mg"
  fact_buffer_limit: 10000
```

## Key Components

1. **SessionManager** (`internal/browser/`) - Rod browser lifecycle
2. **MCP Server** (`internal/mcp/`) - Tool registration and execution
3. **Mangle Engine** (`internal/mangle/`) - Fact storage and queries
4. **Config** (`internal/config/`) - YAML loading

## Dependencies

- `github.com/go-rod/rod` - Chrome DevTools Protocol
- `github.com/mark3labs/mcp-go` - MCP protocol implementation
- `codeberg.org/TauCeti/mangle-go` v0.5.0 - Logic programming engine

## Make sure you always update the version number on big pushed to github.
