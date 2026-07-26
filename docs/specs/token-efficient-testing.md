---
title: BrowserNERD Token-Efficient Testing
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing flight recording, generated tests, assertions, replay, or failure diagnosis
indexes: [browsernerd-corpus, testing]
---

# BrowserNERD Token-Efficient Testing

Status: native create, inspect, and run workflow shipped.

Tests are portable YAML fixtures containing the same operations accepted by
`browser-act` plus Mangle assertions. The progressive `browser-test` tool
creates a fixture from privacy-safe action facts, validates it, replays it, and
returns a bounded result. `run-test` and `generate-test` remain available as
focused tools when the full surface is enabled.

Credential inputs use `value_env`, for example
`value_env: BROWSERNERD_TEST_PASSWORD`. The environment value is resolved only
in an execution copy and is never written back into the fixture. Credential
facts and results remain redacted.

Execution establishes a per-assertion baseline, replays through `browser-act`,
and evaluates complete bounded queries. Action tests default to `scope:
fresh`, so errors that existed before the test do not cause false failure.
Assertion-only tests default to the current fact state.

Failure diagnosis returns compact causal facts: failed requests, console
errors, user-visible toasts, route or component mismatches, spec violations,
and probable frontend or backend code locations. Evidence handles permit
drill-down without attaching every trace row.

Declarative fixtures are the source of truth. The old Python smoke scripts are
replaced by the MCP workflow and a Go-native stdio verifier. The Python
evaluation directories are external benchmark adapters, not the product test
runner. Framework export, fixture versioning, and isolated replay profiles
remain future extensions.
