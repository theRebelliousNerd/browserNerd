---
title: BrowserNERD Browser Lifecycle and Concurrency
created: 2026-07-23
last_updated: 2026-09-24
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
resource growth. `max_tabs` counts OPEN tabs - sessions holding a page. A
detached session (reloaded from the session store after a restart, or left
when its browser went away) owns no tab and never counts toward the limit
(`internal/browser/browser_instances.go` `openTabCountLocked`). Pollers back off and terminate after repeated failure.
Shutdown detaches resources under the manager lock, then closes pages and
browsers outside the lock.

## Several server instances

Every MCP client spawns its own BrowserNERD process, and a process with an
empty `debugger_url` and `launch` launches its own Chrome with a private
temporary profile and a random debugging port, so two clients never drive the
same browser or tab. Processes started with the same config do share the files
it names - the session store, the log and the trace directory - so a session
another process persisted is listed as `detached`, and it cannot be attached:
its target lives in that process's Chrome. A tab that navigates without a
BrowserNERD operation - the page redirecting itself, or a person using a
headful window - is reported by the next `browser-act` or `browser-observe` on
it as `tab_moved` ([declarative automation](declarative-browser-automation.md)).

## Opening a tab

Creating or forking a session opens the tab at `about:blank`, sets the
viewport, starts the event stream - every CDP domain enabled, every
subscription made, the page hooks registered for each new document - applies
any prepared state (a fork's cookies and storage), and only then navigates to
the requested URL. The page load is therefore captured from its first request
and runs with the prepared state ([browser fact model](browser-fact-model.md)).
Creation returns once the document is parsed (`readyState` interactive or
complete, bounded by `default_navigation_timeout`), so an operation that follows
in the same batch acts on a page whose handlers exist; the load event is not
awaited, since a page that long-polls never fires it.
A load that fails still leaves an open tab, and `page_load_failed` and
`net_loading_failed` say why. Attaching to an existing target starts the same
event stream against the page as it stands.

## Dialogs

A JavaScript `alert`, `confirm`, `prompt` or `beforeunload` is answered the
moment it opens, on its own event subscription, so no capture work can delay
the answer and no dialog is ever left blocking the renderer. The answer is the
one armed for that session - by `browser-act`'s `dialog` field or `dialog`
operation, consumed by one dialog - and otherwise `browser.dialog_policy`
(`dismiss`, the default, or `accept`). Dialogs are answered even when the
fact engine is off. Each one is recorded in the session's dialog history (the
last 50) and as `js_dialog`.

## Downloads

In a browser BrowserNERD launched, and in an incognito context it created,
downloads are allowed and saved to `browser.download_dir` (default
`<data root>/downloads`) under their GUID, and download progress events become
`download` facts on the session whose tab started the download. An attached
browser's default context keeps its own download behaviour.

## Live-development workflow

Agents can keep application, documentation, test-user, and diagnostic tabs
open concurrently. A second Chrome instance can represent a different user
profile or a clean end-to-end run without replacing the primary browser.
