# Browser and Session Runtime

Browser instances and tabs are separate resources. Shared-context tabs are the
default; isolated contexts are explicit. Every tab and browser must be
discoverable, bounded, and closable.

An empty debugger URL and launch list means Rod-managed browser discovery and
private endpoints. Do not reintroduce machine-specific paths as defaults.

## Modular boundaries

- `browser_instances.go`: browser inventory, launch, tab create/attach/focus,
  close, and idle reaping.
- `session_core.go`: public metadata, manager lifecycle, React reification,
  runtime evidence.
- `session_events.go`: CDP capture, redaction, DOM snapshots, event backoff.
- `browser_instances_live_test.go`: real-Chrome proof for shared tabs,
  isolated contexts, multiple instances, focus, close, and default promotion.
- `session_persistence.go`: private, sanitized session metadata.
- `repo_trace_capture.go`: browser context and public trace types.
- `repo_trace_scan.go`: bounded repository scanning.
- `repo_trace_planning.go`: hints, hazards, and audit plans.
- `repo_trace_matching.go`: frontend and backend scoring/correlation.
- `repo_trace_facts.go`: fact emission and trace helpers.

Never hold the manager lock while closing Rod pages or browser processes.
Request and response facts must be sampled as a correlated unit.
