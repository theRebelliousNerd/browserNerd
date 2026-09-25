---
title: BrowserNERD Flight Recorder and Evidence
created: 2026-07-23
last_updated: 2026-09-24
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing event capture, diagnostics, temporal reasoning, or exports
indexes: [browsernerd-corpus, diagnostics]
---

# BrowserNERD Flight Recorder and Evidence

Status: shipped.

## Capture

BrowserNERD listens to CDP network, console, navigation, DOM, input, dialog,
download and UI events from a tab's first request, normalizes them as
timestamped facts ([browser fact model](browser-fact-model.md)), and derives
compact causal conclusions. Common conclusions include failed and slow
requests, error chains, toast-after-API-failure, repeated user-visible errors,
cascading failures, and browser-to-container correlations.

Capture is asynchronous and bounded. Low-value, high-volume predicates can be
sampled, and a full buffer sheds low-signal facts before failure evidence
([Mangle reasoning architecture](mangle-reasoning-architecture.md)). Request
and response lifecycle pairs cannot be sampled or evicted independently. The
page event poller backs off on repeated errors and terminates when its session
closes.

## browser-reason: a diagnosis

`browser-reason` reads what the browser recorded for one session and answers
one question per `topic`:

| `topic` | Answers |
|---|---|
| `why_failed` | What broke, or what did my click break |
| `health` (default) | Is anything wrong: a scorecard of 5xx, 4xx and network failures, console errors and warnings, error toasts, storms, WebSocket failures, dialogs, page load failures, slow calls and blocking issues |
| `network` | Is something looping, which calls fail |
| `blocking_issue` | Why the page is empty or stuck: a load failure, a dismissed dialog, an overlay, a login wall, a failed data call |
| `next_best_action` | What can be done here: deduplicated action candidates as ready `browser-act` operations |
| `what_changed_since` | The issues inside `time_window_ms`, newest first |

An unknown topic is an error listing the topics. `intent` presets map
`triage` to `health`, `act_now` to `next_best_action`, `debug_failure` to
`why_failed` at `view: full`, and `unblock` to `blocking_issue`. Scope narrows
with `since_action: true` (evidence since the session's last click, input,
key or submit), `since_navigation: true` (the current route), and
`time_window_ms` (default 300000; 0 means all).

A diagnosis leads with its answer: `status`, a one-line `verdict` naming the
root cause, a `summary`, a `confidence`, and `next` - the one call worth
making next. `root_cause` carries the leading issue's evidence: the failed
`request` (method, URL, status, failure body), how many times it `repeats`,
and the `effects` it explains - each console error, error toast or alert with
its `after_ms` - plus `backend_logs` when the backend was reached. Every other
finding is one line in `issues`, with `more_issues` counting the rest.
`expansion_suggested` marks a confidence below 0.70 or contradicting
evidence.

- A console error is linked to the failed request whose outcome came up to
  2 s before it, and a toast or alert up to 5 s after; an effect that names
  the request's status or path links up to 10 s after it.
- A 5xx always counts; a 4xx counts unless its CDP resource type marks a
  page asset (`Image`, `Font`, `Stylesheet`, `Media`, `Manifest`,
  `TextTrack`, `Other` - the line `failed_request` and browser-act's effects
  draw), judged by URL only when no resource type was recorded; a network
  failure counts unless it is a media asset.
- A dismissed dialog, a page load failure (with the recovery it needs), and a
  WebSocket error or reconnect loop are findings of their own.
- For the failed request, the backend container log lines that mention its
  path in that time window are attached. When no request failed but the
  leading issue is a page that never loaded or a WebSocket that failed, the
  logs are asked about that URL at that moment instead - the case where every
  browser signal reads clean and the reason sits only in a server's log, such
  as a tunnel answering Unauthorized. When Docker cannot answer, one short
  `unavailable: <why>` note stands in for the lines.

The `network` topic groups requests by method and normalized URL - numeric,
UUID and opaque-token path segments collapse to `{id}`, query values drop and
their keys stay, and a same-origin URL loses its origin - failures first, then
storms, then by count. A storm is at least 10 calls in any 30 s, or at least 5
at a steady interval of 2 s or less. WebSockets are grouped by URL, and three
or more connections in scope are a reconnect loop. `expand=<URL substring>`
lists the matching groups and their individual calls. With `expand`,
`body: true` fetches the latest matching responses - up to three - from
Chrome's network buffer and returns each one's `body_shape`: a JSON body's
keys (at most 12 per object, three levels deep), each array as its length and
the shape of its first element, and scalars clipped and redacted, so an empty
list reads as `items: array[0]` without paying for the payload; a text body
returns its first 300 characters. A body Chrome has already evicted returns
`body_unavailable` saying to repeat the action and expand again right after.

## Evidence handles

Progressive responses expose evidence handles for targeted expansion:
`observe:<session>:<section>`, `act:<session>:<epoch ms>`,
`test:<session>:<epoch ms>`, `audit:<session>:<section>`, and for
`browser-reason` `reason:<section>` for each evidence section that has rows -
`page_load_failures`, `failed_requests`, `console`, `toasts`, `dialogs`,
`websockets`, `storms`, `slow_apis`, `root_causes`, `blocking_issues`,
`contradictions`, `network`. Passing handles, or bare section names, in
`expand_handles` returns those sections' rows in `data`, along with `issues`
and `backend_logs` when asked for.

## Export and persistence

Raw flight export is an explicit `browser-mangle` operation and remains inside
configured writable roots. Trace directories use private permissions and
trace files use owner-only permissions.

Facts, recorder calls, persisted sessions, exports, and returned MCP payloads
all pass credential redaction. Evidence is useful only when its provenance,
time scope, session, truncation, and infrastructure errors remain visible.
