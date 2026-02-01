# BrowserNERD

**The token-efficient browser automation MCP server built for AI agents.**

Stop burning 50,000+ tokens on raw HTML dumps. BrowserNERD gives your AI agent structured, actionable browser state in **50-100x fewer tokens** than traditional approaches.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-Compatible-green)](https://modelcontextprotocol.io)

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

**What makes it different:**

- **Structured extraction** - Returns JSON with refs, labels, and actions - not HTML soup
- **Semantic grouping** - Navigation links grouped by page area (nav, sidebar, main, footer)
- **Action-ready refs** - Every element has a `ref` you can pass directly to `interact`
- **Mangle reasoning** - Logic programming engine for causal reasoning and assertions
- **Session persistence** - Detached sessions survive restarts, fork for parallel testing

---

## Features

- **Ultra-Compact Output** - 50-100x fewer tokens than raw HTML
- **Dual Browser Modes** - Auto-launch Chrome OR attach to existing browser instances
- **MCP Protocol** - Works with Claude Desktop, Claude Code, Cursor, and any MCP client
- **Session Management** - Create, attach, fork, and persist browser sessions
- **React Fiber Extraction** - Reify React component trees as queryable facts
- **DOM Snapshotting** - Capture and query DOM state efficiently
- **CDP Event Streaming** - Network, console, and navigation events as Mangle facts
- **Logic Assertions** - Mangle-based causal reasoning for intelligent test assertions

---

## Browser Connection Modes

BrowserNERD supports **two ways** to connect to Chrome:

### Mode 1: Auto-Launch (Zero Config)

BrowserNERD automatically launches and manages Chrome for you:

```
launch-browser  ->  Chrome starts with CDP enabled
create-session  ->  New tab opens
navigate-url    ->  Automate away
shutdown-browser -> Clean exit
```

No manual Chrome setup required. Perfect for automation scripts and CI/CD.

### Mode 2: Attach to Existing Browser

Connect to a Chrome instance you're already using - preserve your logged-in sessions, cookies, and extensions:

```bash
# Start Chrome with remote debugging (one-time setup)
# Windows:
chrome.exe --remote-debugging-port=9222

# macOS:
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222

# Linux:
google-chrome --remote-debugging-port=9222
```

Then configure `config.yaml`:

```yaml
browser:
  auto_start: false
  remote_debugging_url: "ws://localhost:9222"
```

**Why attach mode?**
- Use your existing login sessions (no re-auth needed)
- Keep your extensions active
- Debug alongside your normal browsing
- Perfect for development and testing

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

## Token Efficiency in Action

### Traditional Approach (Playwright/Puppeteer MCP)
```
User: "Click the login button on this page"

1. Get page HTML: 45,000 tokens
2. AI parses HTML to find button: (cognitive overhead)
3. Execute click with selector
4. Get updated HTML: 45,000 tokens

Total: ~90,000 tokens for one click
```

### BrowserNERD Approach
```
User: "Click the login button on this page"

1. get-interactive-elements (filter: buttons): 400 tokens
   Returns: [{ref: "login-btn", label: "Login", action: "click"}]

2. interact(ref: "login-btn", action: "click"): 50 tokens

3. get-page-state: 50 tokens
   Returns: {url: "/dashboard", title: "Dashboard"}

Total: ~500 tokens for one click
```

**That's 180x more efficient.**

---

## MCP Tools Reference

### Navigation & State

| Tool | Tokens | Description |
|------|--------|-------------|
| `get-page-state` | ~50 | URL, title, loading state, scroll position |
| `get-navigation-links` | ~150-400 | All links grouped by page area |
| `get-interactive-elements` | ~300-600 | Buttons, inputs, links with action refs |
| `navigate-url` | ~50 | Navigate and wait for load |

### Interaction

| Tool | Tokens | Description |
|------|--------|-------------|
| `interact` | ~50 | Click, type, select, toggle elements |
| `fill-form` | ~100 | Fill multiple form fields at once |
| `screenshot` | ~100 | Capture to file (no base64 bloat) |

### Sessions

| Tool | Tokens | Description |
|------|--------|-------------|
| `launch-browser` | ~50 | Start Chrome with CDP |
| `create-session` | ~100 | New tab with optional URL |
| `fork-session` | ~100 | Clone session with auth state |
| `list-sessions` | ~50-200 | All active sessions |

### Mangle Reasoning

| Tool | Tokens | Description |
|------|--------|-------------|
| `push-facts` | ~50 | Add facts to knowledge base |
| `query-facts` | ~100-500 | Run Mangle queries |
| `await-fact` | ~50 | Wait for condition |
| `reify-react` | ~200-1000 | Extract React component tree |

---

## Configuration

```yaml
server:
  name: "browsernerd-mcp"
  version: "0.0.1"
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

## Mangle: Logic Programming for Browser State

BrowserNERD uses [Google's Mangle](https://github.com/google/mangle) for declarative reasoning about browser state:

```mangle
# Facts emitted by browser tools
nav_link(Ref, Href, Area, Internal).
interactive(Ref, Type, Label, Action).
react_component(SessionId, ComponentId, Name, ParentId).
dom_node(SessionId, NodeId, Tag, ParentId).

# Query: Find all internal navigation links
internal_nav(Ref, Href) :- nav_link(Ref, Href, _, true).

# Query: Find React components with specific props
auth_component(Id) :-
  react_component(_, Id, "AuthProvider", _),
  react_prop(Id, "authenticated", "true").
```

---

## Architecture

```
browserNerd/
+-- mcp-server/                 # Go MCP server
|   +-- cmd/server/             # Entry point
|   +-- internal/
|   |   +-- browser/            # Rod session management
|   |   +-- mcp/                # MCP tools
|   |   +-- mangle/             # Fact engine
|   |   +-- config/             # Configuration
|   +-- schemas/                # Mangle schemas
+-- docs/                       # Documentation
+-- LICENSE                     # Apache 2.0
+-- NOTICE                      # Attribution
```

**Built on:**
- [Rod](https://github.com/go-rod/rod) - High-performance Chrome DevTools Protocol
- [Mangle](https://github.com/google/mangle) - Google's logic programming language
- [mcp-go](https://github.com/mark3labs/mcp-go) - MCP protocol for Go

---

## Comparison with Alternatives

| Feature | BrowserNERD | Playwright MCP | Puppeteer MCP | Browser-Use |
|---------|-------------|----------------|---------------|-------------|
| Token efficiency | 50-100x better | Baseline | Baseline | ~2-3x better |
| Structured output | JSON with refs | Raw HTML/selectors | Raw HTML | JSON |
| Browser modes | Launch OR attach | Launch only | Launch only | Launch only |
| Session persistence | Yes (survives restart) | No | No | No |
| Logic reasoning | Mangle built-in | None | None | None |
| React extraction | Native | Manual | Manual | No |
| Multi-session | Fork with auth | New session | New session | Limited |

---

## Development

```bash
# Run tests
cd mcp-server && go test ./...

# Verbose logging
./bin/browsernerd --config config.yaml --verbose
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
