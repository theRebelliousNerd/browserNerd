# Browser Session Management

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


`internal/browser/` manages Chrome browser sessions using Rod (Chrome DevTools Protocol wrapper) and streams CDP events to the Mangle fact engine.

## File Overview

| File | Purpose | Key Exports |
|------|---------|-------------|
| **`session_manager.go`** | Browser lifecycle, session tracking | `SessionManager`, `Session` |

## SessionManager Architecture

```go
type SessionManager struct {
    cfg        config.BrowserConfig
    engine     EngineSink           // Mangle fact sink
    browser    *rod.Browser         // Detached Chrome instance
    sessions   map[string]*sessionRecord
    controlURL string               // WebSocket DevTools URL
}

type Session struct {
    ID         string    `json:"id"`
    TargetID   string    `json:"target_id"`
    URL        string    `json:"url"`
    Title      string    `json:"title"`
    Status     string    `json:"status"`
    CreatedAt  time.Time `json:"created_at"`
    LastActive time.Time `json:"last_active"`
}
```

## Core Operations

### Launch Browser

```go
manager := browser.NewSessionManager(cfg.Browser, mangleEngine)
err := manager.Start(ctx)
```

Uses Rod's launcher to start Chrome with:
- Remote debugging enabled
- Optional headless mode
- Custom flags from config

### Create Session

```go
session, err := manager.CreateSession(ctx, "https://example.com")
// Returns: {id: "sess-123", url: "https://example.com", ...}
```

Opens new incognito page, begins CDP event tracking.

### Attach to Existing Tab

```go
session, err := manager.Attach(ctx, "TARGET-ID-FROM-CDP")
```

Connects to existing Chrome tab by TargetID.

### Fork Session (Clone Auth State)

```go
forked, err := manager.ForkSession(ctx, "sess-123", "https://example.com/dashboard")
```

Copies cookies and localStorage into new session.

## CDP Event Ingestion

The SessionManager captures and normalizes CDP events:

| CDP Event | Mangle Fact |
|-----------|-------------|
| `Network.requestWillBeSent` | `net_request(id, method, url, initiator, time)` |
| `Network.responseReceived` | `net_response(id, status, latency, duration)` |
| `Runtime.consoleAPICalled` | `console_event(level, message, time)` |
| `Page.frameNavigated` | `navigation_event(session, url, time)` |

## Event Throttling

High-frequency events can be throttled:

```go
type eventThrottler struct {
    interval time.Duration
    last     map[string]time.Time
}
```

Prevents overwhelming the fact buffer with DOM/input events.

## Session Persistence

Sessions can be persisted to disk:

```yaml
browser:
  session_store: "sessions.json"
```

Enables resumption after server restart.

## Dependencies

- `github.com/go-rod/rod` - Chrome DevTools Protocol
- `github.com/go-rod/rod/lib/launcher` - Chrome launcher
- `github.com/go-rod/rod/lib/proto` - CDP protocol types
