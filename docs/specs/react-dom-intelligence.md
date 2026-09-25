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
become Mangle facts. The design follows the semantic-reification direction in
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

DOM and React payloads are bounded and sampled as lower-value high-volume
facts. Console errors, request/response pairs, route changes, and explicit
actions retain higher priority. Password and other sensitive input values are
redacted in page JavaScript, in Go ingestion, in Mangle facts, and again at
the MCP response boundary.

React extraction is best-effort because production builds and future React
internals can hide or change Fiber structures. Failure to reify React must
leave DOM automation usable and return explicit capability status.
