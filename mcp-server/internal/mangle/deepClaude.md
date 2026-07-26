---
title: BrowserNERD Mangle Engine
created: 2026-02-07
last_updated: 2026-07-23
doc_type: reference
subsystem: browsernerd
read_when: Changing fact storage, rules, queries, watches, or schemas
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Engine

`internal/mangle` wraps `mangle-go` as a bounded browser fact and reasoning
engine. It is standalone and has no Cross-Thread runtime dependency.

## Package ownership

- `engine.go`: facts, stores, rules, queries, watches, limits, and sampling.
- `schema_loader.go`: single-file, directory, and confined manifest loading.
- `audit_planning.go`: deterministic audit-plan helpers.
- `contract_audit.go`: contract and repository-audit helpers.

Canonical `.mg` files live in `../../schemas/modules/`; `browser.mg` is a
compatibility manifest.

## Safety contract

- Runtime rules and queries are limited by bytes, clauses, premises, total
  rules, created facts, returned rows, and wall-clock time.
- Untrusted evaluation uses an isolated store and a single evaluation slot.
- Panics become explicit errors.
- Dynamic rule updates commit facts and derived state transactionally.
- Watch subscriptions establish a fingerprint baseline and emit only new
  derivations.
- Request/response pairs and high-value causal facts are not sampled apart.
- Browser predicates and joins remain session-scoped.

## Verification

Run:

```bash
go test ./internal/mangle
go test ./...
go vet ./...
```

Public declaration or derivation changes also update
`../../../docs/specs/mangle-schema-modules.md` and
`../../../docs/specs/browser-fact-model.md`.
