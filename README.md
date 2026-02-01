# BrowserNERD

**Browser automation with Mangle reasoning** - An MCP server that combines Chrome DevTools Protocol automation via [Rod](https://github.com/go-rod/rod) with [Mangle](https://github.com/google/mangle) declarative logic programming for intelligent browser control.

## Features

- **MCP Protocol** - Works with Claude Desktop, Cursor, and other MCP-compatible clients
- **Session Management** - Create, attach, fork, and persist browser sessions
- **React Fiber Extraction** - Reify React component trees as Mangle facts
- **DOM Snapshotting** - Capture and query DOM state
- **CDP Event Streaming** - Network, console, and navigation events as facts
- **Logic Assertions** - Mangle-based causal reasoning and test assertions

## Requirements

- **Go 1.21+** - Required to build the MCP server
- **Chrome/Chromium** - Browser to automate (auto-detected or configurable)

## Installation

### From Source (Recommended)

```bash
# Clone the repository
git clone https://github.com/theRebelliousNerd/browserNerd.git
cd browserNerd/mcp-server

# Install dependencies
go mod tidy

# Build the binary
go build -o bin/browsernerd ./cmd/server

# (Optional) Install to your PATH
# Linux/macOS:
sudo cp bin/browsernerd /usr/local/bin/
# Windows: Copy bin/browsernerd.exe to a directory in your PATH
```

### Cross-Platform Builds

```bash
# Build for all platforms
cd mcp-server

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/browsernerd.exe ./cmd/server

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o bin/browsernerd-darwin-amd64 ./cmd/server

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o bin/browsernerd-darwin-arm64 ./cmd/server

# Linux
GOOS=linux GOARCH=amd64 go build -o bin/browsernerd-linux-amd64 ./cmd/server
```

## Configuration

Copy and customize the example configuration:

```bash
cp mcp-server/config.example.yaml mcp-server/config.yaml
```

### config.yaml

```yaml
server:
  name: "browsernerd-mcp"
  version: "0.0.1"
  log_file: "data/browsernerd-mcp.log"

browser:
  auto_start: true           # Auto-launch Chrome on first session
  headless: false            # Run with visible UI (set true for CI)
  enable_dom_ingestion: true # Capture DOM as facts
  enable_header_ingestion: true # Capture HTTP headers

mangle:
  enable: true
  schema_path: "schemas/browser.mg"  # Mangle schema definitions
  fact_buffer_limit: 10000           # Max facts in memory
```

## Usage

### Running the MCP Server

```bash
# stdio mode (for Claude Desktop, Cursor)
./mcp-server/bin/browsernerd --config mcp-server/config.yaml

# SSE mode (for HTTP-based clients)
./mcp-server/bin/browsernerd --config mcp-server/config.yaml --sse-port 8080
```

### Claude Desktop Integration

Add to your Claude Desktop MCP configuration (`~/.config/claude/mcp.json` on Linux/macOS or `%APPDATA%\Claude\mcp.json` on Windows):

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

### Cursor Integration

Add to your Cursor MCP settings:

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

## Available MCP Tools

### Session Management

| Tool | Description |
|------|-------------|
| `list-sessions` | List all active browser sessions |
| `create-session` | Create a new browser session |
| `attach-session` | Attach to an existing session |
| `fork-session` | Clone a session with its state |

### Browser Automation

| Tool | Description |
|------|-------------|
| `navigate-url` | Navigate to a URL |
| `screenshot` | Capture page screenshot |
| `interact` | Click, type, or interact with elements |
| `get-page-state` | Get current page URL and title |

### React Reification

| Tool | Description |
|------|-------------|
| `reify-react` | Extract React component tree as Mangle facts |

### DOM Operations

| Tool | Description |
|------|-------------|
| `snapshot-dom` | Capture DOM as Mangle facts |
| `get-interactive-elements` | List clickable/interactive elements |
| `get-navigation-links` | Extract all navigation links |

### Mangle Fact Operations

| Tool | Description |
|------|-------------|
| `push-facts` | Add facts to the knowledge base |
| `read-facts` | Query facts from the knowledge base |
| `query-facts` | Run Mangle queries |
| `await-fact` | Wait for a specific fact to appear |
| `await-conditions` | Wait for multiple conditions |

## Mangle Schema

The `schemas/browser.mg` file defines predicates for browser state:

```mangle
# React component facts
react_component(session_id, component_id, name, parent_id).
react_prop(component_id, key, value).
react_state(component_id, key, value).

# DOM facts
dom_node(session_id, node_id, tag, parent_id).
dom_attr(node_id, key, value).

# Network facts
net_request(session_id, request_id, url, method).
net_response(request_id, status, content_type).

# Navigation facts
navigation_event(session_id, url, timestamp).
current_url(session_id, url).

# Console facts
console_event(session_id, level, message, timestamp).
```

## Architecture

```
browserNerd/
+-- mcp-server/                 # Go MCP server implementation
|   +-- cmd/server/             # Entry point (main.go)
|   +-- internal/
|   |   +-- browser/            # Rod session management, CDP events
|   |   +-- mcp/                # MCP protocol, tool definitions
|   |   +-- mangle/             # Mangle fact engine
|   |   +-- config/             # YAML configuration
|   +-- schemas/                # Mangle schema definitions
|   +-- bin/                    # Compiled binaries
|   +-- data/                   # Runtime data (sessions, logs)
+-- docs/                       # Documentation
|   +-- mangle-programming-references/  # Mangle language guides
+-- LICENSE                     # Apache 2.0
+-- NOTICE                      # Attribution requirements
```

## Development

### Running Tests

```bash
cd mcp-server
go test ./...
```

### Running with Verbose Logging

```bash
./bin/browsernerd --config config.yaml --verbose
```

### Attaching to Existing Chrome

Start Chrome with remote debugging:

```bash
# Windows
chrome.exe --remote-debugging-port=9222

# macOS
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222

# Linux
google-chrome --remote-debugging-port=9222
```

Then configure `config.yaml`:

```yaml
browser:
  auto_start: false
  remote_debugging_url: "ws://localhost:9222"
```

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

### Citation

If you use BrowserNERD in your research or projects, please cite:

```bibtex
@software{browsernerd,
  author = {theRebelliousNerd},
  title = {BrowserNERD: Browser Automation with Mangle Reasoning},
  year = {2024-2026},
  url = {https://github.com/theRebelliousNerd/browserNerd}
}
```

See [NOTICE](NOTICE) for full attribution requirements.

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Submit a pull request

## Related Projects

- [Rod](https://github.com/go-rod/rod) - Chrome DevTools Protocol library for Go
- [Mangle](https://github.com/google/mangle) - Google's declarative logic programming language
- [mcp-go](https://github.com/mark3labs/mcp-go) - MCP protocol implementation for Go
