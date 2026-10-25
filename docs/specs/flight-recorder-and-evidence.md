---
title: BrowserNERD Flight Recorder and Evidence
created: 2026-07-23
last_updated: 2026-10-23
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
cascading failures, and correlations with configured native or container logs.

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
key or submit), `since_navigation` (the page on screen: from the latest
navigation, moved back to the start of the document request that loaded it,
since that request starts before the navigation commits; default true for
`network`, false for every other topic), and `time_window_ms` (default 300000;
0 means all). Navigation scoping uses the current main document's loader and
latest matching Document request, including its initial redirect hop and a
long load whose request began more than 30 seconds before commit. A same-document route change starts at
that route event; a fetch, iframe or preceding reload cannot move its boundary.
When request context is unavailable, the latest matching request within
30 seconds and after the preceding navigation is the bounded evidence.

A diagnosis leads with its answer: `status`, a one-line `verdict` naming the
root cause, a `summary`, a `confidence`, and `next` - the one call worth
making next. `root_cause` carries the leading issue's evidence: the failed
`request` (method, URL, status, failure body), how many times it `repeats`,
and the `effects` it explains - each console error, error toast or alert with
its `after_ms` - plus `backend_logs` when the backend was reached. Every other
finding is one line in `issues`, with `more_issues` counting the rest.
`expansion_suggested` marks a confidence below 0.70 or contradicting
evidence.

Every valid diagnosis, in every view, carries `scope` and `scope_since_ms`.
The human-readable scope names the effective latest lower bound among the
requested action, navigation and time-window bounds. Window-only diagnoses
explicitly say session history; an unbounded read names all retained session
history. `scope_since_ms` is that actual epoch-millisecond lower bound, or
`0` for an unbounded read; no upper bound is implied. Issue verdicts begin
with the same scope, including blocking diagnoses. Clean verdicts also state
their scope. Topic defaults, failure counts, recovery and evidence selection
do not depend on how the scope is rendered.

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
- For the failed request, configured backend log lines that mention its
  path in that time window are attached. When no request failed but the
  leading issue is a page that never loaded or a WebSocket that failed, the
  logs are asked about that URL at that moment instead - the case where every
  browser signal reads clean and the reason sits only in a server's log, such
  as a tunnel answering Unauthorized. File and Docker sources share the
  [native log correlation contract](native-log-correlation.md). Missing,
  uncorrelatable, truncated and partially available evidence carries an explicit
  limitation; one failed source does not hide evidence from a healthy source.

The `network` topic answers for the page on screen: by default it reads only
the requests since that page began, so the 502s of a load during a backend
restart are not the groups of the reload that followed. `earlier` counts the
`requests` it left out and how many `failed`; `since_navigation: false`
includes them, and then each group's `before_navigation` counts its calls from
before the page on screen, as does the summary, and each expanded call carries
`before_navigation: true`. `scope` names the window read.

It groups requests by method and normalized URL - numeric, UUID and
opaque-token path segments collapse to `{id}`, query values become the
explicit `[OMITTED]` marker and their keys stay sorted and URL-escaped, and a
same-origin URL loses its origin. The marker states that grouping omitted
values, including empty values; it never claims the browser sent blank
parameters. Exact request URLs remain separate from display grouping.
A group's failure is
`recovered` when a later call to the exact same URL succeeded; a failure on
the same normalized URL with other parameters is not recovered by it. Groups
order open failures first (worst class first), then storms, then data calls
before page scaffolding, then recovered groups, then by count. A group with a
failure carries `latest`, the outcome of its most recent call, and its summary
line leads with that status and says `(recovered from <status>)`. `status` is
`error` while any group has an open failure and `warning` for recovered groups,
storms or WebSocket failures. A storm is at least 10 calls in any 30 s, or at
least 5 at a steady interval of 2 s or less.

Page scaffolding is a request for a static file by extension (an HTML document
excepted), a dev server's chunk, image, font or hot-update path
(`/_next/static/`, `/_next/image`, `/_next/webpack-hmr`, `/__nextjs`, `/@vite/`,
`/@fs/`, `/@react-refresh`, `/node_modules/`, `/static/`),
or the favicon; anything under an `api` or `graphql` path is a data call. In
the compact and summary views a scaffolding group with no failure and no storm
folds into one `assets` summary (`groups`, `requests`, `statuses`, `failed`):
a Next.js dev load is mostly chunks and fonts, and a list ordered by count
spent most of its rows on them and pushed data calls into `more_groups`.
`view: full` lists every group. WebSockets are grouped by URL, and three or
more connections in scope are a reconnect loop. `expand=<URL substring>`
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

Expansion labels and authorization are separate. A reason response's
`gate_grants` receipt reports whether its exact session/handle/reason bundle
was atomically issued, its actual ten-minute expiry and its authorized reason
names. On capacity, cancellation or unavailable-authority refusal, it reports
unavailable with a named reason while retaining honest diagnostic/expansion
evidence. Only the real reason producer issues grants. A bounded TTL ledger
preserves issued grants independently of Mangle retention; a raw handle fact,
including one written manually or emitted by another tool, is not authority.

## Export and persistence

Raw flight export is an explicit `browser-mangle` operation and remains inside
configured writable roots. Trace directories use private permissions and
trace files use owner-only permissions.

Facts, recorder calls, persisted sessions, exports, and returned MCP payloads
all pass credential redaction. Evidence is useful only when its provenance,
time scope, session, truncation, and infrastructure errors remain visible.
