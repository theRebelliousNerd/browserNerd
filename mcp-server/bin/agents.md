# agents.md

Built binaries for the BrowserNERD MCP server.

- Contains compiled artifacts like `browsernerd-mcp-server`; sources live in `../cmd/server` and `../internal`.
- Rebuild binaries after changing Go sources to keep behavior aligned with the PRD vectors and schema.
- Do not hand-edit files here; regenerate via standard Go build flows documented in `../docs/QUICKSTART_GUIDE.md`.
