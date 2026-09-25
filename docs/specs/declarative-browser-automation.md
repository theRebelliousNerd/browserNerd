---
title: BrowserNERD Declarative Browser Automation
created: 2026-07-23
last_updated: 2026-09-24
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing observe, act, waits, event correlation, or state deltas
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Declarative Browser Automation

Browser automation synchronizes on meaningful state rather than on sleep
durations or one selector at a time. The fields and response shapes of
`browser-observe` and `browser-act` are in
[observation and interaction](browser-observation-and-interaction.md); this
spec is how acting works.

## A batch is validated before it runs

`browser-act` checks the whole `operations` array before running any of it.
Every operation type declares the fields it accepts, their kinds and allowed
values, and which are required (`internal/mcp/act_ops.go`). An unknown type or
field, a value of the wrong kind or outside its range, a missing locator, or
two locators on one element rejects the batch with `status: invalid` and one
`issues` entry per problem that names the fix: the closest accepted name, the
operation a stray field belongs to, or - for `selector`, `css` and `xpath` -
the locators to use instead. `timeout_ms` must be 1 to 120000; `sleep` is
capped at 60000 ms and the refusal points at the await operations;
`prompt_text` needs `dialog: "accept"`. `value_env` is refused, because only
`browser-test` resolves it.

Natural shorthand is normalized instead of refused, and each rewrite is listed
under `normalized`: `click`, `type`, `select`, `toggle`, `clear` and `hover` as
operation types become `interact` with that action; `press` becomes `key`; a
sleep's `ms`, `milliseconds` or `duration` becomes `duration_ms` and `seconds`
is scaled to it; `timeout` becomes `timeout_ms` on any operation; a fork's
`session_id` becomes `source_session_id`.

`browser-test` runs a fixture's operations through the same validation before
its first step ([token-efficient testing](token-efficient-testing.md)).

## Finding an element

An operation that targets an element takes exactly one locator:

- `ref`: an element reference from `browser-observe`.
- `text`: the element's visible text or accessible name.
- `label`: a form control by its label - `aria-labelledby`, `aria-label`, an
  associated `<label>`, then its placeholder.
- `role` with `name`: an ARIA role, explicit or implicit (`button`, `link`,
  `textbox`, `combobox`, `checkbox`, ...), and its accessible name.
- `x` and `y`: a viewport point, for `click` and `hover` only.

Text, label and role matching searches the document and open shadow roots,
keeps visible elements, then ranks exact matches over case-insensitive ones
over contains matches among them (`exact: true` keeps only exact ones) - so a
hidden exact match (a mobile-only "Search" button) never hides the visible
"Search Ctrl K" a person would click - prefers enabled
and interactive ones, and collapses an element with the ancestors that merely
wrap its text. For `type`, `select`, `toggle` and `fill`, `text` matches form
controls only. When several elements remain the operation fails, listing the
first candidates, unless `index` (0-based) picks one; the result's `located`
names the element that was found. `fill` accepts the same locators per field,
point excepted.

Element references come from one in-page generator (`elementRefScript`) that
every ref-issuing observation - interactive elements and screenshot overlays -
and the resolver evaluate. A ref is a pure function of the DOM: a stable
identifier when the element has one (`testid:`, `aria:`, id, name, `row:`,
`rowkey:`), otherwise tag plus two classes, with duplicates numbered over every
candidate in document order before any filter, limit or visibility option
applies. So an element carries the same ref in every observation. The
candidates include elements the page made clickable without a semantic role -
an `onclick` attribute, a React `onClick`, or their own `cursor: pointer` -
reported with type `clickable`. Such an element is a candidate only outside
every semantic control, so the icon inside a button never becomes a second
target. come from one in-page generator (`elementRefScript`) that
every ref-issuing observation - interactive elements and screenshot overlays -
and the resolver evaluate. A ref is a pure function of the DOM: a stable
identifier when the element has one (`testid:`, `aria:`, id, name, `row:`,
`rowkey:`), otherwise tag plus two classes, with duplicates numbered over every
candidate in document order before any filter, limit or visibility option
applies. So an element carries the same ref in every observation.

The same script names each element for display (`__bnAccessibleLabel`) the way
assistive technology does: `aria-labelledby`, then `aria-label`, then the
`<label>` elements associated with it, then its own text, and only then
`title`, placeholder and `alt`, with whitespace collapsed and a 50-character
cap. A form control is reported by its label, not by a placeholder example, and
a radio rendered as a button is reported by the label that names it.
`internal/mcp/element_label_integration_test.go` pins this for interactive
elements and screenshot overlays.

Resolution re-runs the generator and takes the element that carries the ref
now. The fingerprint registered for that exact ref guards against a shifted
DOM: a positionally named element must still show the same tag and text, while
an element named by a stable attribute keeps its identity when a toggle
relabels it. When the ref no longer names anything, fallback strategies match
by fingerprint only; a duplicate suffix is never applied as an index into a
different selector's matches, because that clicks whatever sits at that
position. `internal/mcp/element_ref_roundtrip_integration_test.go` pins all of
this against a live browser.

A click scrolls the element to the middle of the viewport and dispatches at a
point that hits the element itself; when something else sits on top of it -
a sticky header, an open dialog's overlay - the click fails naming what
covers it instead of landing on that element. A tab in the background is
brought to the front before any input reaches it.

## Every operation is bounded

Each operation runs under its own deadline and returns when the deadline
passes, even when the browser call inside it is stuck: navigation and history
get `timeout_ms` (default 30 s), the waits and awaits `timeout_ms` (default
30 s), `js` `timeout_ms` (default 5 s), each plus 2 s of slack; `sleep` its
duration; session and browser lifecycle one minute; `plan` two minutes; any
other operation `timeout_ms` or 30 s, within which a click dispatch and a key
chord each get 5 s. The batch is bounded at five minutes and the call at the
server deadline ([delivery and verification](delivery-and-verification.md)).
An operation that runs out of time fails with an error naming the usual
cause, a JavaScript dialog or a busy renderer, and with `stop_on_error` the
operations after it are counted as `skipped`.

## Waiting on state

An agent waits for route, network, console, component, toast and stable-state
conditions together (`await_conditions`), for one fact (`await_fact`), or for a
predicate to match (`wait`). Subscriptions establish a baseline when created;
historical facts do not satisfy a fresh wait. `await_stable` returns when no
request has started for `network_idle_ms` (default 500) and the DOM has not
changed for 200 ms; on a page that polls it never settles, and it returns at
`timeout_ms` (default 10000) with a warning that the page may poll instead of
failing the batch.

Since-based views are strict: evidence without a usable timestamp is excluded.
Ordinary health views may preserve useful facts that lack timestamped
predicate variants.

## What the batch caused

After a batch that touched a page (`navigate`, `interact`, `fill`, `upload`,
`key`, `history`, `js`, `plan`, `session_create`, `session_fork`),
`browser-act` waits up to `settle_ms` (default 1500, cap 10000) for the
requests the batch started to be answered, its downloads to finish, and their
console output and toasts to land, then reports an `effects` digest read from
the facts recorded since the batch began, in every session it touched, plus a
before-and-after read of the page's alert, status and toast regions. Only keys
with something in them appear:

| Key | Holds |
|---|---|
| `url_changed` | `{from, to}` |
| `title` | the new title, when it changed |
| `toasts` | error, warning and success toast facts, and the alert, status and toast regions newly visible when the batch settled; an info-level toast fact is left to that live-region read, which shows what the page settled on rather than a passing "loading" |
| `errors` | console errors and failed assertions |
| `failed_requests` | `METHOD /path STATUS`, with a snippet of the failure body when one was captured; a 4xx on a page asset (the resource types `failed_request` treats as assets) is left out |
| `pending` | requests still unanswered after the settle window |
| `dialogs` | each JavaScript dialog, its message, and whether it was accepted or dismissed |
| `downloads` | each download's file name and final state |
| `ws_failures` | WebSocket errors and abnormal closes |
| `load_failed` | navigations that failed to load, with the network error |

A URL on the page's own origin is shown as its path, including in a batch
that opened its tab. Each list holds at most five entries plus `+N more`,
repeats fold into `(xN)`, and every line is clipped to 120 characters. The digest answers "what
did that click do" inside the act result, so an agent does not spend an
observe or reason call to learn that its save produced a 500, a console error
and an error toast. The facts it reads are described in the
[browser fact model](browser-fact-model.md).

Request and response facts are not throttled independently, pollers use bounded
backoff, and list and query responses have explicit limits.
