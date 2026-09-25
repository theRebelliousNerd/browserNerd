---
title: BrowserNERD Mangle Schema Modules
created: 2026-07-23
last_updated: 2026-09-24
doc_type: architecture-spec
subsystem: browsernerd
read_when: Adding facts, rules, queries, or schema loading behavior
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Schema Modules

Status: shipped.

## Built-in and project modules

The built-in schema is compiled into the binary (`mcp-server/schemas/embed.go`
embeds `browser.mg` and `modules/*.mg`). The server reasons over the facts its
own Go code emits, so the declarations of those facts ship with that code and
a workspace can never run against a stale copy of them. `browser.mg` is a
manifest (`# @include modules`); its modules load in lexical order:

- `00_core_events.mg`: browser, React, DOM, network, failure, dialog,
  download and event facts.
- `10_causal_temporal.mg`: temporal, causal, toast, and predictive rules.
- `20_automation_testing.mg`: interaction, login, automation, and tests.
- `30_docker_correlation.mg`: backend and cross-layer correlations.
- `40_ui_quality.mg`: fingerprints, page state, accessibility, and forms.
- `50_contract_audit.mg`: frontend-to-API contract evidence.
- `60_audit_planning.mg`: audit state, hazards, and deterministic plans.
- `70_semantic_ui.mg`: semantic UI macros.
- `75_page_structure.mg`: page structure observed by `browser-observe` (dead
  controls).
- `80_progressive_planning.mg`: evidence disclosure, JS gates, and actions.

Project modules load on top of the built-in ones. `mangle.schema_path` names
them - a single `.mg` file, a directory of modules, or a manifest of relative
`# @include` directives - and is empty by default. A workspace whose
`.browsernerd/schemas/` directory holds `.mg` files loads them as project
modules without any setting. A project module may add rules to a built-in
predicate but may not redeclare one; loading fails naming both modules. A
project module byte-identical to a built-in one is that module and loads
once. `mangle.disable_builtin_rules: true` drops the built-in schema and
leaves the project modules as the whole program, at which point capture emits
facts that no declaration or rule describes. The schema listing and the doctor
label each module `builtin:<name>` or `project:<name>`, the name being the
file name without `.mg`.

Directory modules load in lexical order. Includes are confined to the manifest
root, cycles are deduplicated, empty module sets fail explicitly, and parse
errors name the failing file.

## Changing the schema

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
