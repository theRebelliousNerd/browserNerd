---
title: BrowserNERD Progressive MCP Tool Contract
created: 2026-07-23
last_updated: 2026-07-23
doc_type: interface-spec
subsystem: browsernerd
read_when: Changing tool registration, schemas, response views, or evidence handles
indexes: [browsernerd-corpus, mcp]
---

# BrowserNERD Progressive MCP Tool Contract

Status: shipped.

The default MCP surface contains 11 tools: `launch-browser`,
`shutdown-browser`, `close-session`, `browser-observe`, `browser-act`,
`browser-reason`, `browser-audit`, `get-specs`, `check-specs`,
`browser-test`, and `browser-mangle`. Setting `mcp.progressive_only: false`
adds the focused lower-level tools for a total of 48.

Progressive tools accept `summary`, `compact`, or `full` views. Summary
returns status and counts. Compact adds the small set needed for the next
decision. Full returns bounded underlying evidence. Responses expose evidence
handles when further investigation is useful; an agent expands those handles
instead of repeating an entire trace.

`browser-observe` discovers current state and affordances. `browser-act`
executes an ordered `operations` array. `browser-reason` queries causal and
temporal conclusions. `browser-audit` persists a phased browser-to-code
investigation. `browser-mangle` is the bounded escape hatch for raw facts,
queries, rules, waits, and flight exports. Specs and tests remain first-class
progressive tools rather than optional scripts.

Every tool result crosses the credential redactor before MCP delivery. Tool
schemas, registration counts, README references, and tests must change
together whenever this contract changes.
