---
title: BrowserNERD Delivery and Verification
created: 2026-07-23
last_updated: 2026-07-23
doc_type: delivery-spec
subsystem: browsernerd
read_when: Preparing releases, dependency updates, CI, or completion claims
indexes: [browsernerd-corpus, delivery]
---

# BrowserNERD Delivery and Verification

Production Go files remain below 1,500 lines. Progressive tools, audits,
sessions, and repository tracing are split by responsibility. The nearest
`agents.md` files describe ownership after large refactors.

The 2026-07-23 direct-dependency baseline is Go language level 1.26.0 with
toolchain 1.26.5, Mangle 0.5.0, Rod 0.116.2, and mcp-go 0.57.0.

Automated gates are formatting, `go vet ./...`, `go test ./...`, and
`go build ./cmd/server`, plus focused security and lifecycle regressions.

When Chrome is available, the live gate launches one browser, opens two shared
tabs, creates an isolated tab, launches a second browser, creates and closes a
tab there, then closes all remaining resources and confirms the inventories
are empty.

Significant published changes update the version. Runtime data and binaries
stay out of source control. Stashes, branches, and worktree status are checked
before push. Releases warn that traces produced by older builds may contain
plaintext credentials.
