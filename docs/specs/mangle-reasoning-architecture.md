---
title: BrowserNERD Mangle Reasoning Architecture
created: 2026-07-23
last_updated: 2026-07-23
doc_type: architecture-spec
subsystem: browsernerd
read_when: Changing facts, rules, queries, subscriptions, causal reasoning, or memory bounds
indexes: [browsernerd-corpus, mangle]
---

# BrowserNERD Mangle Reasoning Architecture

Mangle is the shared semantic layer between browser capture, repository
evidence, specifications, automation, testing, and diagnosis.

The maintained runtime is `codeberg.org/TauCeti/mangle-go` v0.5.0. Release
tags, not the moving upstream main branch, define the production dependency
boundary. Any upgrade requires a freshness check, upstream behavior review,
complete schema analysis, derivation tests, and bounded performance profiling.

Canonical logic is split across the domain modules documented in
[Mangle schema modules](mangle-schema-modules.md). `browser.mg` remains a
small compatibility manifest rather than a monolithic rules file.

Fact classes cover browser topology, DOM and interaction, React semantics,
runtime events, actions, repository correlations, specifications, and derived
causal conclusions.

Temporal facts retain timestamps in a bounded buffer and predicate index. New
subscriptions snapshot matching fingerprints at creation and emit only fresh
derivations. Since-scoped queries require usable timestamps.

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
