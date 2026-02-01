# agents.md

BrowserNERD MCP server implements the PRD’s Rod + Mangle bridge so agents get semantic, queryable browser state.

- Role: persistent MCP daemon that keeps Chrome sessions alive, ingests CDP signals, and streams them into Mangle for declarative checks instead of brittle DOM scraping.
- PRD alignment: covers React semantic reification (Vector 1), flight-recorder event capture (Vector 2), detached sessions and forking (Vector 3), and logic-based assertions/time-travel debugging (Vector 4).
- Structure: `cmd/server` bootstraps config and servers; `internal/browser` manages Rod sessions and fact emission; `internal/mcp` registers tools; `internal/mangle` buffers facts/queries; `schemas/browser.mg` defines predicates/rules; `docs/` holds quickstart and validation notes.
- Contribution cues: keep new tools idempotent and cost-aware; emit facts that match the schema and avoid overloading the buffer (use sampling/throttling); prefer adding new logic rules over embedding imperative verification.
- References: `../docs/PRD.md` for architecture rationale and `../docs/mangle-programming-references` for Mangle syntax/examples.
