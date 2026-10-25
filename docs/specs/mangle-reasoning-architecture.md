---
title: BrowserNERD Mangle Reasoning Architecture
created: 2026-07-23
last_updated: 2026-10-23
doc_type: architecture-spec
subsystem: browsernerd
read_when: Changing facts, rules, queries, subscriptions, causal reasoning, or memory bounds
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Reasoning Architecture

Mangle is the shared semantic layer between browser capture, repository
evidence, specifications, automation, testing, and diagnosis.

The maintained runtime is `codeberg.org/TauCeti/mangle-go` v0.6.0. Release
tags, not the moving upstream main branch, define the production dependency
boundary. Any upgrade requires a freshness check, upstream behavior review,
complete schema analysis, derivation tests, and bounded performance profiling.

Canonical logic is split across the domain modules documented in
[Mangle schema modules](mangle-schema-modules.md); the built-in modules are
compiled into the binary and project modules load on top of them.

Fact classes cover browser topology, DOM and interaction, React semantics,
runtime events, actions, repository correlations, specifications, and derived
causal conclusions ([browser fact model](browser-fact-model.md)).

## Buffer and retention

Facts live in a bounded buffer of `mangle.fact_buffer_limit` facts (default
16384) over a store indexed on each predicate's leading arguments. When the
buffer passes its limit it trims to 90% of the limit, shedding by signal
class, oldest first within a class:

- low: page snapshots and bookkeeping that the next observe regenerates (DOM,
  React, interactive-element and disclosure facts), and the facts of a
  successful request to a URL the session requests repeatedly - polling - or
  of a canceled one;
- medium: everything not named low or high;
- high, kept until nothing else is left: every fact of a failed request,
  console errors and warnings, WebSocket errors and closes, error-level Docker
  lines, navigations, `current_url`, page load failures, dialogs, downloads,
  browser log entries, toasts, live-region announcements, and user actions.

A request's facts (`net_request`, `net_response`, headers, correlation keys,
initiator, payload fields and the failure facts) share one class and are kept
or evicted as a unit, so a response is never kept without its request. Over
half the limit, low-value predicates are also sampled on the way in. The
engine keeps only the latest `current_url` per session.

Authorization is held in a separate bounded, engine-owned TTL ledger rather
than inferred from retained facts. `browser-reason` atomically issues exact
session/handle and session/reason keys for ten minutes. The ledger bounds
sessions, keys and accounted bytes, prunes expired entries lazily and refuses
capacity without evicting live grants or partially refreshing a bundle.
Observation sampling, trims and raw `disclosure_handle`/`js_gate_open` writes
cannot change this authority. Those predicates remain best-effort telemetry;
their absence in a Mangle read does not mean an issued grant disappeared.
Only the real reason producer calls the typed publication API; ordinary fact
ingestion and non-reason disclosure emitters do not mint grants. The exact
limits and reader-facing issuance receipt are defined by the progressive tool
contract.

A read evaluates only the rules the predicate it asks for depends on, over the
indexed store, and caches the derivation until the next write. New
subscriptions snapshot matching fingerprints at creation and emit only fresh
derivations. Since-scoped queries require usable timestamps. The engine's
self-report (`Engine.Stats`: facts by class, evictions, trims, sampling,
rules, predicates, modules, last evaluation time and error) feeds the doctor,
which flags a failed last evaluation and any evicted high-signal fact.
`BROWSERNERD_DEBUG=1` turns on engine debug logging; without it the engine
logs nothing per fact.

## Queries

`browser-mangle` `query` accepts one atom, a comma conjunction, or a rule
`head :- body`. A conjunction joins without a rule: it is evaluated as a
temporary program that is never persisted, under the same limits as
`submit_rule`, and returns every variable it binds in order of first
appearance. Before anything is evaluated, an atom naming an unknown predicate
or giving a known one the wrong argument count is refused with the declared
signature and its module, or with the closest predicate names - for example
`net_response takes 5 arguments, not 4: net_response(SessionId, Id, Status,
Latency, Duration) [builtin:00_core_events]`. Such a query could never match,
and answering it with zero rows would read as "nothing happened"; the same
check makes a `browser-test` assertion with a misspelled predicate fail
instead of passing. `operation: schema` lists the predicates (`filter` narrows
by substring): the summary view counts them per module, compact lists names
per module until filtered and then signatures with kind, module and fact
count, and full returns the objects. `submit_rule` evaluates only the new
rule and what it depends on.

```yaml
mangle:
  max_rule_bytes: 65536
  max_query_bytes: 32768
  max_rule_clauses: 16
  max_premises: 64
  max_rules: 128
  max_created_facts: 10000
  max_query_results: 1000
  evaluation_timeout: 2s
```

Untrusted rules are parsed, analyzed, and evaluated on isolated state before
commit. A timed-out evaluation cannot mutate the live store.

Queries preserve complete clause bodies, including joins and comparisons.
Fallback behavior cannot turn a parse or evaluation failure into a successful
empty result. Adaptive sampling protects high-value causal predicates and
retains correlated network facts as a unit.

Checked-in rule changes must pass four distinct gates:

1. The TauCeti parser and analyzer accept the complete lexical module bundle.
2. Stratification analysis finds no negative dependency cycle.
3. Module analysis recognizes built-ins, external predicates, and legal
   multi-clause Datalog unions without hiding arity conflicts.
4. Performance analysis reports no high-risk Cartesian join or unbounded
   recursion.

Static helper scripts supplement the real Go runtime tests. A static diagnostic
that disagrees with the pinned parser is a validator defect to fix and cover
with a regression test, not a reason to distort valid Mangle source.
