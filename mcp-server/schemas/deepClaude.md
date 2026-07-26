---
title: BrowserNERD Mangle Schemas
created: 2026-02-07
last_updated: 2026-07-23
doc_type: reference
subsystem: browsernerd
read_when: Changing BrowserNERD facts or rules
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Schemas

BrowserNERD is a standalone development tool. Schemas must not import or
depend on Cross-Thread runtime code, databases, credentials, APIs, containers,
or Vectryx instances.

`browser.mg` is a compatibility manifest that includes `modules/`. The engine
also accepts `modules/` directly as `mangle.schema_path`.

## Modules

| Module | Ownership |
|---|---|
| `00_core_events.mg` | React, DOM, network, console, navigation, and base UI events |
| `10_causal_temporal.mg` | Failure, latency, toast, temporal, and predictive rules |
| `20_automation_testing.mg` | Interaction, login, automation, and test predicates |
| `30_docker_correlation.mg` | Docker log and browser/backend correlation |
| `40_ui_quality.mg` | Fingerprints, page state, accessibility, forms, and sequences |
| `50_contract_audit.mg` | Frontend/API contract and repository evidence |
| `60_audit_planning.mg` | Audit lifecycle, hazards, execution, and deterministic planning |
| `70_semantic_ui.mg` | Semantic page and primary-action macros |
| `80_progressive_planning.mg` | Evidence handles, JS gates, and action candidates |

Modules load in lexical order and analyze as one program. Cross-module
references are allowed; duplicate declarations are not. Keep each module
below 500 lines.

All browser predicates that can overlap between tabs must carry `SessionId`.
Facts containing headers, inputs, URLs, logs, DOM values, or tool arguments
must already be redacted before insertion. Rules must preserve session joins
and avoid cross-session Cartesian products.

Changes require focused Mangle tests plus the full Go suite. Update
`../../docs/specs/mangle-schema-modules.md` and
`../../docs/specs/browser-fact-model.md` when the public fact contract changes.
