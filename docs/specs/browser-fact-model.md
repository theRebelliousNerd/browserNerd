---
title: BrowserNERD Browser Fact Model
created: 2026-07-23
last_updated: 2026-09-30
doc_type: data-spec
subsystem: browsernerd
read_when: Ingesting browser events or authoring Mangle rules
indexes: [browsernerd-corpus, mangle, browser-state]
---

# BrowserNERD Browser Fact Model

Status: shipped.

BrowserNERD normalizes Chrome and tool activity into session-scoped Mangle
facts. The declarations, with argument names, live in
`mcp-server/schemas/modules/` ([schema modules](mangle-schema-modules.md));
`browser-mangle` `operation: schema` lists them from the running engine with
each predicate's module and fact count. Core domains:

- Page and DOM: `current_url`, `navigation_event`, `page_load_failed`,
  `dom_node`, `dom_attr`, `dom_text`, `dom_layout`, and `dom_updated`.
- React: `react_component`, `react_prop`, `react_state`, and `dom_mapping`.
- Network: `net_request`, `net_response`, `net_header`,
  `net_correlation_key`, `request_initiator`, the failure facts
  `net_http_error`, `net_loading_failed` and `net_failure_body`, and the
  WebSocket lifecycle `ws_event`.
- Console and browser: `console_event` and `browser_log`.
- User and UI events: `toast_notification` (with `error_toast` and
  `warning_toast`), `click_event`, `input_event`, `state_change`, `js_dialog`,
  `download`, `dead_control`, and interaction facts.
- Automation and evidence: form, plan, screenshot, audit, repo-trace,
  disclosure, and JS-gate facts.
- Responsive layout: `viewport_emulated` and `layout_finding`, with the
  derived `layout_defect` for high findings
  ([responsive layout audit](responsive-layout-audit.md)).

Every browser-originated fact that can overlap across tabs carries
`SessionId`. Request and response facts preserve the same CDP request id; load
shedding must never independently discard one side of the pair.

## Capture facts

| Fact | Meaning |
|---|---|
| `net_response(SessionId, Id, Status, Latency, Duration)` | Whole milliseconds: `Latency` is the server's wait (request sent to first byte), `Duration` is request issued to response headers received, queueing and connection setup included |
| `net_http_error(SessionId, Id, Url, Status, ResourceType, Timestamp)` | A response with status >= 400, stamped when it arrived; `ResourceType` is the CDP type (`Document`, `XHR`, `Fetch`, `Script`, `Image`, ...) |
| `net_loading_failed(SessionId, Id, ErrorText, Canceled, Timestamp)` | A request that got no response - refused connection, DNS, CORS, abort; `Canceled` is `"true"` for an aborted request |
| `net_failure_body(SessionId, Id, Snippet)` | The first KB of a failed (>= 400) text or JSON response body, redacted |
| `ws_event(SessionId, WsId, Url, Event, Detail, Timestamp)` | WebSocket lifecycle: `created`, `handshake`, `open`, `closed`, `error`. Frame payloads are never captured |
| `browser_log(SessionId, Source, Level, Text, Url, Timestamp)` | Browser-generated warnings and errors - deprecation, intervention, security, CSP - and network messages no request fact carries, which is how a refused WebSocket handshake reports itself |
| `js_dialog(SessionId, Type, Message, Accepted, HandledBy, Timestamp)` | An `alert`, `confirm`, `prompt` or `beforeunload`, answered the moment it opened; `HandledBy` is `armed` or `policy` |
| `download(SessionId, Guid, Url, SuggestedName, State, Timestamp)` | `State` is `started`, `completed` or `canceled` |
| `page_load_failed(SessionId, Url, ErrorText, Timestamp)` | The main frame could not load `Url`: a network error, an HTTP >= 400 document, or Chrome's error page committed in its place |
| `dead_control(SessionId, Ref, Label)` | A control React never attached to ([React and DOM intelligence](react-dom-intelligence.md)) |

Capture begins before the page does. A tab that BrowserNERD creates or forks
opens at `about:blank`; every CDP domain is enabled, every subscription made,
and the page hooks registered to run in each new document before the page's
own scripts; only then does the tab navigate. The document request, the
load-time fetches, the first scripts' console output and the first WebSocket
connections therefore all become facts. An attached tab gets the same
subscriptions, and the hooks run once in its loaded document.

- Only a main-frame navigation moves `current_url` and emits
  `navigation_event`; an iframe loading an ad or widget does not. A
  same-document navigation - `pushState`, `replaceState`, a hash change - is
  a `navigation_event`, so a single-page app's route changes are seen.
- An uncaught exception or unhandled promise rejection becomes a
  `console_event` at level `error`, as DevTools shows it.
- Console errors and warnings are never throttled; `event_throttle_ms`
  applies to other console output and to DOM events.
- Toasts are seen in `role=alert`, `role=status` and live regions and in the
  toast containers of common UI libraries, including when a page rewrites the
  text of a region that is already on screen. A router's navigation
  announcement is not a toast: text inside a route announcer
  (`next-route-announcer`, `#__next-route-announcer__`, `#gatsby-announcer`,
  `[data-route-announcer]`) or equal to the page title is left out, here and
  in browser-act's effects.
- A toast's text is read with `innerText`, whitespace collapsed, so a title
  and description in separate blocks stay apart ("Login failed Invalid
  credentials"). For five seconds a notification is one toast: a re-render, or
  a mutation that reports only its title or only its description (a prefix or
  suffix of at least six characters, compared without whitespace or case), is
  not a second `toast_notification`. A toast whose text grows is reported
  again with the fuller text.

Successful structured navigation always emits `current_url` and
`navigation_event`, so declarative assertions do not depend on CDP event
timing; a navigate to the URL the tab is on is a reload. The engine keeps only
the latest `current_url` per session.

## Derived failure facts

| Fact | Holds when |
|---|---|
| `failed_request(SessionId, RequestId, Url, Status)` | A 5xx, or a 4xx that is not a page asset's. `asset_http_error(SessionId, RequestId)` marks a 4xx whose resource type is `Image`, `Font`, `Stylesheet`, `Media`, `Manifest`, `TextTrack` or `Other` |
| `failed_request_at(..., RequestTimestamp)` and `failed_request_done(SessionId, RequestId, Url, Status, DoneTimestamp)` | The same failure with the time the request left and the time its response arrived |
| `network_failure(SessionId, RequestId, Url, ErrorText, Timestamp)` | A request that got no response and was not canceled |
| `caused_by(SessionId, ConsoleMessage, RequestId)` | A console error linked to the nearest failure whose response (or network error) came within the 2 s before it; `error_chain` carries it on to the URL and status |
| `toast_after_api_failure(SessionId, ToastText, RequestId, Url, Status, TimeDelta)` | An error toast linked to the nearest failed request within 5 s of it |
| `slow_api(SessionId, RequestId, Url, Duration)` | A response whose `Duration` exceeds 1000 ms |
| `click_triggered_navigation(SessionId, Ref, ToUrl, TimeDelta)` | A navigation within 5 s after a click on `Ref` |

Facts have both logical timestamp arguments where required by a rule and the
Go `Fact.Timestamp` used for temporal windows. The engine holds them in a
bounded buffer over an indexed store, mirrors into an `mt_` temporal
predicate only the base predicates the loaded program reads temporally, and
sheds facts by signal class when the buffer is full
([Mangle reasoning architecture](mangle-reasoning-architecture.md)).

Credential-bearing header and input values are replaced before they reach the
engine. Password, token, cookie, authorization, payment-secret, and
one-time-code values must never become queryable plaintext facts.
