# BrowserNERD

**The token-efficient browser automation MCP server built for AI agents.**

Stop burning 50,000+ tokens on raw HTML dumps. BrowserNERD gives your AI agent structured, actionable browser state in **50-100x fewer tokens** than traditional approaches - plus built-in causal reasoning, React extraction, and full-stack error correlation.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-Compatible-green)](https://modelcontextprotocol.io)
[![Tools](https://img.shields.io/badge/MCP_Tools-34-blue)](https://modelcontextprotocol.io)

---

## Why BrowserNERD?

### The Problem with Existing Browser Automation

Traditional browser automation for AI agents is **catastrophically token-inefficient**:

| Approach | Tokens for GitHub Repo Page | Usable? |
|----------|----------------------------|---------|
| Raw HTML dump | ~50,000 tokens | Barely |
| Screenshot + Vision | ~2,000-5,000 tokens | Slow, lossy |
| Full DOM serialization | ~30,000 tokens | Overwhelming |
| **BrowserNERD** | **~500-800 tokens** | Structured, actionable |

### The BrowserNERD Advantage

**50-100x more token efficient.** Real benchmarks from live testing:

```
GitHub repository page (github.com/anthropics/claude-code):
  - get-navigation-links: 30 links in ~400 tokens
  - get-interactive-elements: 10 buttons in ~350 tokens
  - get-page-state: Full status in ~50 tokens

Hacker News front page:
  - get-navigation-links: 10 links in ~150 tokens
  - get-interactive-elements: 20 elements in ~450 tokens
```

---

## Key Features

### Token Efficiency
- **Structured JSON output** - Refs, labels, and actions - not HTML soup
- **Semantic grouping** - Navigation links grouped by page area (nav, sidebar, main, footer)
- **Action-ready refs** - Every element has a `ref` for direct interaction
- **Batch automation** - `execute-plan` runs multiple actions in one call

### Dual Browser Modes
- **Auto-launch** - BrowserNERD starts Chrome automatically via Rod
- **Attach to existing** - Connect to your browser, preserve logins and cookies

### React Intelligence
- **Fiber tree extraction** - Full React component hierarchy as Mangle facts
- **Props and state** - Query component props and hook state
- **DOM mapping** - Link Fiber nodes to DOM elements

### Mangle Reasoning Engine
- **70+ built-in predicates** - DOM, network, React, console, toasts
- **25+ causal reasoning rules** - Automatic root cause analysis
- **Semantic UI Macros** - Detect modals, main content, and primary actions (Vector 14)
- **Custom rule submission** - Define your own derived facts
- **Temporal queries** - Time-windowed fact analysis

### Full-Stack Error Correlation
- **Console error tracking** - With causal API correlation
- **Toast/notification detection** - Instant error overlay capture
- **Docker log integration** - Correlate browser errors with backend containers
- **Root cause analysis** - Automatic error chain detection

### Session Management
- **Persistent sessions** - Survive server restarts
- **Fork with auth** - Clone sessions preserving login state
- **Multi-session** - Run parallel automation flows

---

## Quick Start

### Requirements

- **Go 1.21+** - Required to build the MCP server
- **Chrome/Chromium** - Browser to automate (auto-detected or configurable)

### Build & Run

```bash
# Clone the repository
git clone https://github.com/theRebelliousNerd/browserNerd.git
cd browserNerd/mcp-server

# Build
go mod tidy
go build -o bin/browsernerd ./cmd/server

# Run (stdio mode for Claude Desktop/Cursor)
./bin/browsernerd --config config.yaml
```

### Add to Claude Desktop

Edit `~/.config/claude/mcp.json` (Linux/macOS) or `%APPDATA%\Claude\mcp.json` (Windows):

```json
{
  "mcpServers": {
    "browsernerd": {
      "command": "/path/to/browsernerd",
      "args": ["--config", "/path/to/config.yaml"]
    }
  }
}
```

---

## Browser Connection Modes

### Mode 1: Auto-Launch (Zero Config)

BrowserNERD automatically launches and manages Chrome via Rod:

```
launch-browser  ->  Chrome starts with CDP enabled
create-session  ->  New tab opens
navigate-url    ->  Automate away
shutdown-browser -> Clean exit
```

### Mode 2: Attach to Existing Browser

Connect to a Chrome instance you're already using:

```bash
# Start Chrome with remote debugging
chrome --remote-debugging-port=9222
```

Configure `config.yaml`:
```yaml
browser:
  auto_start: false
  remote_debugging_url: "ws://localhost:9222"
```

**Benefits:** Preserve logins, cookies, extensions. Debug alongside normal browsing.

---

## Complete Tool Reference (34 Tools)

### Session Management (8 tools)

| Tool | Description |
|------|-------------|
| `launch-browser` | Start Chrome with CDP enabled (idempotent) |
| `shutdown-browser` | Close Chrome and clean up all sessions |
| `list-sessions` | List all active browser sessions |
| `create-session` | Open new tab with optional starting URL |
| `attach-session` | Attach to existing CDP target by ID |
| `fork-session` | Clone session preserving auth state (cookies, localStorage) |
| `reify-react` | Extract React Fiber tree as Mangle facts |
| `snapshot-dom` | Capture DOM structure as Mangle facts |

### Navigation & State (4 tools)

| Tool | Tokens | Description |
|------|--------|-------------|
| `get-page-state` | ~50 | URL, title, loading state, scroll position, active element |
| `get-navigation-links` | ~150-400 | All links grouped by page area (nav/side/main/footer) |
| `get-interactive-elements` | ~300-600 | Buttons, inputs, links, selects with action refs |
| `navigate-url` | ~50 | Navigate with wait options (load/networkidle/none) |

### Browser Interaction (7 tools)

| Tool | Description |
|------|-------------|
| `interact` | Click, type, select, toggle, clear elements by ref |
| `fill-form` | Fill multiple form fields in one call (token efficient) |
| `press-key` | Send keyboard input (Enter, Tab, Escape, characters) |
| `browser-history` | Navigate back, forward, or reload |
| `discover-hidden-content` | Find elements hidden by CSS/JS |
| `screenshot` | Capture page/element to file (no base64 bloat) |
| `evaluate-js` | Execute JavaScript in browser context |

### Mangle-Driven Automation (4 tools)

| Tool | Description |
|------|-------------|
| `execute-plan` | Run batch actions from Mangle facts (MASSIVE token savings) |
| `wait-for-condition` | Wait until Mangle predicate matches (with wildcards) |
| `await-stable-state` | Block until network idle AND DOM settled |
| `diagnose-page` | One-shot page health check via Mangle queries |

### Mangle Fact Operations (10 tools)

| Tool | Description |
|------|-------------|
| `push-facts` | Inject facts into the knowledge base |
| `read-facts` | View recent facts in the buffer |
| `query-facts` | Run Mangle queries with variable binding |
| `query-temporal` | Query facts in a time window |
| `submit-rule` | Add derivation rules at runtime |
| `evaluate-rule` | Check if a rule matches right now |
| `subscribe-rule` | Push-based notification when rule triggers |
| `await-fact` | Wait for a specific fact to appear |
| `await-conditions` | Wait for multiple facts (AND logic) |

### Diagnostics (3 tools)

| Tool | Description |
|------|-------------|
| `get-console-errors` | Console errors with root cause analysis + Docker correlation |
| `get-toast-notifications` | Detect toast/snackbar overlays with API correlation |

---

## Token Efficiency in Action

### Traditional Approach
```
User: "Click the login button"

1. Get page HTML: 45,000 tokens
2. AI parses HTML to find button
3. Execute click with selector
4. Get updated HTML: 45,000 tokens

Total: ~90,000 tokens
```

### BrowserNERD Approach
```
User: "Click the login button"

1. get-interactive-elements (filter: buttons): 400 tokens
   Returns: [{ref: "login-btn", label: "Login", action: "click"}]

2. interact(ref: "login-btn", action: "click"): 50 tokens

3. get-page-state: 50 tokens

Total: ~500 tokens (180x more efficient)
```

### execute-plan: Ultimate Token Efficiency

Instead of individual tool calls:
```
1. type email: 50 tokens
2. type password: 50 tokens
3. click submit: 50 tokens
4. wait for navigation: 50 tokens
Total: 200 tokens + 4 round trips
```

With execute-plan:
```
execute-plan({
  actions: [
    {type: "type", ref: "email", value: "user@test.com"},
    {type: "type", ref: "password", value: "secret"},
    {type: "click", ref: "submit"},
    {type: "wait", value: "1000"}
  ]
})
Total: ~100 tokens, 1 round trip
```

---

## Mangle: Logic Programming for Browser State

BrowserNERD uses [Google's Mangle](https://github.com/google/mangle) for declarative reasoning.

### Built-in Predicates (60+)

**React Fiber:**
```mangle
react_component(FiberId, ComponentName, ParentId).
react_prop(FiberId, Key, Value).
react_state(FiberId, HookIndex, Value).
```

**DOM & Navigation:**
```mangle
dom_node(NodeId, Tag, Text, ParentId).
dom_attr(NodeId, Key, Value).
navigation_event(SessionId, Url, Timestamp).
current_url(SessionId, Url).
```

**Network (HAR-like):**
```mangle
net_request(Id, Method, Url, InitiatorId, StartTime).
net_response(Id, Status, Latency, Duration).
net_header(Id, Kind, Key, Value).
```

**Interactive Elements:**
```mangle
interactive(Ref, Type, Label, Action).
nav_link(Ref, Href, Area, Internal).
user_click(Ref, Timestamp).
user_type(Ref, Value, Timestamp).
```

**Diagnostics:**
```mangle
console_event(Level, Message, Timestamp).
toast_notification(Text, Level, Source, Timestamp).
docker_log(Container, Level, Tag, Message, Timestamp).
```

### Built-in Causal Reasoning Rules (20+)

**API-Triggered Crash Detection:**
```mangle
caused_by(ConsoleErr, ReqId) :-
    console_event("error", ConsoleErr, TError),
    net_response(ReqId, Status, _, _),
    Status >= 400,
    TNet < TError,
    fn:minus(TError, TNet) < 100.
```

**Slow API Detection (>1 second):**
```mangle
slow_api(ReqId, Url, Duration) :-
    net_request(ReqId, _, Url, _, _),
    net_response(ReqId, _, _, Duration),
    Duration > 1000.
```

**Full-Stack Error Correlation:**
```mangle
full_stack_error(ConsoleMsg, ReqId, Url, BackendMsg) :-
    caused_by(ConsoleMsg, ReqId),
    net_request(ReqId, _, Url, _, _),
    api_backend_correlation(ReqId, Url, _, BackendMsg, _).
```

**Universal Login Detection:**
```mangle
login_succeeded(SessionId) :-
    url_changed_after_submit(SessionId, _, _, TNav),
    form_submitted(SessionId, _, TSubmit),
    successful_post(_, _, TPost),
    fn:minus(TNav, TSubmit) < 5000.
```

### Custom Rules

Submit rules at runtime:
```
submit-rule("ready() :- navigation_event(_, \"/dashboard\", _), dom_text(_, \"Welcome\").")
wait-for-condition(predicate: "ready", timeout_ms: 10000)
```

---

## Docker Log Integration

Enable full-stack error correlation by connecting to backend containers:

```yaml
docker:
  enabled: true
  containers:
    - symbiogen-backend
    - symbiogen-frontend
  log_window: 60s
```

**What it does:**
- Queries backend container logs when errors occur
- Correlates browser API failures with backend exceptions
- Provides full chain: Browser console -> Failed API -> Backend error
- Analyzes container health status

**Example output from get-console-errors:**
```json
{
  "errors": [{
    "message": "TypeError: Cannot read property 'map' of undefined",
    "caused_by": {
      "request_id": "req-123",
      "url": "/api/users",
      "status": 500
    }
  }],
  "backend_correlations": [{
    "request_id": "req-123",
    "container": "symbiogen-backend",
    "backend_error": "KeyError: 'users'",
    "time_delta_ms": 45
  }],
  "container_health": {
    "symbiogen-backend": {"status": "degraded", "error_count": 3}
  }
}
```

---

## Configuration

```yaml
server:
  name: "browsernerd-mcp"
  version: "0.0.2"
  log_file: "data/browsernerd-mcp.log"

browser:
  auto_start: true           # Auto-launch Chrome
  headless: false            # Visible UI (true for CI)
  enable_dom_ingestion: true # Capture DOM as facts
  enable_header_ingestion: true

mangle:
  enable: true
  schema_path: "schemas/browser.mg"
  fact_buffer_limit: 10000

docker:
  enabled: false             # Enable for full-stack correlation
  containers: []             # Container names to monitor
  log_window: 60s            # How far back to query logs
```

---

## Cross-Platform Builds

```bash
cd mcp-server

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/browsernerd.exe ./cmd/server

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o bin/browsernerd-darwin-arm64 ./cmd/server

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o bin/browsernerd-darwin-amd64 ./cmd/server

# Linux
GOOS=linux GOARCH=amd64 go build -o bin/browsernerd-linux-amd64 ./cmd/server
```

---

## Comparison with Alternatives

| Feature | BrowserNERD | Playwright MCP | Puppeteer MCP | Browser-Use |
|---------|-------------|----------------|---------------|-------------|
| Token efficiency | 50-100x better | Baseline | Baseline | ~2-3x better |
| Structured output | JSON with refs | Raw HTML/selectors | Raw HTML | JSON |
| Browser modes | Launch OR attach | Launch only | Launch only | Launch only |
| Session persistence | Yes (survives restart) | No | No | No |
| React extraction | Native Fiber | Manual | Manual | No |
| Mangle reasoning | 60+ predicates | None | None | None |
| Causal analysis | 20+ built-in rules | None | None | None |
| Docker correlation | Full-stack | None | None | None |
| Toast detection | Native | Manual | Manual | No |
| Batch automation | execute-plan | Individual calls | Individual calls | Limited |
| Fork with auth | Yes | No | No | No |

---

## Architecture

```
browserNerd/
+-- mcp-server/                 # Go MCP server
|   +-- cmd/server/             # Entry point
|   +-- internal/
|   |   +-- browser/            # Rod session management, CDP events
|   |   +-- mcp/                # MCP server, 34 tool implementations
|   |   +-- mangle/             # Fact engine, rule evaluation
|   |   +-- config/             # YAML configuration
|   |   +-- docker/             # Container log integration
|   +-- schemas/                # Mangle predicates and rules
+-- LICENSE                     # Apache 2.0
+-- NOTICE                      # Attribution
```

**Built on:**
- [Rod](https://github.com/go-rod/rod) - High-performance Chrome DevTools Protocol
- [Mangle](https://github.com/google/mangle) - Google's logic programming language
- [mcp-go](https://github.com/mark3labs/mcp-go) - MCP protocol for Go

---

## Development

```bash
# Run tests
cd mcp-server && go test ./...

# Verbose logging
./bin/browsernerd --config config.yaml --verbose

# SSE mode (HTTP clients)
./bin/browsernerd --config config.yaml --sse-port 8080
```

---

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

### Citation

If you use BrowserNERD in your research or projects, please cite:

```bibtex
@software{browsernerd,
  author = {theRebelliousNerd},
  title = {BrowserNERD: Token-Efficient Browser Automation with Mangle Reasoning},
  year = {2024-2026},
  url = {https://github.com/theRebelliousNerd/browserNerd}
}
```

---

## Contributing

Contributions welcome! Fork, branch, and PR.

---

**Stop wasting tokens. Start browsing smarter.**
