---
title: BrowserNERD
created: 2026-01-31
last_updated: 2026-10-22
doc_type: readme
subsystem: infra
read_when: Orienting in this directory
indexes: []
---

# BrowserNERD

**The token-efficient browser automation MCP server built for AI agents.**

Stop burning 50,000+ tokens on raw HTML dumps. BrowserNERD gives your AI agent structured, actionable browser state in **50-100x fewer tokens** than traditional approaches - plus built-in causal reasoning, React extraction, and full-stack error correlation.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27.2-00ADD8?logo=go)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-Compatible-green)](https://modelcontextprotocol.io)
[![Tools](https://img.shields.io/badge/MCP_Tools-48-blue)](https://modelcontextprotocol.io)
[![Version](https://img.shields.io/badge/version-1.4.0-orange)](#whats-new-in-140)

---

## What's new in 1.4.0

One BrowserNERD again: the work done in Cross-Thread's vendored copy since
1.2.0 joins the 1.3.0 Go 1.27/Linux line.

- **Responsive layout audit:** `browser-observe mode:"layout"` finds overlap,
  text bleed, horizontal overflow, offscreen, clipped, toolbar cut-off and tap
  target defects with refs, geometry and an annotated screenshot, after waiting
  for the page to finish loading.
- **Device emulation:** `browser-act {type:"viewport", preset:"iphone-14"}` (or
  explicit width/height/scale/mobile/touch/UA), per tab, surviving navigation.
- **Chrome lifetime:** server exit closes the Chrome it launched, and startup
  reaps Chrome left by a server that died, recognised by an owner mark in its
  profile. An attached `debugger_url` browser is never closed.
- **Streamable HTTP:** the network transport serves MCP at `/mcp`;
  `--sse-port`/`mcp.sse_port` and `--sse-host`/`mcp.sse_host` are now
  `--http-port`/`mcp.http_port` and `--http-host`/`mcp.http_host`.
- Friction fixes from live use: absent assertions fail, same-URL navigate
  reloads, toasts deduplicate, the network view is scoped to the current
  navigation, contradictions are scoped to the selected evidence.

## What's new in 1.3.0

A platform release: BrowserNERD now builds, tests and runs natively on Linux.

- **Go 1.27.2** toolchain; CI reads the version from `go.mod`.
- **Dependencies:** mcp-go 1.2.1 (MCP protocol 2026-07-28 alongside earlier
  revisions) and Mangle 0.6.0. Mangle 0.6 rejects a negation over unbound
  wildcards that 0.5 silently dropped, which had left every audit plan step
  reading as pending; the rule is fixed.
- **Linux:** an empty `browser.launch` runs the installed Chrome/Chromium/Edge
  instead of downloading Chromium; a killed server no longer orphans managed
  Chrome; the default and example configs load; `debugger_url:
  "ws://localhost:9222"` attaches; history back/forward no longer race Chrome's
  navigation.

## What's new in 1.2.0

A release shaped by live agent runs against a real React application: every change below
closes a failure an agent actually hit.

**Acting**
- `browser-act` finds elements by text, label, role or point, and `type`, `fill` and `select`
  verify what the page holds afterwards. A batch is validated before it runs, every op is
  bounded, and the response reports what the actions caused (navigations, dialogs, downloads,
  failed requests), naming what broke by path.
- Refs reach every control a page made clickable; a control React never attached a handler to,
  or one on a page that never hydrated, is flagged dead instead of silently clicked. A widget
  library's own DOM is not mistaken for a dead control.
- `upload` sets a file input through CDP, confined to workspace files. Every dialog is answered,
  and a download a click starts appears in the effects digest.

**Observing**
- `browser-observe` is small by default and drills down on request; action candidates collapse
  repeats and are ranked for the next decision.
- `mode: storage` says which token expired and where, without reading a value out.
- Pages are captured from their first byte, so failures that leave a page empty are recorded.

**Reasoning**
- `browser-reason` returns a diagnosis, not a dump: network rows show what an API answered as
  the shape of its body, a failed 4xx is one line judged by resource type, a body that fails
  after its 200 reads as the network failure it is, and `why_failed` asks the backend about a
  page that never loaded or a WebSocket that failed.
- Backend log lines are matched to the one failed request; a dead Docker is reported as
  unavailable, and log queries fail when every container fails instead of reporting an empty
  success. `get-console-errors` groups near-identical messages.

**Mangle engine**
- The built-in schema ships inside the binary; project modules in `.browsernerd/schemas/` add
  to it instead of replacing it. `browser-mangle` lists the schema and joins without a rule.
- The engine keeps failure evidence under load, evaluates only what a read needs, rejects
  queries that can never match, and no longer runs a quadratic rule that starved every insert.

**Testing and operations**
- `browser-test` fixtures take operations, refuse steps that test nothing, and stop before any
  step whose `value_env` is unset or empty - credentials go from the environment to the browser
  without passing through a transcript.
- A wedged call answers within its deadline. `-doctor` names what is wrong with BrowserNERD
  itself (stale binary, engine, workspace), `-version` prints the build, and `go run
  ./cmd/rebuild` rebuilds beside a running MCP host.
- Tool definitions an agent loads cost fewer tokens than before, despite documenting far more;
  `go run ./cmd/smoke -mode list` prices every definition.

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

### What an agent gets

- **Small by default** - `browser-observe` sees a page in a few hundred
  tokens: 400 identical buttons are one row with a count, a screenshot returns
  a file path, and `mode: text` reads the page instead of a picture of it.
- **Acts without a prior observe** - `browser-act` finds elements by `ref`,
  visible `text`, form `label`, `role` + `name`, or a point; checks the whole
  batch before running it; bounds every operation; verifies that typed and
  selected values stuck; and reports what the batch caused (URL change,
  failed requests with their body, console errors, toasts, dialogs,
  downloads).
- **A diagnosis, not a dump** - `browser-reason` answers "what broke" with a
  one-line verdict, the failed request, the console error or toast it caused,
  the backend log line behind it, and the one call worth making next.
- **Capture from the first byte** - a tab starts recording before it
  navigates, every JavaScript dialog is answered the moment it opens, and the
  failures that leave a page empty (load failures, WebSocket errors, refused
  requests) are facts.
- **Security-first capture** - credential redaction, private traces, confined
  exports, trusted workspace authority, safe page arguments, and bounded Mangle
  execution.
- **Multi-tab by default** - shared-context tabs, isolated forks, focus/close,
  browser inventory, and multiple managed Chrome instances.
- **Never hangs** - every call has a deadline and answers with a structured
  timeout; `browser-observe mode: doctor` (or `browsernerd -doctor`) says what
  is wrong with BrowserNERD itself.

---

## Key Features

### Token Efficiency
- **Structured JSON output** - Refs, labels, and actions - not HTML soup
- **Semantic grouping** - Navigation links grouped by page area (nav, sidebar, main, footer)
- **Action-ready refs** - Every element has a `ref` for direct interaction
- **Batch automation** - `browser-act` runs a validated batch of operations in one call

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
- **Native and Docker logs** - Correlate browser errors with configured log files or backend containers
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

- **Go 1.27.2** - Required to build the MCP server
- **Chrome/Chromium** - Browser to automate (auto-detected or configurable)

### Build & Run

```bash
# Clone the repository
git clone https://github.com/theRebelliousNerd/browserNerd.git
cd browserNerd/mcp-server

# Build, stamping the release version into the binary
go mod tidy
go build -ldflags "-X browsernerd-mcp-server/internal/buildinfo.Version=1.4.0" -o bin/browsernerd ./cmd/server

# Confirm which build you are running (version, revision, build time)
./bin/browsernerd -version

# Run with a checked-in project workspace
./bin/browsernerd --workspace-dir /path/to/project

# Run with machine-level config only
./bin/browsernerd --config config.yaml
```

### Add to Claude Code

```bash
claude mcp add --scope user browsernerd -- \
  /path/to/browserNerd/mcp-server/bin/browsernerd \
  --no-workspace --config ~/.browsernerd/global.yaml
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
    "browsernerd-myapp": {
      "command": "/path/to/browserNerd/mcp-server/bin/browsernerd",
      "args": [
        "--workspace-dir",
        "/path/to/myapp"
      ]
    }
  }
}
```

Pinned-project and shared-global Codex examples:

```toml
[mcp_servers.browsernerd_myapp]
command = "/path/to/browserNerd/mcp-server/bin/browsernerd"
args = ["--workspace-dir", "/path/to/myapp"]

[mcp_servers.browsernerd_shared]
command = "/path/to/browserNerd/mcp-server/bin/browsernerd"
args = ["--no-workspace", "--config", "/home/you/.browsernerd/global.yaml"]
```

Create one MCP entry per project when you want different repo-trace defaults, schema paths, or Docker container lists.

---

## Browser Connection Modes

### Mode 1: Auto-Launch (Zero Config)

BrowserNERD automatically launches and manages Chrome via Rod. It runs
`browser.launch[0]` when set, else the installed Chrome, Chromium or Edge, and
downloads Rod's Chromium only when none is installed. Server exit closes the
Chrome it launched, and the next start reaps any it left behind after a crash.

```
launch-browser                         ->  Chrome starts with CDP enabled
browser-act {type: session_create, url} -> New tab, recording before it loads
browser-observe / browser-act           ->  Automate away
shutdown-browser                       ->  Clean exit
```

### Mode 2: Attach to Existing Browser

Connect to a Chrome instance you're already using:

```bash
# Start Chrome with remote debugging (Chrome requires a non-default profile)
google-chrome --remote-debugging-port=9222 --user-data-dir="$HOME/.browsernerd/chrome-debug"
# Windows: mcp-server/start-chrome-debug.bat
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

### Default surface (11 tools)

With `mcp.progressive_only: true` (the default) these are the only tools an
agent sees; each drills down through parameters instead of more tools.

| Tool | Description |
|------|-------------|
| `launch-browser` | Start Chrome with CDP enabled (idempotent) |
| `shutdown-browser` | Close Chrome and clean up all sessions |
| `close-session` | Idempotently close one tab and its event streams |
| `browser-observe` | See a page by `mode`: composite (default), text, interactive, screenshot, layout (spatial audit), storage, sessions, doctor, and narrow slices |
| `browser-act` | Run a validated, time-bounded batch of `operations`; report what it caused |
| `browser-reason` | Diagnose by `topic`: why_failed, health, network, blocking_issue, next_best_action, what_changed_since |
| `browser-audit` | Phased frontend-to-API contract audit with evidence handles |
| `browser-mangle` | Schema listing, queries (atoms, conjunctions, rules), temporal reads, waits, flight export |
| `browser-test` | Create, inspect, or run a declarative fixture of `browser-act` operations plus Mangle assertions |
| `get-specs` | Deliver spec invariants governing a component, route, selector, or file line-range |
| `check-specs` | Evaluate spec invariants against current state, report violations |

### Focused tools (`mcp.progressive_only: false` adds 37)

The progressive tools delegate to these, so the two surfaces share one
implementation.

#### Sessions (8 tools)

| Tool | Description |
|------|-------------|
| `list-sessions` | List all active browser sessions |
| `create-session` | Open new tab with optional starting URL |
| `attach-session` | Attach to existing CDP target by ID |
| `list-browsers` | List Chrome instances and tab counts |
| `focus-session` | Activate one tracked tab |
| `fork-session` | Clone session preserving auth state (cookies, localStorage) |
| `reify-react` | Extract React Fiber tree as Mangle facts |
| `snapshot-dom` | Capture DOM structure as Mangle facts |

#### Navigation & State (4 tools)

| Tool | Description |
|------|-------------|
| `get-page-state` | URL, title, loading state, scroll position, active element |
| `get-navigation-links` | All links grouped by page area (nav/side/main/footer) |
| `get-interactive-elements` | Controls with action refs; `group: true` collapses repeated controls into one row |
| `navigate-url` | Navigate with wait options (load/networkidle/none) |

#### Browser Interaction (8 tools)

| Tool | Description |
|------|-------------|
| `interact` | Click, type, select, toggle, clear, hover by ref, text, label, role+name, or point |
| `fill-form` | Fill several fields in one call, each value read back |
| `press-key` | Send a key or chord (`Enter`, `Control+a`, `Shift+Tab`) |
| `browser-history` | Navigate back, forward, or reload |
| `discover-hidden-content` | Find elements hidden by CSS/JS |
| `discover-grids` | Find virtualized grids and bounded row samples |
| `screenshot` | Capture page/element to file; returns path and size only |
| `evaluate-js` | Trusted-config, gated JavaScript escape hatch (disabled by default) |

#### Mangle-Driven Automation (4 tools)

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

#### Diagnostics (2 tools)

| Tool | Description |
|------|-------------|
| `get-console-errors` | Console errors with root cause analysis + Docker correlation |
| `get-toast-notifications` | Detect toast/snackbar overlays with API correlation |

#### Testing (2 tools)

| Tool | Description |
|------|-------------|
| `run-test` | Run a declarative test: replay operations, evaluate Mangle assertions, return a causal chain on failure |
| `generate-test` | Turn a recorded interaction (action facts) into a draft fixture |

---

## Token Efficiency in Action

### Traditional Approach
```
User: "Save the form and tell me whether it worked"

1. Get page HTML: 45,000 tokens
2. AI parses HTML to find the button
3. Execute click with selector
4. Get updated HTML and the network log: 45,000+ tokens

Total: ~90,000 tokens
```

### BrowserNERD Approach
```
User: "Save the form and tell me whether it worked"

1. browser-act {operations: [{type: "click", text: "Save"}]}
   -> the click, plus an effects digest of what it caused:
      failed_requests: ["POST /api/items 500: {\"detail\": \"name is required\"}"]
      errors: ["Uncaught (in promise) Error: Request failed"]
      toasts: ["error: Could not save"]

Total: one call, a few hundred tokens, no follow-up observe
```

No observe was needed to find the button: `text`, `label` and `role` + `name`
locate an element by what the user sees. When a ref is wanted,
`browser-observe` returns the whole page's controls grouped, in a few hundred
tokens. The live probe tests pin each default response's budget (composite
observe <= 900 tokens, a screenshot result <= 150, `browser-reason` why_failed
<= 600).

### One batch, one round trip

```
browser-act({
  session_id: "<session>",
  operations: [
    {type: "navigate", url: "http://localhost:3000/login"},
    {type: "fill", fields: [
      {label: "Email", value: "user@test.com"},
      {label: "Password", value: "secret"}
    ], submit_button: "Sign in"}
  ]
})
```

The batch is checked before anything runs - a misspelled field rejects it with
the fix named - every operation is time-bounded, each typed value is read back,
and the result's `effects` say where the page went and what failed on the way.

---

## Progressive Disclosure: Right Detail at the Right Time

Every progressive tool takes `view: summary | compact | full` (default
`compact`) and returns `evidence_handles` to expand instead of repeating a
call at `full`.

### browser-observe

```
browser-observe(session_id)
  -> composite: title, headings and status text, health digest, controls
     grouped (400 identical buttons = 1 row with count + refs), ranked
     ready-to-send browser-act calls

browser-observe(session_id, mode: "text", text_query: "Revenue")
  -> what the page says: outline, landmarks, live regions, matching blocks

browser-observe(session_id, mode: "screenshot", annotate: true)
  -> {file_path, format, size_bytes, width, height} plus numbered controls in view

browser-observe(session_id, mode: "layout", viewports: ["phone", "tablet"], url: "http://localhost:3001/services")
  -> per size: overlaps, text bleeding out of or cut by its box, sideways
     scroll, content unreachable in drawers or under a phone toolbar, columns
     that never stack, small tap targets, covered content - each with
     selectors, refs, boxes and a pixel (CV) verdict - and an annotated
     screenshot; the session's size is restored afterwards

browser-observe(mode: "doctor")
  -> BrowserNERD's own health: stale binary, env, engine, wedged calls
```

`browser-act` `{type: "viewport", preset: "phone"}` sizes one tab as a phone
(390x844 @3, mobile, touch); `reset: true` puts it back. See
[docs/specs/responsive-layout-audit.md](docs/specs/responsive-layout-audit.md).

**Modes:** `composite` | `text` | `interactive` | `screenshot` | `layout` | `storage` |
`sessions` | `doctor` | `state` | `nav` | `hidden` | `grids` | `react` |
`dom_snapshot`
**Intents:** `quick_status` | `find_actions` | `map_navigation` |
`hidden_content` | `deep_audit` | `check_sessions` | `visual_check` |
`grid_hunt`

### browser-act

```
browser-act(session_id, operations: [
  {type: "select", label: "Region", value: "EMEA"},
  {type: "key", key: "Control+s"},
  {type: "click", text: "Delete", dialog: "accept"}
])
  -> per-op results (select verified the widget shows EMEA), the confirm()
     the delete opened answered, and an effects digest
```

Operations: `navigate`, `interact`, `fill`, `upload`, `key`, `history`,
`sleep` (<= 60 s), `dialog`, `wait`, `await_stable`, `await_fact`,
`await_conditions`, `session_create`, `session_attach`, `session_focus`,
`session_close`, `session_fork`, `browser_launch`, `browser_close`, `js`
(gated), `plan`. A `session_create` in a batch hands its new `session_id` to
the operations after it.

### browser-reason

```
browser-reason(session_id, topic: "why_failed", since_action: true)
  -> verdict: "POST /api/items -> 500 ...; console error 40 ms later",
     root_cause: the request and its body, the console error and toast it
     caused, the backend log lines; next: the one call worth making

browser-reason(session_id, topic: "network", expand: "/api/items", body: true)
  -> the calls to that URL and the shape of what they returned
     (items: array[0] means it answered empty)
```

**Topics:** `why_failed` | `health` (default) | `network` | `blocking_issue` |
`next_best_action` | `what_changed_since`
**Intents:** `triage` | `act_now` | `debug_failure` | `unblock`

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

BrowserNERD uses [TauCeti Mangle Go v0.6.0](https://codeberg.org/TauCeti/mangle-go) for declarative reasoning.

The built-in schema is compiled into the binary; a project adds its own
modules on top through `.browsernerd/schemas/*.mg` or `mangle.schema_path`.
`browser-mangle` `operation: schema` lists every predicate with its argument
names, module and fact count (`filter: "net_"` narrows it), and a query that
names an unknown predicate or the wrong argument count is refused with the
declared signature instead of returning zero rows.

### Built-in Predicates (60+)

Every browser fact leads with `SessionId`, so tabs never contaminate each
other.

**React Fiber:**
```mangle
react_component(SessionId, FiberId, ComponentName, ParentFiberId).
react_prop(SessionId, FiberId, PropKey, PropValue).
react_state(SessionId, FiberId, HookIndex, Value).
```

**DOM & Navigation:**
```mangle
dom_node(SessionId, NodeId, Tag, Text, ParentId).
dom_attr(SessionId, NodeId, Key, Value).
navigation_event(SessionId, Url, Timestamp).
current_url(SessionId, Url).
page_load_failed(SessionId, Url, ErrorText, Timestamp).
```

**Network (HAR-like):**
```mangle
net_request(SessionId, Id, Method, Url, InitiatorId, StartTime).
net_response(SessionId, Id, Status, Latency, Duration).
net_header(SessionId, Id, Kind, Key, Value).
net_http_error(SessionId, Id, Url, Status, ResourceType, Timestamp).
net_loading_failed(SessionId, Id, ErrorText, Canceled, Timestamp).
net_failure_body(SessionId, Id, Snippet).
ws_event(SessionId, WsId, Url, Event, Detail, Timestamp).
```

**Interactive Elements:**
```mangle
interactive(SessionId, Ref, Type, Label, Action).
nav_link(SessionId, Ref, Href, Area, Internal).
user_click(SessionId, Ref, Timestamp).
user_type(SessionId, Ref, Value, Timestamp).
dead_control(SessionId, Ref, Label).
```

**Diagnostics:**
```mangle
console_event(SessionId, Level, Message, Timestamp).
browser_log(SessionId, Source, Level, Text, Url, Timestamp).
toast_notification(SessionId, Text, Level, Source, Timestamp).
js_dialog(SessionId, Type, Message, Accepted, HandledBy, Timestamp).
download(SessionId, Guid, Url, SuggestedName, State, Timestamp).
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

- `failed_request(SessionId, RequestId, Url, Status)` - every 5xx, and every
  4xx except a page asset's (image, font, stylesheet, media, manifest).
- `network_failure(SessionId, RequestId, Url, ErrorText, Timestamp)` - a
  request that got no response and was not canceled.
- `caused_by(SessionId, ConsoleMessage, RequestId)` - a console error linked to
  the nearest failure that completed within the 2 s before it;
  `error_chain` carries it on to the URL and status.
- `toast_after_api_failure(SessionId, ToastText, RequestId, Url, Status,
  TimeDelta)` - an error toast linked to the nearest failed request within 5 s.
- `full_stack_error(SessionId, ConsoleMsg, RequestId, Url, BackendMsg)` - the
  console error, the failed call, and the backend log line behind it.

**Slow API Detection (>1 second from request to response headers):**
```mangle
slow_api(SessionId, ReqId, Url, Duration) :-
    net_response(SessionId, ReqId, _, _, Duration),
    Duration > 1000,
    net_request(SessionId, ReqId, _, Url, _, _).
```

**Universal Login Detection:**
```mangle
login_succeeded(SessionId) :-
    form_submitted(SessionId, _, TSubmit),
    successful_post(SessionId, _, _, TPost),
    TPost >= TSubmit,
    url_changed_after_submit(SessionId, _, _, TNav),
    TimeDelta = fn:minus(TNav, TSubmit),
    TimeDelta < 5000.
```

### Queries and Custom Rules

A query can join without a rule:
```
browser-mangle(operation: "query",
  query: "net_request(S, R, M, U, I, T), net_response(S, R, 500, L, D).")
```

Submit rules at runtime and wait on them:
```
browser-mangle(operation: "submit_rule",
  rule: "ready(S) :- current_url(S, \"http://localhost:3000/dashboard\"), dom_text(S, _, \"Welcome\").")
browser-act(operations: [{type: "wait", predicate: "ready", timeout_ms: 10000}])
```

---

## Server Log Integration

Correlate browser failures with native application logs, Docker logs, or both.
Configure sources for any application in its workspace or a machine profile:

```yaml
logs:
  files:
    - name: api
      path: logs/api.log
    - name: web
      path: logs/web.log
  log_window: 30s
  max_read_bytes: 1048576
  max_entries: 2000
```

JSON timestamp/time fields and timestamped text logs are supported. Multiline
exceptions stay with their timestamped entry; undated records are reported as
uncorrelatable. Relative workspace paths resolve against that workspace;
relative paths in an explicit profile resolve against the profile directory.
Untrusted workspace sources remain inside the workspace, symlinks included.
Reads are bounded and regular-file-only, and all returned evidence is redacted.

For containerized applications, configure Docker independently:

```yaml
docker:
  enabled: true
  containers:
    - my-app-backend
    - my-app-frontend
  log_window: 60s
```

**What it does:**
- Queries configured native and container logs when errors occur
- Correlates browser API failures with backend exceptions
- Provides full chain: Browser console -> Failed API -> Backend error
- Asks the logs about a page that never loaded or a WebSocket that failed,
  where every browser signal reads clean and the reason is only server-side
- Reports unavailable or partially available sources and truncated evidence explicitly

The same sources feed diagnoses, error diagnostics and optional server-log flight
exports. Doctor lists configured source names and types. No application-specific
routes, paths, frameworks or service names are built into the readers. See the
[native log correlation contract](docs/specs/native-log-correlation.md).

To verify the installed server against a generic local app through a real
browser, run from `mcp-server`:

```bash
go run ./cmd/smoke -mode native-logs -server bin/browsernerd -timeout 90s
```

This checks a failed request, its native exception, Doctor's source listing and
flight-export evidence with Docker disabled.

**Example output from `browser-reason` `topic: why_failed` (abridged):**
```json
{
  "topic": "why_failed",
  "verdict": "GET /api/users -> 500 \"KeyError: 'users'\"; console error 45 ms later",
  "root_cause": {
    "request": {"method": "GET", "url": "/api/users", "status": 500, "body": "{\"detail\": \"KeyError: 'users'\"}"},
    "effects": [{"kind": "console_error", "message": "TypeError: Cannot read properties of undefined (reading 'map')", "after_ms": 45}],
    "backend_logs": ["my-app-backend 14:02:11.482 KeyError: 'users'"]
  },
  "next": {"tool": "browser-reason", "args": {"topic": "network", "expand": "/api/users"}}
}
```

---

## Workspace Config (`.browsernerd/`)

Projects can ship their own BrowserNERD configuration by adding a `.browsernerd/` directory at the project root. The server auto-discovers this directory by walking up from the current working directory for up to 10 parent directories.

### Directory Structure

```
.browsernerd/
  config.yaml       # Project-specific config overrides (version-controlled)
  schemas/           # Project Mangle modules, loaded on top of the built-in schema (version-controlled)
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
- **`--config path`** - Machine/user-level settings (Chrome path, headless, viewport). Keep project settings out of it: a `docker` block here overrides the workspace's, and `-doctor` names any such override as a problem
- **CLI flags** (`--http-port`, `--http-host`) - Invocation-level overrides

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--no-workspace` | `false` | Disable `.browsernerd/` auto-discovery |
| `--workspace-dir` | `""` | Explicit workspace root (skip walk-up search) |
| `--init-workspace` | `false` | Create `.browsernerd/` template and exit |
| `--http-port` | `0` | Serve MCP over Streamable HTTP at `/mcp` on this port instead of stdio (config `mcp.http_port`) |
| `--http-host` | `127.0.0.1` | Interface the HTTP server listens on (config `mcp.http_host`) |

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

# .mg files in .browsernerd/schemas/ load on top of the built-in schema
# without any setting; mangle.schema_path points somewhere else.

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
  version: "1.4.0"
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
  dialog_policy: dismiss     # answer for a JS dialog nobody armed: dismiss | accept
  download_dir: ""           # default <data root>/downloads
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
  progressive_only: true     # the 11 default tools; false adds the 37 focused ones
  tool_timeout: 2m           # per-call deadline, plus the waits a call declares

mangle:
  enable: true
  schema_path: ""            # PROJECT modules on top of the built-in schema
  disable_builtin_rules: false
  fact_buffer_limit: 16384

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
| Batch automation | Validated `browser-act` batches | Individual calls | Individual calls | Limited |
| Fork with auth | Yes | No | No | No |

---

## Architecture

```
browserNerd/
+-- mcp-server/                 # Go MCP server
|   +-- cmd/server/             # MCP server entry point (-version, -doctor)
|   +-- cmd/rebuild/            # Build beside a running server
|   +-- cmd/smoke/              # Go-native stdio verifier and probe tour
|   +-- internal/
|   |   +-- browser/            # Rod session management, CDP events
|   |   +-- mcp/                # MCP server, 48 tool implementations
|   |   +-- mangle/             # Fact engine, rule evaluation
|   |   +-- config/             # YAML configuration
|   |   +-- logs/               # Shared evidence, native file readers, request correlation
|   |   +-- docker/             # Container log integration
|   |   +-- correlation/        # Keyed cross-domain fact correlation
|   +-- schemas/                # Built-in Mangle schema, embedded in the binary
|   +-- testdata/fixtures/      # Declarative browser-test examples
|   +-- testdata/probe/         # The page live tests drive to pin behaviour and token budgets
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

# Rebuild while an MCP host holds the binary, then reconnect the host
go run ./cmd/rebuild

# Which binary is running, is it stale, and what is wrong
./bin/browsernerd -doctor --workspace-dir /path/to/project

# Verbose logging
./bin/browsernerd --config config.yaml --verbose

# Streamable HTTP mode: MCP at http://127.0.0.1:8080/mcp
./bin/browsernerd --config config.yaml --http-port 8080
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
