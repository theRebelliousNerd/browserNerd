# agents.md

MCP surface that exposes BrowserNERD tools.

- Registers tools for session control (list/create/attach/fork), React reification, DOM snapshotting, fact push/read, and await-fact/await-conditions per the PRD toolset.
- Uses `mark3labs/mcp-go` to serve stdio or SSE; keep handlers thin and delegate to browser/mangle packages.
- When adding tools, define JSON schemas, update docs, and ensure behavior matches the deterministic logic/testing model instead of imperative scripting.
- Avoid embedding side effects outside the session manager or engine; prefer idempotent fact operations and schema-aligned payloads.
