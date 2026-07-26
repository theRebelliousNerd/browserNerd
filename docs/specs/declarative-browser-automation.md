---
title: BrowserNERD Declarative Browser Automation
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing observe, act, waits, event correlation, or state deltas
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Declarative Browser Automation

Browser automation should synchronize on meaningful state rather than sleep
durations or one selector at a time.

`browser-observe` provides bounded page state, navigation, interaction, hidden
content, grids, screenshots, React data, DOM facts, and session inventory.
Compact views return stable references; full evidence stays behind handles.

`browser-act` executes bounded sequences covering navigation, interaction,
forms, keyboard input, waits, trusted-and-gated JavaScript, plans, tabs, and
browser instances.
It reports per-operation status and evidence handles.

An agent can declaratively wait for route, network, console, component, toast,
and stable-state conditions together. Subscriptions establish a baseline when
created; historical facts do not trigger a fresh wait.

Since-based views are strict: evidence without a usable timestamp is excluded.
Ordinary health views may preserve useful legacy facts that lack timestamped
predicate variants.

Element references are generation-aware, request and response facts are not
throttled independently, pollers use bounded backoff, and list and query
responses have explicit limits.
