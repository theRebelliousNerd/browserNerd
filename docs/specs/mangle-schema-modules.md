---
title: BrowserNERD Mangle Schema Modules
created: 2026-07-23
last_updated: 2026-07-23
doc_type: architecture-spec
subsystem: browsernerd
read_when: Adding facts, rules, queries, or schema loading behavior
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Schema Modules

Status: shipped.

BrowserNERD loads Mangle from a single `.mg` file, a directory of `.mg`
modules, or a manifest containing relative `# @include` directives. Directory
modules load in lexical order. Includes are confined to the manifest root,
cycles are deduplicated, empty module sets fail explicitly, and parse errors
name the failing file.

`mcp-server/schemas/browser.mg` is the compatibility manifest. Its modules are:

- `00_core_events.mg`: browser, React, DOM, network, and event facts.
- `10_causal_temporal.mg`: temporal, causal, toast, and predictive rules.
- `20_automation_testing.mg`: interaction, login, automation, and tests.
- `30_docker_correlation.mg`: backend and cross-layer correlations.
- `40_ui_quality.mg`: fingerprints, page state, accessibility, and forms.
- `50_contract_audit.mg`: frontend-to-API contract evidence.
- `60_audit_planning.mg`: audit state, hazards, and deterministic plans.
- `70_semantic_ui.mg`: semantic UI macros.
- `80_progressive_planning.mg`: evidence disclosure, JS gates, and actions.

New declarations and rules belong in the narrowest existing module. A new
module is warranted when it establishes an independently explainable fact
domain. Modules should remain below 500 lines. Tests must prove the complete
module set analyzes together and that any new derived predicate returns the
intended session-scoped result.

Runtime `submit-rule` additions are separate, bounded overlays. They must not
be used to hide missing canonical declarations from the checked-in modules.

The repository Mangle programming skill owns the shared syntax, built-in,
stratification, module, and profiling vocabulary. BrowserNERD owns the runtime
truth: `go test ./internal/mangle` must parse and analyze the exact manifest
and modules shipped with the server. Both checks are required because a text
scanner cannot replace the language parser, while focused static analysis can
still expose expensive joins that are syntactically valid.
