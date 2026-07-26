---
title: BrowserNERD Browser Fact Model
created: 2026-07-23
last_updated: 2026-07-23
doc_type: data-spec
subsystem: browsernerd
read_when: Ingesting browser events or authoring Mangle rules
indexes: [browsernerd-corpus, mangle, browser-state]
---

# BrowserNERD Browser Fact Model

Status: shipped.

BrowserNERD normalizes Chrome and tool activity into session-scoped Mangle
facts. Core domains include:

- Page and DOM: `current_url`, `navigation_event`, `dom_node`, `dom_attr`,
  `dom_text`, `dom_layout`, and `dom_updated`.
- React: `react_component`, `react_prop`, `react_state`, and `dom_mapping`.
- Network: `net_request`, `net_response`, `net_header`,
  `net_correlation_key`, and `request_initiator`.
- User and UI events: `console_event`, `toast_notification`, `click_event`,
  `input_event`, `state_change`, and interaction facts.
- Automation and evidence: form, plan, screenshot, audit, repo-trace,
  disclosure, and JS-gate facts.

Every browser-originated fact that can overlap across tabs carries
`SessionId`. Request and response facts preserve the same CDP request id; load
shedding must never independently discard one side of the pair.

Successful structured navigation always emits `current_url`, including a
same-URL no-op, so declarative assertions do not depend on CDP event timing.

Facts have both logical timestamp arguments where required by a rule and the
Go `Fact.Timestamp` used for temporal windows. The engine maintains a bounded
fact buffer and predicate index, mirrors timestamped facts into temporal
predicates, and rebuilds derived state after bounded updates.

Credential-bearing header and input values are replaced before they reach the
engine. Password, token, cookie, authorization, payment-secret, and
one-time-code values must never become queryable plaintext facts.
