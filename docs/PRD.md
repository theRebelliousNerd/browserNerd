---
title: BrowserNERD Product Requirements
created: 2025-11-23
last_updated: 2026-07-23
doc_type: prd
subsystem: browsernerd
read_when: Planning or implementing BrowserNERD product behavior
indexes: [browsernerd-corpus]
---

# BrowserNERD Product Requirements

BrowserNERD is a standalone browser intelligence and automation server for AI
coding agents. It combines Rod, Chrome DevTools Protocol events, and Mangle so
agents can query browser state, wait declaratively, test requirements, and
diagnose failures without repeatedly consuming raw DOM, screenshot, and log
dumps.

## Product thesis

The durable advantage is browser state as a bounded, privacy-safe, queryable
fact database. Browser actions, specifications, tests, and causal diagnosis
all use that common representation.

BrowserNERD is not part of the Cross-Thread runtime. Cross-Thread specs and
indexes can be configured as one read-only input corpus, exactly like those of
any other project.

## Product requirements

1. Security and privacy are default-on at every capture and persistence
   boundary.
2. Shared-context multi-tab automation is the default. Isolated tabs and
   multiple independent Chrome instances are explicit, first-class options.
3. Sessions have complete lifecycle controls: list, create, attach, focus,
   fork, close, bound, reap, and shutdown.
4. Declarative waits observe newly derived facts and preserve correlated event
   pairs.
5. Mangle rules and queries are bounded by size, complexity, results, facts,
   and wall-clock time.
6. Project specs are loaded from configurable named Markdown corpora and
   optional indexes. Generic prose is deliverable context; BrowserNERD
   invariants are executable conformance checks.
7. Observe and act responses can deliver compact route- and feature-relevant
   spec context.
8. Recorded browser activity can become compact declarative tests whose
   failures drill into Mangle-derived causes.
9. Model-directed file writes remain inside configured writable roots.
10. Production Go modules remain focused and below 1,500 lines.

## Canonical corpus

- [Security and privacy](specs/security-and-privacy.md)
- [Browser lifecycle and concurrency](specs/browser-lifecycle-and-concurrency.md)
- [Declarative browser automation](specs/declarative-browser-automation.md)
- [Configurable spec delivery](specs/configurable-spec-delivery.md)
- [Token-efficient testing](specs/token-efficient-testing.md)
- [Mangle reasoning architecture](specs/mangle-reasoning-architecture.md)
- [Mangle schema modules](specs/mangle-schema-modules.md)
- [Progressive MCP tool contract](specs/progressive-mcp-tool-contract.md)
- [Browser fact model](specs/browser-fact-model.md)
- [React and DOM intelligence](specs/react-dom-intelligence.md)
- [Flight recorder and evidence](specs/flight-recorder-and-evidence.md)
- [Contract audit and repository tracing](specs/contract-audit-and-repo-tracing.md)
- [Browser observation and interaction](specs/browser-observation-and-interaction.md)
- [Workspace configuration](specs/workspace-configuration.md)
- [Delivery and verification](specs/delivery-and-verification.md)
- [Integrated implementation contract](specs/secure-declarative-automation.md)

The original architecture validation and research citations are preserved in
[Mangle-MCP architecture validation](research/mangle-mcp-architecture-validation.md).

## Success measures

- Zero plaintext credentials in facts, traces, exports, or session files.
- Zero untrusted workspace-triggered process launches.
- No unbounded user-submitted Mangle execution.
- All tabs and browser instances are individually discoverable and closable.
- Spec delivery returns bounded relevant excerpts rather than whole corpora.
- Declarative waits do not pass on stale history.
- Focused tests, full tests, vet, build, and real-Chrome smoke checks pass.
