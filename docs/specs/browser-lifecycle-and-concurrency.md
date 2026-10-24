---
title: BrowserNERD Browser Lifecycle and Concurrency
created: 2026-07-23
last_updated: 2026-10-22
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

Connection health is a bounded `Browser.getVersion` CDP probe, not the
presence of a retained browser pointer. The default instance is reported as
reachable, unreachable (confirmed closed transport), unverified (timeout,
cancellation or another probe failure), or not_started. A probe never closes
a browser, changes focus or replays an operation. Metadata inventories describe
recorded ownership; they do not substitute for a fresh transport assessment.

Idempotent launch verifies this health. A reachable default is reused. Launch
may retire and replace only its exact confirmed-closed default when that
instance is managed, its temporary profile is marked as belonging to this
server, its recorded process is proved exited and it has zero tracked open
contexts. PID liveness uncertainty, a live/reused PID, unknown ownership,
attached browsers and remaining contexts refuse automatic recovery with a
named reason. An ambiguous timeout never authorizes cleanup. Start calls are
serialized. Retirement sends no signal to the exited PID and preserves every
other instance/context; a healthy remaining instance can become the default.
An explicit browser close remains a separate caller-controlled action.

Closing the last isolated context must permit another fresh isolated context
while the managed transport remains live. This is verified with headed private
displays; no permanent QA tab, user-profile anchor or speculative keepalive
flag is required by the lifecycle contract.

`max_tabs`, `max_browsers`, and optional `idle_tab_timeout` prevent unbounded
resource growth. `max_tabs` counts OPEN tabs - sessions holding a page. A
detached session (reloaded from the session store after a restart, or left
when its browser went away) owns no tab and never counts toward the limit
(`internal/browser/browser_instances.go` `openTabCountLocked`). Pollers back off and terminate after repeated failure.
Shutdown detaches resources under the manager lock, closes pages and browsers outside
the lock, terminates launched Chrome process trees, and removes their temporary
user-data directories on all exit paths (MCP client disconnect, stdin EOF, signals,
and errors). An attached browser (`debugger_url`) is never closed: its tabs we
opened are, the browser keeps running. Each launched profile carries a
`browsernerd-owner` JSON record containing the server PID and its process scope.
On Linux the scope is the PID namespace identity; a PID from another namespace
cannot establish whether that server lives. On startup the server reaps only
profiles with a qualified owner in its own process scope whose process is
confirmed absent. Permission denial and other uncertain probes preserve the
tree. Missing, malformed, integer-only and foreign-scope ownership marks grant
no cleanup authority. The parent process proves nothing on Linux, where init
or a subreaper can adopt orphans. Confirmed orphan cleanup removes its temporary
profile and reports the reaped count in `browser-observe mode=doctor`.

CLI `-doctor` and MCP Doctor are read-only assessments. They never reap orphan
trees, delete profiles, or change browser lifecycle.

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

## Background automation

Tabs are created with `Target.createTarget background: true`. Creation and
attachment enable `Emulation.setFocusEmulationEnabled` on that target before
automation begins. The renderer can receive input and produce frames while
its desktop window is initially in the background. Failure to enable focus
emulation fails the operation. Managed Chrome starts without a startup window;
new windows are created inactive. Ordinary operations never explicitly activate,
minimize or restore windows. Chromium's native pointer handling can still focus
a headed window, so non-interrupting headed automation uses display isolation.
Only explicit `session_focus` restores a minimized window and activates its tab.

## Isolated headed displays

`browser.isolated_display: true` runs each managed headed Chrome on a private
display. The Linux backend requires `Xvfb` and `openbox` on PATH. It creates a
fresh high-numbered X display, proves its X lock belongs to the process it
started and that its Unix socket exists, then starts its own window manager.
Only those child processes and Chrome receive the private display environment;
the server's ambient `DISPLAY`, Wayland and desktop environment are unchanged.
Chrome is forced to X11 on that private display and retains native trusted
mouse/keyboard input and headed screenshot fidelity.

Isolation is optional and defaults to false. Requested isolation fails clearly
on unsupported platforms, with headless mode, with an attached browser, or when
the backend cannot start. It never falls back to the user's desktop or to
headless Chrome. Extra browser instances receive separate displays. Failure
and shutdown close/kill Chrome before stopping its window manager and X server.
Browser/session inventories expose `isolated_display`; explicit focus reports
that it acts inside the private display, without bringing a window onto the
user's desktop. Machine profiles select this setting; it is not app-specific.
CLI and MCP Doctor report the requested `config.isolated_display` setting and
`config.display_backend` capability, availability and missing dependency names
before launch. A requested but unavailable backend is a reported problem.

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

The dialog subscription registers a pending report before it sends the answer
to Chrome. Publishing the history record completes that report. An action can
therefore observe that its dialog answer is still being recorded even when
Chrome has already resumed the renderer and completed the input command.

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
