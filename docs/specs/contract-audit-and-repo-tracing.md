---
title: BrowserNERD Contract Audit and Repository Tracing
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing browser-audit, repo tracing, plans, hazards, or evidence persistence
indexes: [browsernerd-corpus, webdev]
---

# BrowserNERD Contract Audit and Repository Tracing

Status: shipped.

`browser-audit` connects current route, forms, controls, React hints, network
requests, backend expectations, and bounded repository matches. It does not
depend on a prebuilt Cross-Thread index and does not make BrowserNERD part of
the Cross-Thread runtime.

The audit lifecycle is:

1. `discover`: passive page and repository inventory plus deterministic plan.
2. `execute`: replay persisted steps under explicit navigation, mutation,
   risk, and destructive-action gates.
3. `report`: synthesize browser, Mangle, backend-log, and repository evidence.
4. `resume`: reopen only requested evidence handles.

Audit state and discovered hazards are represented in Mangle. Repository scans
are confined to explicit `repo_root`, bounded by file, byte, match, and depth
limits, and return likely locations rather than unbounded source dumps.
Same-route reveal controls and owned navigation can be planned separately from
write-capable or destructive controls.

Results must distinguish observation, inference, skipped work, approval-gated
work, execution failure, and confirmed contract mismatch.
