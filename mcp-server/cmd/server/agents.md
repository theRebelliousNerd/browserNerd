# agents.md

Main binary entrypoint for the BrowserNERD MCP server.

- `main.go` composes config loading, session manager construction, mangle engine bootstrap, and MCP server startup.
- Keep orchestration minimal: handle contexts, defer shutdown, and choose stdio vs SSE based on config.
- New behavior belongs in `internal/*`; this layer only glues components per the detached server model in the PRD.
