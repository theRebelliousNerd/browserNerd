# MCP Tool Surface

The default progressive surface includes browser lifecycle, close-session,
observe, act, reason, audit, Mangle, get-specs, check-specs, and browser-test.

## Modular boundaries

- `progressive_observe.go`: token-aware browser observation.
- `progressive_act.go`: action sequencing and lifecycle operations.
- `progressive_reason.go`: reasoning entry point.
- `progressive_evidence.go`: error ranking and disclosure.
- `progressive_planning.go`: intents, deltas, and action recommendations.
- `progressive_mangle.go`: Mangle operations and confined flight export.
- `audit_*.go`: audit core, repo correlation, discovery, persistence,
  execution, evidence, and findings.
- `spec_tools.go`: configured spec delivery and conformance.
- `browser_test_tool.go`: script-free test create, inspect, run, and disclosure.
- `test_tools.go`: native fixtures, environment-only secret resolution,
  fresh-fact assertions, replay, and causal diagnosis.

Tool schemas and docs must change with contracts. Model-supplied paths go
through the path policy. Untrusted page values are passed as evaluation data,
not interpolated JavaScript source. `evaluate-js` remains disabled unless a
trusted operator enables it, and audit persistence uses private files.
