# BrowserNERD MCP Server

Detached Rod + Mangle MCP server for the BrowserNERD PRD. Ships session management, CDP ingestion (network/console/navigation/DOM), React Fiber reification, and logic-based assertions.

## Features

- MCP stdio/SSE via `mark3labs/mcp-go`.
- Session controls: `list-sessions`, `create-session`, `attach-session`, `fork-session`.
- React reification: `reify-react` emits component/prop/state facts.
- DOM snapshotting: automatic and on-demand (`snapshot-dom`), header ingestion toggle.
- Fact/logic helpers: `push-facts`, `read-facts`, `await-fact`, `await-conditions`.

## Quickstart

```bash
go mod tidy
go run ./cmd/server --config config.yaml           # stdio
go run ./cmd/server --config config.yaml --sse-port 2024  # SSE
```

`config.example.yaml` includes Chrome attach/launch options and ingestion toggles.

## Tools

- Session: `list-sessions`, `create-session`, `attach-session`, `fork-session`
- React: `reify-react`
- DOM: `snapshot-dom`
- Facts: `push-facts`, `read-facts`
- Assertions: `await-fact`, `await-conditions`

## Schema

`schemas/browser.mg` defines predicates for React (component/prop/state), DOM (node/attr), navigation, console, and network (request/response/header) facts.

## Notes

- DOM ingestion is sampled (default 200 nodes) to limit fact volume.
- Header ingestion is optional; toggle via config.
- Session metadata persists to disk (`session_store`) so detached sessions survive restarts.
