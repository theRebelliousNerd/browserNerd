---
title: BrowserNERD MCP Server
created: 2025-11-23
last_updated: 2026-09-24
doc_type: readme
subsystem: infra
read_when: Orienting in this directory
indexes: []
---

# BrowserNERD MCP Server

BrowserNERD is a production-grade browser intelligence copilot: Rod automation plus Mangle reasoning, wrapped in an MCP interface that keeps agents fast, grounded, and debuggable.

If you want fewer blind clicks, fewer hallucinated root causes, and faster time-to-fix, this is the stack.

## Why BrowserNERD

Most browser automation tools can click. BrowserNERD can explain.

- It observes: network, console, DOM, toasts, navigation, React, and Docker logs.
- It reasons: causal chains and temporal patterns via Mangle.
- It acts: guided next-step plans with evidence handles.
- It records: redacted, private JSONL flight logs for postmortem replay and RCA.

## Core Capabilities

- Rod-backed browser control and session lifecycle management; a tab records
  from its first request, and every JavaScript dialog is answered the moment
  it opens.
- Mangle fact engine with derived predicates and causal diagnostics, a
  built-in schema compiled into the binary, and retention that keeps failure
  evidence under load.
- Temporal reasoning over request/navigation/error timelines.
- Token-optimized observation and reasoning (`summary`, `compact`, `full`),
  with each default response's token budget pinned by live tests.
- Docker correlation for browser-to-backend issue tracing.
- React and DOM extraction for UI-state-aware debugging, including controls
  React never attached to.
- Workspace-discovered repo tracing and phased contract audits with persisted evidence handles.
- A deadline on every call, and a doctor that says what is wrong with the
  server itself.

Specs of record: `../docs/specs/` (start with
`progressive-mcp-tool-contract.md`).

## Progressive Disclosure (Default)

With `mcp.progressive_only: true`, BrowserNERD exposes 11 high-signal tools by default:

- `launch-browser`
- `shutdown-browser`
- `close-session`
- `browser-observe`
- `browser-act`
- `browser-reason`
- `browser-audit`
- `get-specs`
- `check-specs`
- `browser-test`
- `browser-mangle`

This keeps agent context focused while preserving deep capability through parameters.

### browser-observe

Use for state and affordance discovery, small by default:

- Modes: `composite` (default: title, headings and status text, health digest,
  controls grouped - 400 identical buttons are one row with a count and refs -
  and ranked ready-to-send `browser-act` calls), `text` (read the page;
  `text_query`, `max_chars`), `interactive`, `screenshot` (returns the file
  path and size; `annotate`, `ref`, `full_page`), `storage` (cookie and
  web-storage names, flags and token expiry, never values), `sessions`,
  `doctor`, and the slices `state`, `nav`, `hidden`, `grids`, `react`,
  `dom_snapshot`
- Views: `summary`, `compact`, `full`
- Intents: `quick_status`, `find_actions`, `map_navigation`, `hidden_content`, `deep_audit`, `check_sessions`, `visual_check`, `grid_hunt`

### browser-act

Use for deterministic execution:

- Operations: `navigate`, `interact` (`click`, `type`, `select`, `toggle`,
  `clear`, `hover`), `fill`, `upload`, `key` (chords such as `Control+a`),
  `history`, `sleep`, `dialog`, `wait`, `await_stable`, `await_fact`,
  `await_conditions`, session and browser lifecycle, gated `js`, and `plan`
- Locators: `ref`, `text`, `label`, `role` + `name`, or `x`/`y`; `index`
  picks among several matches
- The batch is validated before anything runs; every operation is bounded;
  typed and selected values are read back; any operation can answer the
  dialog it opens (`dialog: accept | dismiss`)
- The result's `effects` digest reports what the batch caused: URL change,
  failed requests with their body, console errors, toasts, dialogs,
  downloads, WebSocket and load failures

### browser-reason

Use for triage and root cause analysis:

- Topics: `health` (default), `why_failed`, `network`, `blocking_issue`, `next_best_action`, `what_changed_since`
- A diagnosis leads with a one-line `verdict`, the `root_cause` (request,
  body, the effects it caused, backend log lines) and the one `next` call
- `network` groups requests by method and URL, flags storms, and with
  `expand` plus `body: true` returns the shape of what the latest calls
  answered (`items: array[0]` is an empty answer)
- Intents: `triage`, `act_now`, `debug_failure`, `unblock`
- Scope: `since_action=true` (since your last click or input), `since_navigation=true` (the current route), `time_window_ms`

### browser-audit

Use for frontend-to-API contract tracing:

- `phase=discover` builds a deterministic plan skeleton and hazard list without executing risky actions
- `phase=execute` replays the persisted plan, runs safe steps by default, and skips gated actions unless explicitly allowed
- `phase=report` correlates current page context, failed requests, backend correlations, and repo evidence
- `phase=resume` narrows follow-up sections from an existing audit run instead of dumping the full report again
- `repo_root` is required on entry even when `.browsernerd/config.yaml` already defines repo-trace defaults
- `audit_id` lets you persist and later re-open a specific run; report is the default phase when omitted
- `resume_handle` or `expand_handles` focuses follow-up on selected evidence sections
- `allow_risky`, `allow_navigation`, `allow_mutating`, and `allow_destructive` gate execute-mode replays
- Highlights auth failures like missing JWT / API-key wiring
- Surfaces route drift and likely frontend source files under `repo_root`
- Returns progressive evidence handles for deeper follow-up with `browser-mangle`

### browser-mangle

Use for raw fact access and forensic workflows:

- Operations: `schema`, `query`, `read`, `temporal`, `evaluate`, `push`, `submit_rule`, `subscribe`, `await_fact`, `await_conditions`, `export_flight`
- `query` takes one atom, a comma conjunction (joins without a rule), or
  `head :- body`; a wrong argument count or unknown predicate is refused with
  the declared signature

## Debugging Workflow (Recommended)

1. Launch and create a session (`browser-act` `session_create`; its result
   carries the `session_id`).
2. `browser-observe` for the page; act with `browser-act` and read its
   `effects`.
3. `browser-reason` with `topic=why_failed` and `since_action=true` or
   `since_navigation=true`.
4. For a page that renders empty, `topic=network` with `expand` and
   `body=true`.
5. Drill into evidence handles, or `browser-mangle` for raw facts; export raw
   evidence with `export_flight`.
6. When BrowserNERD itself misbehaves, `browser-observe` `mode=doctor`.

Example sequence:

```json
{"tool":"launch-browser","arguments":{}}
{"tool":"browser-act","arguments":{"operations":[{"type":"session_create","url":"https://cross-thread.ai"},{"type":"await_stable","timeout_ms":12000}]}}
{"tool":"browser-reason","arguments":{"session_id":"<session>","topic":"why_failed","view":"compact","since_navigation":true}}
{"tool":"browser-reason","arguments":{"session_id":"<session>","topic":"network","expand":"/api/","body":true}}
{"tool":"browser-mangle","arguments":{"operation":"export_flight","session_id":"<session>","include_server_logs":true}}
```

## Audit Workflow (Recommended)

Use the phased audit system when you need a reproducible browser-to-code investigation loop instead of a one-shot report.

1. Discover a passive plan.
2. Execute only the steps you explicitly allow.
3. Report the full contract synthesis.
4. Resume by handle when you only need one slice of evidence.

Example sequence:

```json
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"discover","view":"compact","include_repo_matches":true}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"execute","audit_id":"audit-123","view":"compact"}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"execute","audit_id":"audit-123","allow_mutating":true,"allow_navigation":true,"view":"compact"}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"report","audit_id":"audit-123","view":"compact","include_repo_matches":true}}
{"tool":"browser-audit","arguments":{"session_id":"<session>","repo_root":"/repo","phase":"resume","audit_id":"audit-123","resume_handle":"audit:<session>:mangle_contracts","view":"compact"}}
```

What to expect:

- `discover` returns `audit_plan`, `audit_hazards`, `report_handles`, and `approval_required` without mutating page state.
- `execute` persists `completed_steps` and `skipped_steps`; when work remains it returns `status=resume_ready`.
- `report` returns ranked findings plus evidence handles such as `audit:<session>:contract_findings`, `audit:<session>:mangle_contracts`, `audit:<session>:repo_matches`, and `audit:<session>:repo_trace`.
- `resume` narrows the response to the requested handles instead of repeating every section.

## Flight Recorder

BrowserNERD supports raw JSONL flight export for exact replay and offline debugging.

- Use `browser-mangle` with `operation=export_flight`.
- Exports include fact rows and optional Docker log rows.
- Capture includes timestamps, predicates, args, and session scoping for reliable incident reconstruction.

## Temporal Reasoning

BrowserNERD stores timestamped facts and supports time-aware triage.

- `time_window_ms` in `browser-reason` narrows evidence by recency.
- `since_navigation=true` scopes findings to the active route transition.
- `since_action=true` scopes findings to what followed your last click, input, key or submit.
- `browser-mangle temporal` supports predicate queries across explicit time windows.

This dramatically reduces stale-noise investigations in long-lived sessions.

## Workspace Config

BrowserNERD can auto-discover a project workspace by walking up from the current working directory until it finds `.browsernerd/config.yaml` (up to 10 parent directories).

Merge order:

```text
CLI flags > explicit --config > .browsernerd/config.yaml > DefaultConfig()
```

Useful flags:

- `--init-workspace` creates `.browsernerd/`, `schemas/`, `data/`, and a starter config.
- `--workspace-dir` pins the workspace root instead of relying on walk-up discovery.
- `--no-workspace` disables workspace discovery entirely.

Common multi-project layouts:

- Repo-owned workspace: run `browsernerd --init-workspace --workspace-dir ~/projects/myapp` once, then register an MCP entry that always passes `--workspace-dir ~/projects/myapp`.
- Shared/global profile: run with `--no-workspace --config ~/.browsernerd/global.yaml` when you want one personal BrowserNERD config and no repo-local overrides.
- Monorepo or partial trace: keep the workspace at the umbrella repo if that is where shared schema and Docker config belong, but set `repo_root` to the specific package or service tree you want `browser-audit` to scan.

Example client configs:

Claude project config (`.mcp.json`):

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

Codex config (`~/.codex/config.toml`):

```toml
[mcp_servers.browsernerd_myapp]
command = "/path/to/browserNerd/mcp-server/bin/browsernerd"
args = ["--workspace-dir", "/path/to/myapp"]

[mcp_servers.browsernerd_shared]
command = "/path/to/browserNerd/mcp-server/bin/browsernerd"
args = ["--no-workspace", "--config", "/home/you/.browsernerd/global.yaml"]
```

Audit-focused workspace example:

```yaml
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
    - myapp-api
    - myapp-worker
  log_window: "60s"
```

Relative paths are resolved against the workspace root. The workspace does not replace the `repo_root` argument on `browser-audit`; it keeps repo tracing, schema loading, trace output, and Docker correlation aligned with the project. In a single-repo setup `repo_root` is usually the workspace root. In a monorepo it can be a narrower subtree.

## Contract Audit And Mangle

The phased audit system is not just a UI workflow. It persists and emits Mangle facts that downstream tools can query directly.

- `audit_plan_state` records the current phase and status for an audit id.
- `audit_plan_step` and `audit_discovered_action` capture the passive discover plan.
- `scoped_audit_run`, `scoped_audit_run_completed_action`, `scoped_audit_run_skipped_action`, and `scoped_audit_run_resume_action` model execute and resume state.
- `repo_trace_*` facts carry recursive repo/code evidence found from browser context.
- `browser-mangle` can query those facts or export raw flight evidence for postmortems.

## Build And Run

```bash
# Build bin/browsernerd (bin/browsernerd.exe on Windows), stamping the
# version; works while an MCP host holds the binary
go run ./cmd/rebuild

# Stdio mode (Claude Code / MCP host)
./bin/browsernerd --config config.yaml

# SSE mode
./bin/browsernerd --config config.yaml --sse-port 8080

# Identity and self-diagnosis
./bin/browsernerd -version
./bin/browsernerd -doctor --workspace-dir <repo>
```

Build, staleness, the doctor and per-call deadlines are specified in
`../docs/specs/delivery-and-verification.md`.

## Native Testing And Smoke Verification

Browser automation tests are native MCP fixtures -
`{name, session_id, operations[], assertions[]}` - of `browser-act`
operations plus bounded Mangle assertions. Create, inspect, and run them with
`browser-test`. A fixture is validated before anything runs: unknown keys,
invalid operations, and a fixture that tests nothing are refused. Credentials
use `value_env`, resolved only during execution; an unset or empty variable
stops the run before its first step, naming the variable.

```json
{"tool":"browser-test","arguments":{"operation":"create","session_id":"<session>","name":"login flow"}}
{"tool":"browser-test","arguments":{"operation":"inspect","test_yaml":"<fixture yaml>"}}
{"tool":"browser-test","arguments":{"operation":"run","session_id":"<session>","test_yaml":"<fixture yaml>","view":"summary"}}
```

The example fixture is `testdata/fixtures/login.yaml`. Repository verification
uses Go, including the official MCP Go client:

```bash
go test ./...
go run ./cmd/rebuild
go run ./cmd/smoke -mode list
go run ./cmd/smoke -mode smoke -url https://example.com/
go run ./cmd/smoke -mode probe -workspace-dir <repo>   # tour testdata/probe, price every call
```

## Configuration

Use `config.yaml` (see `config.example.yaml`).

Highlights:

- `mcp.progressive_only`: keep default focused tool surface.
- `mcp.tool_timeout`: the per-call deadline (default 2m, plus the waits a call declares).
- `browser.dialog_policy`: the answer to a JavaScript dialog nobody armed (`dismiss` default, or `accept`).
- `browser.download_dir`: where downloads land in a browser this server launched.
- `browser.repo_trace.*`: bound recursive repo scans, plan generation, and frontend/backend match counts.
- `recorder.enabled` + `recorder.trace_dir`: keep flight evidence available.
- `mangle.schema_path`: project Mangle modules loaded on top of the built-in schema (empty by default; `.browsernerd/schemas/*.mg` load without it).
- `mangle.fact_buffer_limit`: buffer size (default 16384); over it, low-signal facts go first and failure evidence last.
- `docker.enabled`: correlate browser failures with backend logs.

## Tooling Positioning

BrowserNERD is built for teams that need evidence-rich browser debugging, not just scripted automation.

- Faster diagnosis through ranked `top_errors` and `investigation_items`.
- Better token economics with progressive disclosure and compact views.
- Stronger trust through raw flight export and Mangle-grounded reasoning.

## License And Attribution

BrowserNERD is licensed under Apache License 2.0 with an explicit `NOTICE` file.

- You may use, modify, and distribute.
- You must preserve license text and required attribution notices.
- If you redistribute derivatives, retain the BrowserNERD attribution in NOTICE and documentation.

See `LICENSE` and `NOTICE`.

## Repository

- Source: `https://github.com/theRebelliousNerd/browserNerd`
