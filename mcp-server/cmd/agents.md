# agents.md

Entry points for running the BrowserNERD MCP server.

- `server/main.go` wires config parsing and starts the stdio or SSE server; keep it thin and delegate to internal packages.
- Flags should surface PRD toggles (detached sessions, DOM/header ingestion, fact buffers) without embedding business logic.
- Ensure startup/shutdown respects the persistent session store and cleans up Rod per the Vector 3 continuity goals.
- When adding commands or flags, update `../docs/QUICKSTART_GUIDE.md` and keep behavior consistent with the BrowserNERD PRD.
