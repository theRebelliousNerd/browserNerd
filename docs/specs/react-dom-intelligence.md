---
title: BrowserNERD React and DOM Intelligence
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing React extraction, DOM snapshots, refs, or UI bindings
indexes: [browsernerd-corpus, frontend-intelligence]
---

# BrowserNERD React and DOM Intelligence

Status: shipped with framework limits.

BrowserNERD extracts compact DOM structure and, when available, traverses
React Fiber to restore the component vocabulary lost in rendered HTML.
Component name, parentage, bounded props/state, and fiber-to-DOM mappings
become Mangle facts. The design follows the semantic-reification direction in
the architecture validation rather than treating HTML as the primary agent
interface.

Interactive discovery returns sparse semantic refs, labels, action types,
enabled/visible state, bounded geometry, and alternative selectors. A
per-session fingerprint registry supports re-identification and reports
property changes after DOM updates. Navigation clears stale identities.

DOM and React payloads are bounded and sampled as lower-value high-volume
facts. Console errors, request/response pairs, route changes, and explicit
actions retain higher priority. Password and other sensitive input values are
redacted in page JavaScript, in Go ingestion, in Mangle facts, and again at
the MCP response boundary.

React extraction is best-effort because production builds and future React
internals can hide or change Fiber structures. Failure to reify React must
leave DOM automation usable and return explicit capability status.
