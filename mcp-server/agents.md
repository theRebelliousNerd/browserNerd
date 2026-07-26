# BrowserNERD MCP Server

This standalone Go server connects Rod browser state to bounded Mangle facts
and exposes the result through MCP.

## Package map

- `cmd/server`: configuration, startup, signal handling, transports.
- `internal/browser`: browser instances, shared and isolated tabs, CDP capture,
  repository tracing, session persistence.
- `internal/mcp`: progressive tools, audit workflow, session tools, specs,
  screenshots, testing, and MCP binding.
- `internal/mangle`: bounded fact store, rules, queries, and fresh-fact watches.
- `internal/spec`: generic Markdown corpora, index hints, matching, invariants.
- `internal/security`: redaction and confined path writes.
- `internal/recorder`: privacy-safe diagnostic flight recording.
- `cmd/smoke`: Go-native stdio MCP verification; product automation remains
  declarative fixtures executed through `browser-test`.

Keep handlers thin, make side effects explicit, and preserve the trust
boundaries in `../docs/specs/security-and-privacy.md`.
