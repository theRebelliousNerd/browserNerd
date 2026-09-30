---
title: BrowserNERD Observation and Interaction
created: 2026-07-23
last_updated: 2026-09-30
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing navigation, element discovery, actions, screenshots, or JS
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Observation and Interaction

Status: shipped.

## browser-observe

`browser-observe` answers "what is on this page" in a few hundred tokens and
drills down by `mode`. Filters are validated against an allowlist and passed
into page evaluation as data, never interpolated as JavaScript source. Every
mode except `sessions` and `doctor` needs `session_id`. An `intent` preset
(`quick_status`, `find_actions`, `map_navigation`, `hidden_content`,
`deep_audit`, `check_sessions`, `visual_check`, `grid_hunt`) fills only the
knobs the caller left out, and the response echoes `intent` only when one was
given. `include_specs` defaults to false.

### composite (default)

One call returns:

- `data.state`: `url` and `title`, plus `loading` or `hasDialog` only when
  true.
- `data.content`: at most 300 characters of what the page says - up to three
  live-region lines (alerts, status lines, toasts) and four headings, with
  `more: "mode:text"` when headings were cut. Omitted at `view: summary`.
- `data.health`: counted from facts already recorded for the current page
  load - `failed_requests` (responses with status >= 400) with
  `recent_failures`, `console_errors`, and `dialogs`. Omitted when nothing
  went wrong. It is a digest; `browser-reason` explains causes.
- `data.interactive`: the controls, grouped. Controls sharing type, label and
  action collapse into one row `{ref, type, label, action, count, refs}`:
  `ref` is the first member and `refs` names the others as a numbered run
  (`button.browse_1..button.browse_399`) or the first three plus a count.
  `summary.total` is the true control count, `summary.groups` the distinct
  rows, `summary.returned` the rows this response carries (at most
  `max_items`, default 20), and `truncated: true` marks a cut. Links are rows
  with their `href`; the link-area map is `mode: nav`. Dead controls are
  reported as described in [React and DOM intelligence](react-dom-intelligence.md).
- `data.recommendations`: up to `max_recommendations` (default 3) ready
  `browser-act` calls `{tool, args, reason}`, ranked for the next decision -
  a primary control such as Save outranks a repeated row, and candidates
  offering the same choice yield one recommendation. Planning reads up to 60
  control groups however few rows are returned, so a primary control below
  the fold still reaches the plan. `view: full` adds every `action_candidates`
  row.
- `next_step`: the one call worth making next - `await_stable` while the page
  loads, `browser-reason` when diagnostics report an error or warning,
  otherwise the top recommendation.
- `summary`: one line naming the title, control and group counts, dead
  controls, links, and the health digest.

`include_diagnostics: true` adds the diagnose-page and error-toast views, and
`view: full` adds hidden content and every section untrimmed.

### Other modes

- `text` reads the page in reading order: title, the h1-h3 outline, landmark
  regions, live regions, and visible text blocks, spent from one `max_chars`
  budget (default 1500, at most 20000) in that order. Blocks are
  deduplicated, and blocks differing only in their digits ("Row 1: ...",
  "Row 2: ...") collapse into the first with a count. `text_query` keeps only
  blocks containing the term, matched against every block, and a long match
  returns the window around it. Text mode is the way to read a page; a
  screenshot is for layout.
- `interactive` returns the controls alone, grouped as in composite, with
  `filter` (`all`, `buttons`, `inputs`, `links`, `selects`) and
  `visible_only` (default true). A row's `type` is one of `button`, `input`,
  `link`, `select`, `combobox`, `option`, `tab`, `menuitem`, `checkbox`,
  `radio`, `row`, `listbox`, or `clickable` - an element the page made
  clickable through an `onclick` attribute, a React `onClick`, or its own
  `cursor: pointer`, with no semantic role.
- `screenshot` saves an image and returns only `{file_path, format,
  size_bytes, width, height}`. `annotate: true` draws numbered boxes on the
  controls in the viewport, at most `max_items` (default 30, cap 200), and
  lists them grouped with their refs. `ref` captures one element and
  `full_page: true` the whole scrollable page. The tab is brought to the front
  first, and activation plus capture are bounded at 20 s: a tab that cannot
  render fails with an error instead of holding the call.
- `storage` answers "why am I signed out" from the page's own state: cookies
  (name, size, flags, expiry) and `localStorage`/`sessionStorage` keys (size,
  kind), auth-looking names first, and for every JWT-shaped value - including
  one nested in a JSON state blob - its `exp` and `iat` as `expires_in` or
  `expired_ago`. An expired token turns the status to `warning`. No value,
  and no claim other than those two times, leaves the page. The sections are
  `data.jar` (cookies), `data.storage` (`local`, `session`) and `data.expiry`
  (token times): names that avoid the words the server's credential redactor
  matches, which blanks the whole value of a key naming a cookie or token.
- `layout` audits the page's spatial layout at its current size - or, with
  `viewports`, at each named size, restoring the session's own afterwards -
  and returns each overlap, text bleed or cut, horizontal overflow,
  unreachable or toolbar-covered item, squeezed column set, small tap target
  and permanently covered item with the elements' selectors and refs, their
  boxes, a CV verdict from the pixels, and an annotated screenshot. Contract:
  [responsive layout audit](responsive-layout-audit.md).
- `sessions` lists live tabs `{id, url, status, isolated}`, most recently
  active first, with `active_count` and `detached_count`; a tab a `viewport`
  operation sized also shows `viewport` (`390x844@3 mobile touch (phone)`).
  `include_detached: true` also lists detached records - sessions reloaded
  from an earlier server run with no tab behind them, which `attach-session`
  can bind again.
- `doctor` is BrowserNERD's self-diagnosis
  ([delivery and verification](delivery-and-verification.md)).
- `state`, `nav`, `hidden`, `grids`, `react` and `dom_snapshot` return one
  narrow slice each.

## browser-act

`browser-act` runs an ordered `operations` array against `session_id`. The
mechanics - batch validation, locators, time bounds, the effects digest - are
in [declarative automation](declarative-browser-automation.md).

Top-level arguments: `session_id`, `operations` (required), `stop_on_error`
(default true), `view` (default compact), `max_items` (compact result rows,
default 20), `settle_ms` (default 1500, cap 10000), `include_specs` (default
false), `spec_terms`.

| `type` | Fields |
|---|---|
| `navigate` | `url`, `wait_until` (`load`, `networkidle`, `none`), `timeout_ms` |
| `interact` | `action` (`click`, `type`, `select`, `toggle`, `clear`, `hover`), one locator, `value`, `submit`, `timeout_ms` |
| `fill` | `fields: [{<locator>, value}]`, `submit` (Enter) or `submit_button` (a ref or the button's visible text), `timeout_ms` |
| `upload` | `ref`, `files`, `timeout_ms` |
| `key` | `key` (`Enter`, `a`, `Control+a`, `Shift+Tab`, `Meta+k`), `modifiers`, `timeout_ms` |
| `history` | `action` (`back`, `forward`, `reload`), `timeout_ms` |
| `sleep` | `duration_ms`, at most 60000 |
| `dialog` | `accept`, `prompt_text` |
| `wait` | `predicate`, `match_args`, `timeout_ms`, `poll_interval_ms` |
| `await_stable` | `timeout_ms`, `network_idle_ms` |
| `await_fact` | `predicate`, `args`, `timeout_ms` |
| `await_conditions` | `conditions`, `timeout_ms` |
| `session_create` | `url`, `browser_id`, `isolated` |
| `session_attach` | `target_id`, `browser_id` |
| `session_focus`, `session_close` | `session_id` (default: the batch's session) |
| `session_fork` | `source_session_id` (default: the batch's session), `url` |
| `browser_launch` | `new_instance` (default true) |
| `browser_close` | `browser_id` |
| `js` | `script`, `element_ref`, `timeout_ms`, `gate_reason`, `approved_by_handle`, `result_mode` (`scalar`, `compact_json`, `raw`), `max_result_bytes` |
| `plan` | `actions`, `predicate`, `delay_ms` |
| `viewport` | `preset` (`phone`, `iphone-14`, `pixel-7`, `phone-small`, `tablet`, `laptop`, `desktop`), or `width`, `height`, `device_scale_factor`, `mobile`, `touch`, `orientation`, `toolbar_height`, `user_agent`; or `reset: true`. Sizes the batch's tab only ([responsive layout audit](responsive-layout-audit.md)) |

Every operation also accepts `dialog` (`accept` or `dismiss`) and
`prompt_text`. The types `click`, `type`, `select`, `toggle`, `clear` and
`hover` are shorthand for `interact` with that action, and `press` for `key`.

- `type` and `fill` empty the field through the element's native value
  setter, so a framework that tracks the value (React) sees the change, type
  the text as input, and read the value back; a field that does not end up
  holding the value fails the operation. A contenteditable region is written
  the same way.
- `select` sets a native `<select>` by option value or label, and opens a
  custom combobox (Radix and the like) and clicks the option; either way it
  verifies the control now shows the chosen option.
- `key` takes a chord: modifiers joined to the key with `+`.
- A `session_create`, `session_fork` or `session_attach` result carries the
  new `session_id`, and the operations after it in the same batch run against
  that session. `session_focus` moves the batch the same way.
- A JavaScript dialog is never left open, because an open dialog blocks the
  renderer and every later call on the tab. `dialog: "accept"` or `"dismiss"`
  on an operation answers the dialog that operation opens, with `prompt_text`
  for a prompt; a `{type: "dialog", accept}` operation arms the answer for the
  next dialog the tab opens. A dialog nobody armed is answered by
  `browser.dialog_policy` (default `dismiss`), and the op result and effects
  name every dialog and how it was answered.
- A tab in the background is brought to the front before input is sent to it.

The response carries `success`, `status` (`ok`, `error`, or `invalid` for a
batch rejected before running), a `summary` naming the first failure,
`counts` (`total`, `succeeded`, `failed`, and `skipped` when a stop left
operations unrun), `session_id` (the session the batch ended on),
`normalized` (each alias applied), `results`, `effects`, and an evidence
handle `act:<session>:<epoch ms>`. A compact result row carries `index`,
`type`, `success`, the `error` of a failed operation, and any of
`session_id`, `url`, `title`, `located`, `selected`, `verified_by`, `armed`,
`warning`, `note` and `dialogs` the operation produced. A rejected batch
returns `status: invalid`, the first problem as `error`, every problem in
`issues` (`{index, type, error}`), and every operation counted as skipped.
At `view: summary` the effects fold into the summary line.

## Uploads, screenshots and JavaScript

`upload` (`{type: "upload", ref, files: [paths]}`) sets the files of an
`<input type="file">` through CDP `DOM.setFileInputFiles`, as a person choosing
files in the picker does, so an upload flow is testable without script. The
ref must name a file input, more than one file needs its `multiple` attribute,
and every path passes the upload policy in the security spec first; relative
paths resolve against the workspace root. The op returns the file names it set
and emits `user_upload(SessionId, Ref, FileCount, Timestamp)`. Proven by
`mcp-server/internal/mcp/upload_files_live_test.go` against headless Chrome:
the page's own change handler reads the file's name and size.

Screenshots support PNG and JPEG accurately. Overlay drawing occurs on a PNG
working image and final encoding follows the requested format. Model-directed
save paths are confined to configured writable roots, including symlink
resolution.

Arbitrary JavaScript is disabled by default and, when a trusted operator
enables it, remains an explicitly gated diagnostic escape hatch. Routine
automation should prefer semantic refs and native operations because those
produce normalized facts, safer logs, and replayable tests.
