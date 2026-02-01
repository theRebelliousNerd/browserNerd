# agents.md

Config loader for the BrowserNERD MCP server.

- Parses YAML with defaults for server metadata, Rod launch/attach settings, DOM/header ingestion toggles, session store path, SSE port, and Mangle schema/buffer limits.
- Validates required knobs (server name, debugger URL or launch command) to keep the daemon predictable.
- Align new settings with PRD vectors (sampling, buffering, persistence) and update `../../docs/QUICKSTART_GUIDE.md` when behavior changes.
- Keep this package limited to shaping configuration and helpers—runtime logic belongs elsewhere.
