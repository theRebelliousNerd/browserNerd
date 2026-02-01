# agents.md

Core packages backing the BrowserNERD MCP server.

- `browser`: Rod session manager streaming CDP signals into facts for developer-context and flight-recorder vectors.
- `mcp`: MCP tool registration exposing session controls, reification, fact operations, and awaiters.
- `mangle`: Deductive engine wrapper around schema-driven facts/rules with a circular buffer for time-travel queries.
- `config`: YAML config loading/validation for browser, MCP, and Mangle knobs.
- Keep packages single-responsibility and feed facts through the schema; avoid cross-package side effects.
