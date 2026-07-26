---
title: BrowserNERD Browser Lifecycle and Concurrency
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing browser launch, sessions, tabs, contexts, or concurrency limits
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Browser Lifecycle and Concurrency

## Defaults

`browser.multi_tab_default` defaults to true. `session_create` opens a normal
tab in the default Chrome instance and shared context, so login state, cookies,
and local storage are available across tabs.

Set `isolated: true` for a fresh incognito context. `session_fork` uses
isolation so divergent or destructive tests do not alter the source context.

## Multiple browsers

BrowserNERD tracks browser instances separately from tabs:

- `list-browsers` returns IDs, status, ownership, default status, and tab count;
- `browser_launch` with `new_instance: true` starts another managed Chrome;
- `session_create` accepts `browser_id`;
- `browser_close` closes one instance and only its tabs;
- closing the default browser promotes a remaining instance.

Launch configuration must allow Rod to allocate distinct endpoints and
profiles. Fixed debugging ports and fixed user-data directories cannot be
reused for multiple managed instances.

When `browser.debugger_url` and `browser.launch` are both empty, Rod discovers
or downloads Chrome and allocates a private endpoint. This portable default
also applies to additional browser instances; a machine-specific executable
path is optional.

## Session lifecycle and limits

Every tab can be listed, created, attached, focused, forked, and closed.
Closing is idempotent and cancels event ingestion, closes the page and isolated
context, removes metadata, and updates persistence.

`max_tabs`, `max_browsers`, and optional `idle_tab_timeout` prevent unbounded
resource growth. Pollers back off and terminate after repeated failure.
Shutdown detaches resources under the manager lock, then closes pages and
browsers outside the lock.

## Live-development workflow

Agents can keep application, documentation, test-user, and diagnostic tabs
open concurrently. A second Chrome instance can represent a different user
profile or a clean end-to-end run without replacing the primary browser.
