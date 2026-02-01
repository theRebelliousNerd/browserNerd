# agents.md

Session and event bridge between Rod and the Mangle fact sink.

- Responsibilities: connect/launch Chrome, maintain the session registry, persist metadata, and expose create/attach/fork per the PRD’s detached session model (Vector 3).
- Context capture: streams navigation, console, network, and optional DOM snapshots into facts; uses DOM/header toggles to control volume and protect the fact buffer (Vectors 1 and 2).
- Semantic reification: `ReifyReact` walks the React Fiber tree to emit `react_*` and `dom_mapping` facts for developer-context reasoning (Vector 1).
- Tool support: one-off `SnapshotDOM` plus continuous event feeds let logic rules and awaiters evaluate recent state (Vector 4).
- Contribution tips: honor config timeouts, keep sampling limited, and send facts through `EngineSink` using predicates defined in `../../schemas/browser.mg`.
