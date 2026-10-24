---
title: BrowserNERD React and DOM Intelligence
created: 2026-07-23
last_updated: 2026-09-24
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing React extraction, DOM snapshots, refs, or UI bindings
indexes: [browsernerd-corpus, frontend-intelligence]
---

# BrowserNERD React and DOM Intelligence

Status: shipped with framework limits.

BrowserNERD extracts compact DOM structure and, when available, traverses
React Fiber to restore the component vocabulary lost in rendered HTML.
Component name, parentage, bounded props/state, and fiber-to-DOM mappings
are returned directly and may also be submitted as Mangle facts. The design follows the semantic-reification direction in
the architecture validation rather than treating HTML as the primary agent
interface.

Interactive discovery returns sparse semantic refs, labels, action types,
enabled/visible state, bounded geometry, and alternative selectors. A
per-session fingerprint registry supports re-identification and reports
property changes after DOM updates. Navigation clears stale identities.

Discovery reaches controls the page made clickable without a semantic role -
an `onclick` attribute, a React `onClick` prop, or the element's own
`cursor: pointer` - and reports them with type `clickable`. A `combobox` role
(the trigger of a custom select) is typed `combobox`, and an `option` or
`treeitem` role is typed `option`.

## Dead controls

A React root is the element carrying a `__reactContainer$` marker, or the page
itself when the marker sits on `document` or `<html>` (React 19 under the
Next.js App Router hydrates the whole document); a document-rooted page is
checked from `<body>` down.

On a page with a React root, every candidate is checked for React ownership:
an element React rendered carries a fiber or React props (`owned`). An element
without either is `foreign` when React rendered an element between it and the
root - a widget library (code editor, map, chart) building its own DOM inside a
React-rendered container, which works - and `dead` when nothing between it and
the root was rendered by React: server-rendered markup whose hydration failed,
so clicking it does nothing. A page that carries server-rendered React markup
(`#__next`, `__NEXT_DATA__`, `self.__next_f`, `data-reactroot`) but no React
root or fiber anywhere, once loaded and more than 3 s old, never ran its
JavaScript: every control on it is `unhydrated`.

A dead or unhydrated control (links and elements with an `onclick` attribute
excepted, since those work without React) is reported with `dead: true` on its
row, grouped apart from live controls with the same label, and summarized per
observation as `dead_controls: {count, labels}` (up to ten labels) and in the
observe summary line; an unhydrated page adds `dead_controls.page`, a sentence
naming the page-wide failure. Each one is also recorded as
`dead_control(SessionId, Ref, Label)` (`schemas/modules/75_page_structure.mg`),
and ranked action candidates drop dead controls, so neither `browser-observe`
recommendations nor `browser-reason` `next_best_action` offer a click that does
nothing. A page with neither a React root nor server-rendered React markup
reports no hydration state at all, so a static page never shows dead controls.

DOM and React inspection returns a bounded structured subtree directly to the
caller. `root_selector` selects exactly one element; `root_ref` resolves an
existing semantic ref. The two are mutually exclusive. Without a root the
scope is a document preorder prefix. Capture visits at most four times
`max_nodes` elements/fibers; React root discovery has its own equal scan bound
reported as `discovery_scanned`. Capture admits at most `max_nodes` rows and stops at
`max_depth`. Visibility filtering precedes row admission, with visited hidden
rows still consuming the scan budget. Child/sibling traversal never escapes
the selected subtree. Depth, scan, row, scalar-field and byte limits are
reported as named incompleteness rather than an empty or healthy verdict.

Row and parent IDs belong to one explicit `capture_id`; anonymous elements
cannot collide across scoped snapshots. `dom_id` records an element's own HTML
id separately. A filtered or response-truncated parent may be outside the
returned rows, with the corresponding filtering or incompleteness explicit.
DOM rows include bounded safe attributes, direct text, visible state and
non-sensitive scalar control value. React rows include component/host type,
parent identity, bounded own scalar props/state and function-presence markers
of shape `{type: "function"}`. A function marker reveals neither source,
closure contents nor invocation results. Non-scalar values and accessors are
not traversed or invoked. Missing function markers are not hydration evidence.
React root discovery is bounded; absent/inaccessible Fiber reports explicit
capability incompleteness while DOM automation remains usable.

Snapshot scope, captured/returned counts, traversal completeness and response
completeness remain visible in every view. Summary intentionally returns no
rows; compact/full are capped by the requested row and byte budgets. These
counts describe the returned capture, not the shared Mangle store. Optional
fact emission is disabled by default for explicit inspection and does not
claim retained facts. Automatic DOM fact sampling uses the same bounded,
redacted capture machinery. Console errors, requests, routes and explicit
actions retain higher priority in the engine. Sensitive input metadata and
credential keys are redacted before rows or facts are created; configured
credential environment values are removed before output and storage. Fixed
internal scripts receive JSON arguments; unsafe JavaScript settings and the
disclosure gate do not change.

React extraction is best-effort because production builds and future React
internals can hide or change Fiber structures. Failure to reify React must
leave DOM automation usable and return explicit capability status.

Deep extraction through `browser-observe` modes `react` and `dom_snapshot`
uses the same disclosure gate as the focused tools. Call `browser-reason`
first, then supply a recent `approved_by_handle` for that session or a
`gate_reason` currently authorized by its reasoning evidence. The accepted
reasons are `explicit_user_intent` (requires a handle), `low_confidence`,
`contradiction_detected`, and `no_matching_tool`. Handles and reason grants
expire and cannot authorize another session. All response views retain gate,
error and timeout evidence; absent extraction evidence is never rendered as
an empty tree or a zero-node snapshot.
