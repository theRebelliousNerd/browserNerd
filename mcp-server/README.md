# BrowserNERD MCP Server

BrowserNERD is a production-grade browser intelligence copilot: Rod automation plus Mangle reasoning, wrapped in an MCP interface that keeps agents fast, grounded, and debuggable.

If you want fewer blind clicks, fewer hallucinated root causes, and faster time-to-fix, this is the stack.

## Why BrowserNERD

Most browser automation tools can click. BrowserNERD can explain.

- It observes: network, console, DOM, toasts, navigation, React, and Docker logs.
- It reasons: causal chains and temporal patterns via Mangle.
- It acts: guided next-step plans with evidence handles.
- It records: raw JSONL flight logs for postmortem replay and RCA.

## What Is New In v0.0.8

This release pushes BrowserNERD into a stronger debugging and triage posture:

- Progressive disclosure-first UX (6 primary tools, full power behind mode/operation dispatch).
- Compiler/build error prioritization in `browser-reason` top errors.
- Repeated console error dedupe for cleaner, token-efficient summaries.
- Route-scoped reasoning mode with `since_navigation=true`.
- Temporal and change-window diagnostics with `time_window_ms` + Mangle timestamps.
- Flight recorder export hardening for raw evidence capture.
- Investigation steps tuned to error class (`compiler_error` -> `console_event` queries, toast classes -> toast queries).

## Core Capabilities

- Rod-backed browser control and session lifecycle management.
- Mangle fact engine with derived predicates and causal diagnostics.
- Temporal reasoning over request/navigation/error timelines.
- Token-optimized observation and reasoning (`summary`, `compact`, `full`).
- Docker correlation for browser-to-backend issue tracing.
- React and DOM extraction for UI-state-aware debugging.

## Progressive Disclosure (Default)

With `mcp.progressive_only: true`, BrowserNERD exposes 6 high-signal tools by default:

- `launch-browser`
- `shutdown-browser`
- `browser-observe`
- `browser-act`
- `browser-reason`
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
{"tool":"browser-act","arguments":{"operations":[{"type":"session_create","url":"https://symbiogen.ai"},{"type":"await_stable","timeout_ms":12000}]}}
{"tool":"browser-reason","arguments":{"session_id":"<session>","topic":"why_failed","view":"compact","since_navigation":true}}
{"tool":"browser-mangle","arguments":{"operation":"export_flight","session_id":"<session>","include_server_logs":true}}
```

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

## Build And Run

```bash
# Build
go build -o bin/browsernerd.exe ./cmd/server

# Stdio mode (Claude Code / MCP host)
./bin/browsernerd.exe --config config.yaml

# SSE mode
./bin/browsernerd.exe --config config.yaml --sse-port 8080
```

## Local Smoke Harness

```bash
python scripts/mcp_smoke.py list
python scripts/mcp_smoke.py smoke --url https://example.com/
python scripts/mcp_smoke.py smoke --go-test --build --url https://example.com/
python scripts/mcp_smoke.py repl
```

## Configuration

Use `config.yaml` (see `config.example.yaml`).

Highlights:

- `mcp.progressive_only`: keep default focused tool surface.
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
