---
title: BrowserNERD
created: 2026-01-31
last_updated: 2026-07-23
doc_type: readme
subsystem: infra
read_when: Orienting in this directory
indexes: []
---

# BrowserNERD

**The token-efficient browser automation MCP server built for AI agents.**

Stop burning 50,000+ tokens on raw HTML dumps. BrowserNERD gives your AI agent structured, actionable browser state in **50-100x fewer tokens** than traditional approaches - plus built-in causal reasoning, React extraction, and full-stack error correlation.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-Compatible-green)](https://modelcontextprotocol.io)
[![Tools](https://img.shields.io/badge/MCP_Tools-48-blue)](https://modelcontextprotocol.io)

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

### What Is New In v1.1.0

- **Security-first capture** - credential redaction, private traces, confined
  exports, trusted workspace authority, safe page arguments, and bounded Mangle
  execution.
- **Multi-tab by default** - shared-context tabs, isolated forks, focus/close,
  browser inventory, and multiple managed Chrome instances.
- **Configurable spec delivery** - named Markdown corpora and optional indexes,
  with compact spec context attached to observe and act results.
- **Correctness fixes** - complete query bodies, fresh-fact watches, correlated
  network capture, strict since-navigation deltas, and working JPEG output.
- **Modular core and CI** - production Go files stay below 1,500 lines, with
  Windows and Linux verification.

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

### Contract Auditing
- **Recursive frontend-to-API tracing** - Start from live browser context, then recurse into repo evidence, request sites, route hints, and backend expectations
- **Safe phased audit flow** - `discover` builds a deterministic plan, `execute` replays only allowed steps, `report` synthesizes findings, `resume` re-opens a focused slice
- **Auth contract checks** - Highlight missing JWT, missing API-key wiring, and auth-mechanism drift
- **Payload contract checks** - Flag required-field and frontend/backend payload mismatches
- **Repo-backed evidence handles** - Return likely source files, route correlations, and evidence handles for deeper debugging with `browser-mangle`
- **Per-step runtime evidence** - Capture route changes, toasts, console errors, and recent request ids after execute-mode steps

### Session Management
- **Persistent sessions** - Survive server restarts
- **Fork with auth** - Clone sessions preserving login state
- **Multi-tab default** - Reuse login state across concurrent tabs
- **Multi-browser** - Run separate Chrome instances and profiles concurrently

---

## Quick Start

### Requirements

- **Go 1.26.5** - Required to build the MCP server
- **Chrome/Chromium** - Browser to automate (auto-detected or configurable)

### Build & Run

```bash
# Clone the repository
git clone https://github.com/theRebelliousNerd/browserNerd.git
cd browserNerd/mcp-server

# Build
go mod tidy
go build -o bin/browsernerd ./cmd/server

# Run with a checked-in project workspace
./bin/browsernerd --workspace-dir /path/to/project

# Run with machine-level config only
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

### Multi-Project MCP Setup

BrowserNERD is designed so each target repository can own its own `.browsernerd/` directory:

- Run `--init-workspace` once per repo to scaffold `.browsernerd/config.yaml`, `.browsernerd/schemas/`, and `.browsernerd/data/`.
- Use `--workspace-dir <repo-root>` in Claude or Codex configs when the MCP host launches tools from a shared home/tools directory.
- Use `--no-workspace` only when you want a shared global BrowserNERD entry with no repo-local overrides.

Pinned-project Claude Desktop example:

```json
{
  "mcpServers": {
    "browsernerd-cross-thread": {
      "command": "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe",
      "args": [
        "--workspace-dir",
        "C:\\CodeProjects\\SybioGenv3\\crossthread"
      ]
    }
  }
}
```

Pinned-project and shared-global Codex examples:

```toml
[mcp_servers.browsernerd_cross-thread]
command = "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe"
args = ["--workspace-dir", "C:\\CodeProjects\\SybioGenv3\\crossthread"]

[mcp_servers.browsernerd_shared]
command = "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe"
args = ["--no-workspace", "--config", "C:\\Users\\you\\.browsernerd\\global.yaml"]
```

Create one MCP entry per project when you want different repo-trace defaults, schema paths, or Docker container lists.

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
  debugger_url: "ws://localhost:9222"
```

**Benefits:** Preserve logins, cookies, extensions. Debug alongside normal browsing.

---

## Complete Tool Reference (48 Tools)

### Session Management (11 tools)

| Tool | Description |
|------|-------------|
| `launch-browser` | Start Chrome with CDP enabled (idempotent) |
| `shutdown-browser` | Close Chrome and clean up all sessions |
| `list-sessions` | List all active browser sessions |
| `create-session` | Open new tab with optional starting URL |
| `attach-session` | Attach to existing CDP target by ID |
| `list-browsers` | List Chrome instances and tab counts |
| `focus-session` | Activate one tracked tab |
| `close-session` | Idempotently close one tab and its event streams |
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

### Browser Interaction (8 tools)

| Tool | Description |
|------|-------------|
| `interact` | Click, type, select, toggle, clear elements by ref |
| `fill-form` | Fill multiple form fields in one call (token efficient) |
| `press-key` | Send keyboard input (Enter, Tab, Escape, characters) |
| `browser-history` | Navigate back, forward, or reload |
| `discover-hidden-content` | Find elements hidden by CSS/JS |
| `discover-grids` | Find virtualized grids and bounded row samples |
| `screenshot` | Capture page/element to file (no base64 bloat) |
| `evaluate-js` | Trusted-config, gated JavaScript escape hatch (disabled by default) |

### Mangle-Driven Automation (4 tools)

| Tool | Description |
|------|-------------|
| `execute-plan` | Run batch actions from Mangle facts (MASSIVE token savings) |
| `wait-for-condition` | Wait until Mangle predicate matches (with wildcards) |
| `await-stable-state` | Block until network idle AND DOM settled |
| `diagnose-page` | One-shot page health check via Mangle queries |

### Mangle Fact Operations (9 tools)

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

### Progressive Disclosure (5 tools)

| Tool | Description |
|------|-------------|
| `browser-observe` | Progressive-disclosure page observation (modes: state/nav/interactive/hidden/composite, views: summary/compact/full) |
| `browser-act` | Progressive-disclosure action execution with intent presets |
| `browser-reason` | Progressive-disclosure Mangle reasoning with causal analysis |
| `browser-audit` | Progressive frontend-to-API contract auditing with evidence handles |
| `browser-mangle` | Bounded queries, rules, temporal reads, and confined flight export |

### Diagnostics (2 tools)

| Tool | Description |
|------|-------------|
| `get-console-errors` | Console errors with root cause analysis + Docker correlation |
| `get-toast-notifications` | Detect toast/snackbar overlays with API correlation |

### Testing & Spec Delivery (5 tools)

| Tool | Description |
|------|-------------|
| `browser-test` | Progressive create, inspect, and run surface using `browser-act` operations and bounded Mangle assertions |
| `run-test` | Run a declarative test: replay actions, evaluate Mangle assertions, return a causal chain on failure |
| `generate-test` | Turn a recorded interaction (action facts) into a draft `run-test` spec |
| `get-specs` | Deliver spec invariants governing a component, route, selector, or the file line-range you're editing |
| `check-specs` | Evaluate spec invariants against current state, report violations with causal diagnosis |

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

## Progressive Disclosure: Right Detail at the Right Time

v0.0.4 introduces **progressive disclosure tools** that let AI agents control exactly how much detail they receive. Instead of getting everything and filtering, agents request the precision level they need:

### browser-observe

One tool replaces multiple observation calls with configurable modes and views:

```
browser-observe(mode: "composite", view: "summary")
  -> Page state + nav links + interactive elements in ~200 tokens

browser-observe(mode: "interactive", view: "full", filter: "button")
  -> All buttons with full details for form automation

browser-observe(intent: "quick_status")
  -> Minimal page state check in ~50 tokens
```

**Modes:** `state` | `nav` | `interactive` | `hidden` | `composite`
**Views:** `summary` (minimal) | `compact` (practical) | `full` (diagnostic)
**Intents:** `quick_status` | `find_actions` | `map_navigation` | `hidden_content` | `deep_audit`

### browser-act

Consolidated action execution with progressive feedback:

```
browser-act(operations: [{type: "interact", action: "click", ref: "submit"}], view: "compact")
  -> Execute + return only what changed
```

### browser-reason

Mangle-powered reasoning with adjustable depth:

```
browser-reason(intent: "errors", view: "summary")
  -> Quick error count and top issue

browser-reason(intent: "performance", view: "full")
  -> Full slow API analysis with correlation chains
```

### browser-audit

Progressive contract auditing with persisted runs and evidence handles:

```
browser-audit(session_id: "s1", repo_root: "/repo", phase: "discover", view: "compact")
  -> Passive audit plan + hazard list + report handles

browser-audit(session_id: "s1", repo_root: "/repo", phase: "execute", audit_id: "audit-123")
  -> Replays safe steps from the persisted plan and leaves risky steps skipped unless allowed

browser-audit(session_id: "s1", repo_root: "/repo", phase: "resume", resume_handle: "audit:s1:mangle_contracts")
  -> Re-opens only the selected evidence slice instead of returning the full report again
```

**Phases:** `discover` (passive) | `execute` (plan replay with allow flags) | `report` (default full synthesis) | `resume` (handle-focused follow-up)
**Key args:** `session_id` + `repo_root` required on entry, optional `audit_id`, `resume_handle`, `expand_handles`
**Allow flags:** `allow_risky` | `allow_navigation` | `allow_mutating` | `allow_destructive`

What the phased flow gives you:

- `discover` returns `audit_plan`, `audit_hazards`, `report_handles`, and `approval_required` without mutating page state
- `execute` persists `completed_steps` and `skipped_steps`, then recursively appends newly revealed safe follow-up steps when the page state expands
- `report` returns ranked findings plus handles like `audit:<session>:contract_findings`, `audit:<session>:mangle_contracts`, `audit:<session>:repo_matches`, and `audit:<session>:repo_trace`
- `resume` narrows the response to one or more selected evidence handles instead of replaying the whole report

### Audit Workflow

Use the phased audit loop when you want a reproducible browser-to-code investigation instead of a one-shot dump:

1. `discover` a passive plan
2. `execute` only the steps you explicitly allow
3. `report` the full contract synthesis
4. `resume` by handle when you only need one evidence slice

Example sequence:

```json
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"discover","view":"compact","include_repo_matches":true}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"execute","audit_id":"audit-123","view":"compact"}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"execute","audit_id":"audit-123","allow_mutating":true,"allow_navigation":true,"view":"compact"}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"report","audit_id":"audit-123","view":"compact","include_repo_matches":true}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"resume","audit_id":"audit-123","resume_handle":"audit:<session>:mangle_contracts","view":"compact"}}
```

---

## MCP Resources

BrowserNERD exposes read-only MCP resources for context-aware integrations:

| Resource | Description |
|----------|-------------|
| `browsernerd://about` | Server name, version, and usage notes |
| `browsernerd://session/{sessionId}/facts?predicate=X&limit=N` | Token-efficient fact slice for a session, filtered by predicate |

---

## Mangle: Logic Programming for Browser State

BrowserNERD uses [TauCeti Mangle Go v0.5.0](https://codeberg.org/TauCeti/mangle-go) for declarative reasoning.

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

**Contract Audit:**
```mangle
request_payload_field(SessionId, RequestId, Field, ValueKind).
audit_plan_state(SessionId, AuditId, Phase, Status, Timestamp).
audit_discovered_action(SessionId, AuditId, Step, Ref, Action, Label, Route).
scoped_audit_run(SessionId, RunId, Focus, StartedAt).
scoped_audit_run_completed_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, CompletedAt).
scoped_audit_run_skipped_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, SkipReason, Timestamp).
scoped_audit_run_resume_action(SessionId, RunId, ActionKind, TargetRef, PageRoute, ApiRoute, RequestId, Priority, HazardClass, MutabilityClass, Reason).
scoped_missing_jwt_or_auth_header(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ExpectedMechanism).
scoped_missing_api_key(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method).
scoped_auth_mechanism_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, ObservedMechanism, ExpectedMechanism).
scoped_payload_requirement_mismatch(SessionId, ActionRef, PageRoute, RequestId, ApiRoute, Method, Field, Requirement).
scoped_frontend_backend_contract_gap(SessionId, ActionRef, PageRoute, ApiRoute, Method, Aspect, FrontendValue, BackendValue).
repo_trace_frontend_request_site(TraceId, SeedId, File, Line, Confidence, Route, Method, ApiRoute).
repo_trace_backend_expectation(TraceId, Route, Method, ApiRoute, AuthKind, PayloadHint, Confidence).
```

These let BrowserNERD reason about:

- missing JWT or auth header wiring
- missing API-key headers
- frontend/backend auth-mechanism drift
- missing required payload fields
- route- and contract-level mismatches between UI and API expectations

### Built-in Causal Reasoning Rules (20+)

**API-Triggered Crash Detection:**
```mangle
caused_by(ConsoleErr, ReqId) :-
    console_event("error", ConsoleErr, TError),
    net_response(ReqId, Status, _, _),
    net_request(ReqId, _, _, _, TNet),
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
    - my-app-backend
    - my-app-frontend
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
    "container": "my-app-backend",
    "backend_error": "KeyError: 'users'",
    "time_delta_ms": 45
  }],
  "container_health": {
    "my-app-backend": {"status": "degraded", "error_count": 3}
  }
}
```

---

## Workspace Config (`.browsernerd/`)

Projects can ship their own BrowserNERD configuration by adding a `.browsernerd/` directory at the project root. The server auto-discovers this directory by walking up from the current working directory for up to 10 parent directories.

### Directory Structure

```
.browsernerd/
  config.yaml       # Project-specific config overrides (version-controlled)
  schemas/           # Project-specific Mangle schemas (version-controlled)
  data/              # Runtime data - sessions, logs (gitignored)
  .gitignore         # Ignores data/ directory
```

### Quick Setup

```bash
# Create a .browsernerd/ template in the current directory
./bin/browsernerd --init-workspace

# Or create one for a target repo from anywhere
./bin/browsernerd --init-workspace --workspace-dir /path/to/project
```

### Config Merge Order (highest priority wins)

```
CLI flags  >  explicit --config  >  .browsernerd/config.yaml  >  DefaultConfig()
```

- **DefaultConfig()** - Hardcoded Go defaults (unchanged)
- **`.browsernerd/config.yaml`** - Project-level overrides (Docker containers, schemas, etc.)
- **`--config path`** - Machine/user-level settings (Chrome path, headless, viewport)
- **CLI flags** (`--sse-port`) - Invocation-level overrides

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--no-workspace` | `false` | Disable `.browsernerd/` auto-discovery |
| `--workspace-dir` | `""` | Explicit workspace root (skip walk-up search) |
| `--init-workspace` | `false` | Create `.browsernerd/` template and exit |

### Multi-Project Patterns

| Goal | Recommended setup |
|------|-------------------|
| One repo with checked-in BrowserNERD defaults | Run `browsernerd --init-workspace` at that repo root, then launch from inside the repo or pin it with `--workspace-dir <repo>` |
| Many independent repos | Give each repo its own `.browsernerd/` directory and register one MCP entry per repo, each with a different `--workspace-dir` |
| Shared personal defaults only | Launch with `--no-workspace --config <global.yaml>` so no repo-local overrides are loaded |
| Monorepo or sub-app audit | Keep the workspace wherever it is most useful, but pass `repo_root` to the specific package or service tree you want `browser-audit` to trace |

### Example

```yaml
# .browsernerd/config.yaml - version-controlled with your project
docker:
  enabled: true
  containers:
    - my-app-backend
    - my-app-frontend
  log_window: "30s"

mangle:
  schema_path: ".browsernerd/schemas/project.mg"

browser:
  headless: false
  viewport_width: 1280
  viewport_height: 720
```

Relative paths in workspace config are resolved against the workspace root directory. Absolute paths are left unchanged.

For audit-heavy setups, the workspace is the best place to pin repo-trace, recorder, and Docker defaults. `browser-audit` still requires an explicit `repo_root` argument on entry, but the workspace config keeps schema paths, trace output, container lists, and recursive repo scan limits stable across sessions. In a single-repo setup, `repo_root` is usually the workspace root. In a monorepo, `repo_root` can be a narrower subtree while the workspace remains at the umbrella root.

Practical host setup rules:

- Put `docker.containers` in each repo's `.browsernerd/config.yaml`, not in one shared global config, so Docker correlation follows the right app
- Use `--workspace-dir <repo>` in Claude Desktop or Codex when the MCP host starts BrowserNERD from a shared tools directory or a different cwd
- Use `--no-workspace --config <global.yaml>` only for intentionally shared personal defaults
- Keep passing `repo_root` on every `browser-audit` call even when it matches the workspace root; the workspace provides defaults, but `repo_root` keeps each audit target explicit

### Audit-Focused Workspace Example

```yaml
# .browsernerd/config.yaml
browser:
  debugger_url: "ws://localhost:9222"
  session_store: ".browsernerd/data/sessions.json"
  repo_trace:
    enabled: true
    root_dir: "."
    search_roots:
      - "frontend"
      - "backend"
    max_files: 2500
    max_file_bytes: 1048576
    max_seed_hints: 24
    max_navigation_hints: 16
    max_control_hints: 24
    max_plan_steps: 16
    max_frontend_matches: 12
    max_backend_matches: 12

mangle:
  schema_path: ".browsernerd/schemas/project.mg"

recorder:
  enabled: true
  trace_dir: ".browsernerd/data/traces"
  max_rotated_files: 5

docker:
  enabled: true
  containers:
    - my-app-api
    - my-app-worker
  log_window: "60s"
```

---

## Configuration

```yaml
server:
  name: "browsernerd-mcp"
  version: "1.1.0"
  log_file: "data/browsernerd-mcp.log"

browser:
  auto_start: true           # Auto-launch Chrome
  headless: false            # Visible UI (true for CI)
  debugger_url: "ws://localhost:9222"
  enable_dom_ingestion: true # Capture DOM as facts
  enable_header_ingestion: true
  multi_tab_default: true
  max_tabs: 32
  max_browsers: 4
  repo_trace:
    enabled: true
    root_dir: "."
    search_roots: ["."]
    max_files: 4000
    max_file_bytes: 1048576
    max_seed_hints: 24
    max_navigation_hints: 16
    max_control_hints: 24
    max_plan_steps: 16
    max_frontend_matches: 12
    max_backend_matches: 12

mcp:
  progressive_only: true

mangle:
  enable: true
  schema_path: "schemas/browser.mg"
  fact_buffer_limit: 2048

docker:
  enabled: false             # Enable for full-stack correlation
  containers: []             # Container names to monitor
  log_window: 60s            # How far back to query logs

recorder:
  enabled: true
  trace_dir: "data/traces"
  max_rotated_files: 3
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
|   +-- cmd/server/             # MCP server entry point
|   +-- cmd/smoke/              # Go-native stdio verifier
|   +-- internal/
|   |   +-- browser/            # Rod session management, CDP events
|   |   +-- mcp/                # MCP server, 48 tool implementations
|   |   +-- mangle/             # Fact engine, rule evaluation
|   |   +-- config/             # YAML configuration
|   |   +-- docker/             # Container log integration
|   |   +-- correlation/        # Keyed cross-domain fact correlation
|   +-- schemas/modules/        # Modular Mangle predicates and rules
|   +-- testdata/fixtures/      # Declarative browser-test examples
+-- eval/                       # Evaluation framework
+-- LICENSE                     # Apache 2.0
+-- NOTICE                      # Attribution
```

**Built on:**
- [Rod](https://github.com/go-rod/rod) - High-performance Chrome DevTools Protocol
- [Mangle Go](https://codeberg.org/TauCeti/mangle-go) - maintained Mangle logic engine
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
