---
title: BrowserNERD MCP Server
created: 2025-11-23
last_updated: 2026-07-23
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

## What Is New In v1.1.0

This release hardens and expands BrowserNERD:

- Security-first capture, persistence, workspace authority, exports, page
  arguments, and bounded Mangle execution.
- Shared-context multi-tab operation by default, isolated forks, per-tab close,
  browser inventory, and multiple managed Chrome instances.
- Configurable named Markdown spec corpora and indexes, with compact context
  attached to observe and act results.
- Correct query bodies, fresh-fact watches, correlated network facts, working
  JPEG output, modular production files, and Windows/Linux CI.
- Compiler/build error prioritization in `browser-reason` top errors.
- Repeated console error dedupe for cleaner, token-efficient summaries.
- Route-scoped reasoning mode with `since_navigation=true`.
- Temporal and change-window diagnostics with `time_window_ms` + Mangle timestamps.
- Flight recorder export hardening for raw evidence capture.
- First-pass `browser-audit` frontend-to-API contract tracing for missing JWT/API-key wiring, contract drift, and route mismatch discovery.
- Investigation steps tuned to error class (`compiler_error` -> `console_event` queries, toast classes -> toast queries).

## Core Capabilities

- Rod-backed browser control and session lifecycle management.
- Mangle fact engine with derived predicates and causal diagnostics.
- Temporal reasoning over request/navigation/error timelines.
- Token-optimized observation and reasoning (`summary`, `compact`, `full`).
- Docker correlation for browser-to-backend issue tracing.
- React and DOM extraction for UI-state-aware debugging.
- Workspace-discovered repo tracing and phased contract audits with persisted evidence handles.

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

Use for state and affordance discovery:

- Modes: `state`, `nav`, `interactive`, `hidden`, `grids`, `composite`, `sessions`, `screenshot`, `react`, `dom_snapshot`
- Views: `summary`, `compact`, `full`
- Intents: `quick_status`, `find_actions`, `map_navigation`, `hidden_content`, `deep_audit`, `check_sessions`, `visual_check`, `grid_hunt`

### browser-act

Use for deterministic execution:

- Operations include: session create/attach/fork, navigate/history, interact/type/fill/select/toggle, waits, key, js, and plan execution

### browser-reason

Use for triage and root cause analysis:

- Topics: `health`, `next_best_action`, `blocking_issue`, `why_failed`, `what_changed_since`
- Intents: `triage`, `act_now`, `debug_failure`, `unblock`
- Route-scoped mode: `since_navigation=true` to focus on new errors after the latest navigation event

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

- Operations: `query`, `read`, `temporal`, `evaluate`, `push`, `submit_rule`, `subscribe`, `await_fact`, `await_conditions`, `export_flight`

## Debugging Workflow (Recommended)

1. Launch and create a session.
2. `browser-observe` with `intent=quick_status` for instant status.
3. `browser-reason` with `topic=why_failed` and `since_navigation=true`.
4. Drill into evidence handles with `browser-mangle`.
5. Export raw evidence with `export_flight`.

Example sequence:

```json
{"tool":"launch-browser","arguments":{}}
{"tool":"browser-act","arguments":{"operations":[{"type":"session_create","url":"https://cross-thread.ai"},{"type":"await_stable","timeout_ms":12000}]}}
{"tool":"browser-reason","arguments":{"session_id":"<session>","topic":"why_failed","view":"compact","since_navigation":true}}
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

- Repo-owned workspace: run `browsernerd --init-workspace --workspace-dir C:\CodeProjects\MyApp` once, then register an MCP entry that always passes `--workspace-dir C:\CodeProjects\MyApp`.
- Shared/global profile: run with `--no-workspace --config C:\Users\you\.browsernerd\global.yaml` when you want one personal BrowserNERD config and no repo-local overrides.
- Monorepo or partial trace: keep the workspace at the umbrella repo if that is where shared schema and Docker config belong, but set `repo_root` to the specific package or service tree you want `browser-audit` to scan.

Example client configs:

Claude project config (`.mcp.json`):

```json
{
  "mcpServers": {
    "browsernerd-myapp": {
      "command": "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe",
      "args": [
        "--workspace-dir",
        "C:\\CodeProjects\\MyApp"
      ]
    }
  }
}
```

Codex config (`~/.codex/config.toml`):

```toml
[mcp_servers.browsernerd_myapp]
command = "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe"
args = ["--workspace-dir", "C:\\CodeProjects\\MyApp"]

[mcp_servers.browsernerd_shared]
command = "C:\\CodeProjects\\SybioGenv3\\crossthread\\dev_tools\\BrowserNERD\\mcp-server\\bin\\browsernerd.exe"
args = ["--no-workspace", "--config", "C:\\Users\\you\\.browsernerd\\global.yaml"]
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
# Build
go build -o bin/browsernerd.exe ./cmd/server

# Stdio mode (Claude Code / MCP host)
./bin/browsernerd.exe --config config.yaml

# SSE mode
./bin/browsernerd.exe --config config.yaml --sse-port 8080
```

## Native Testing And Smoke Verification

Browser automation tests are native MCP fixtures: `browser-act` operations plus
bounded Mangle assertions. Create, inspect, and run them with `browser-test`;
credentials use `value_env` and are resolved only during execution.

```json
{"tool":"browser-test","arguments":{"operation":"create","session_id":"<session>","name":"login flow"}}
{"tool":"browser-test","arguments":{"operation":"inspect","test_yaml":"<fixture yaml>"}}
{"tool":"browser-test","arguments":{"operation":"run","session_id":"<session>","test_yaml":"<fixture yaml>","view":"summary"}}
```

The example fixture is `testdata/fixtures/login.yaml`. Repository verification
uses Go, including the official MCP Go client:

```bash
go test ./...
go build -o bin/browsernerd.exe ./cmd/server
go run ./cmd/smoke -mode list -server bin/browsernerd.exe
go run ./cmd/smoke -mode smoke -server bin/browsernerd.exe -url https://example.com/
```

## Configuration

Use `config.yaml` (see `config.example.yaml`).

Highlights:

- `mcp.progressive_only`: keep default focused tool surface.
- `browser.repo_trace.*`: bound recursive repo scans, plan generation, and frontend/backend match counts.
- `recorder.enabled` + `recorder.trace_dir`: keep flight evidence available.
- `mangle.fact_buffer_limit`: tune history depth for temporal diagnostics.
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
