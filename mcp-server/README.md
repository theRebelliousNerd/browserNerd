# BrowserNERD MCP Server

Detached Rod + Mangle MCP server for browser automation. Ships session management, CDP ingestion (network/console/navigation/DOM), React Fiber reification, Docker log correlation, and logic-based assertions via Google Mangle.

## Features

- **MCP Transport**: stdio (Claude Code) or SSE (multi-client) via `mark3labs/mcp-go`
- **Session Management**: Multiple concurrent tabs, session forking with auth state cloning
- **React Reification**: Extract component tree, props, and state as Mangle facts
- **CDP Event Stream**: Network requests, console logs, navigation, DOM mutations
- **Docker Integration**: Correlate browser errors with backend container logs
- **Mangle Reasoning**: Causal rules for root cause analysis (API failures, cascading errors)

## Quickstart

```bash
# Build
go build -o bin/browsernerd.exe ./cmd/server

# Run (stdio for Claude Code)
./bin/browsernerd.exe --config config.yaml

# Run (SSE for multi-client)
./bin/browsernerd.exe --config config.yaml --sse-port 8080
```

## Configuration Reference

All settings go in `config.yaml`. See `config.example.yaml` for a minimal template.

### server

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `name` | string | `"browsernerd-mcp"` | MCP server name |
| `version` | string | `"0.0.1"` | Server version |
| `log_file` | string | `"browsernerd-mcp.log"` | Log file path (required for stdio mode to avoid stderr pollution) |

### browser

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auto_start` | bool | `true` | Launch browser on server start vs on-demand via `launch-browser` tool |
| `headless` | bool | `true` | Run Chromium without visible window |
| `debugger_url` | string | `""` | WebSocket URL to attach to existing Chrome (e.g., `ws://localhost:9222`) |
| `launch` | []string | `[]` | Chrome launch command and flags (first element is binary path) |
| `default_navigation_timeout` | string | `"15s"` | Timeout for page navigation |
| `default_attach_timeout` | string | `"10s"` | Timeout when attaching to existing targets |
| `session_store` | string | `"sessions.json"` | Path to persist session metadata across restarts |
| `enable_dom_ingestion` | bool | `true` | Capture DOM structure as Mangle facts |
| `enable_header_ingestion` | bool | `true` | Capture HTTP headers as Mangle facts |
| `event_logging_level` | string | `"normal"` | `minimal` (errors only), `normal` (all events), `verbose` (+ DOM mutations) |
| `event_throttle_ms` | int | `0` | Throttle high-frequency events (0=none, 100-200 recommended) |
| `viewport_width` | int | `1920` | Browser viewport width |
| `viewport_height` | int | `1080` | Browser viewport height |

**Launch command example (Windows):**
```yaml
launch:
  - "C:\\Users\\you\\AppData\\Roaming\\rod\\browser\\chromium-1321438\\chrome.exe"
  - "--remote-debugging-port=9222"
  - "--user-data-dir=C:\\temp\\chrome-debug"
  - "--no-first-run"
  - "--disable-sync"
```

**Launch command example (Linux/Mac):**
```yaml
launch:
  - "/home/you/.rod/browser/chromium-1321438/chrome"
  - "--remote-debugging-port=9222"
  - "--user-data-dir=/tmp/chrome-debug"
  - "--no-first-run"
```

### mcp

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `sse_port` | int | `0` | Port for SSE HTTP transport (0 = stdio mode for Claude Code) |

### docker

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable Docker log integration for full-stack error correlation |
| `containers` | []string | `["symbiogen-backend", "symbiogen-frontend"]` | Container names to monitor |
| `log_window` | string | `"30s"` | How far back to query logs when correlating errors |
| `host` | string | `""` | Docker host (empty = local socket, or `tcp://host:2375`) |

**Docker integration** correlates browser API failures with backend exceptions:
- `api_backend_correlation(ReqId, Url, Status, BackendMsg, TimeDelta)` - links failed requests to backend errors
- `full_stack_error(ConsoleMsg, ReqId, Url, BackendMsg)` - complete chain from browser to backend

### mangle

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enable` | bool | `true` | Enable Mangle causal reasoning engine |
| `schema_path` | string | `"schemas/browser.mg"` | Path to Mangle schema with predicates and rules |
| `fact_buffer_limit` | int | `2048` | Circular buffer size for facts (higher = more history, more memory) |
| `disable_builtin_rules` | bool | `false` | Disable built-in causal reasoning rules |

## Tools

**Session Management:**
- `launch-browser` - Start Chrome (idempotent)
- `shutdown-browser` - Stop Chrome and clear sessions
- `list-sessions` - List active browser tabs
- `create-session` - Open new tab (incognito)
- `attach-session` - Attach to existing tab by TargetID
- `fork-session` - Clone session with auth state (cookies, storage)

**Navigation & Interaction:**
- `navigate-url` - Navigate to URL
- `get-interactive-elements` - Discover clickable elements
- `get-navigation-links` - Extract page links by area (nav, side, main, footer)
- `interact` - Click, type, select, toggle elements
- `fill-form` - Batch fill form fields
- `press-key` - Send keyboard input
- `browser-history` - Navigate back/forward

**Diagnostics:**
- `get-console-errors` - Browser console + Docker container errors
- `get-page-state` - Current URL, title, cookies, storage
- `screenshot` - Capture page screenshot
- `diagnose-page` - Run all diagnostic Mangle queries

**React & DOM:**
- `reify-react` - Extract React Fiber tree as facts
- `snapshot-dom` - Capture DOM structure as facts
- `discover-hidden-content` - Find elements outside viewport

**Mangle Facts & Rules:**
- `push-facts` - Add custom facts
- `read-facts` - Read all facts (optionally filtered)
- `query-facts` - Query facts by predicate
- `query-temporal` - Query facts with time range
- `submit-rule` - Add custom Mangle rule
- `evaluate-rule` - Evaluate rule and return results

**Automation & Waiting:**
- `await-fact` - Wait for predicate to become true
- `await-conditions` - Wait for multiple conditions
- `await-stable-state` - Wait for page to stabilize (no network/DOM activity)
- `wait-for-condition` - Wait for Mangle rule to match
- `execute-plan` - Execute batch of actions from Mangle facts

## Schema

`schemas/browser.mg` defines the Mangle predicates and causal reasoning rules:

**Core Predicates:**
- `dom_node`, `dom_attr`, `dom_text`, `dom_layout` - DOM structure
- `react_component`, `react_prop`, `react_state` - React Fiber tree
- `net_request`, `net_response`, `net_header` - Network events
- `console_event`, `toast_notification` - Console and UI errors
- `navigation_event`, `current_url` - Page navigation
- `docker_log`, `backend_error`, `frontend_ssr_error` - Container logs

**Causal Rules:**
- `caused_by(ConsoleErr, ReqId)` - Console error caused by failed request
- `slow_api(ReqId, Url, Duration)` - API calls exceeding 1 second
- `cascading_failure(ChildReqId, ParentReqId)` - Request chain failures
- `api_backend_correlation(...)` - Browser failure linked to backend exception
- `full_stack_error(...)` - Complete error chain from browser to backend
- `login_succeeded(SessionId)` - Universal login detection

## Claude Code Integration

Add to `.mcp.json`:
```json
{
  "mcpServers": {
    "browsernerd": {
      "command": "C:\\path\\to\\bin\\browsernerd.exe",
      "args": ["--config", "C:\\path\\to\\config.yaml"],
      "type": "stdio"
    }
  }
}
```

## Notes

- **Multi-session**: One browser instance, multiple tabs. Each session has isolated element registry.
- **Session persistence**: Metadata survives server restarts via `session_store`.
- **DOM sampling**: Limited to 200 nodes to control fact volume.
- **Event throttling**: Recommended 100-200ms for production to prevent fact explosion.
- **Headless CI**: Set `headless: true` and `auto_start: true` for CI pipelines.
