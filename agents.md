# BrowserNERD Agent Guide

BrowserNERD is a standalone development tool. It may inspect a target project,
but it must not import or depend on Cross-Thread runtime packages, databases,
credentials, APIs, containers, or Vectryx instances.

## Canonical product documentation

- `docs/PRD.md` is the concise product index.
- `docs/specs/` is the canonical BrowserNERD product corpus.
- `docs/research/` preserves supporting architecture research.
- Target project specs and indexes are read-only configured inputs, never
  BrowserNERD-owned platform documents.

## Engineering rules

- Keep production Go files below 1,500 lines.
- Keep security and privacy default-on.
- Shared-context multi-tab operation is the browser default.
- Multiple browser instances and isolated tabs must have explicit lifecycle
  and resource bounds.
- Write tests for every code change and run focused tests before the full suite.
- Update the nearest `agents.md` after large refactors.
- Use ASCII in code and documentation.
- Update the release version for significant published changes.

## Build loop

1. Run focused tests.
2. Batch edits.
3. Run formatting, focused tests, full tests, vet, and build.
4. Run a real-Chrome smoke test when Chrome is available.
5. Ask the user to reload the MCP binary only after a verified build.
