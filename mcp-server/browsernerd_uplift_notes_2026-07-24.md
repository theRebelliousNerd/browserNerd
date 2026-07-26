---
title: BrowserNERD uplift notes -- 2026-07-24
created: 2026-07-24
last_updated: 2026-07-24
doc_type: reference
subsystem: infra
read_when: Reference material for infra
indexes: []
---

# BrowserNERD uplift notes -- 2026-07-24

Notes taken while debugging a real production-shaped bug (Next.js dev app served
through a Cloudflare tunnel rendered correctly but never hydrated, so nothing on
the page was clickable). BrowserNERD itself was down for the diagnosis, so the
work was done with a generic screenshot/JS browser tool. That contrast is the
source of most of these observations: each item below is a place where the
generic tool cost real tokens or real wall-clock that BrowserNERD could have
saved.

Fixed this session (see commit "fix(browsernerd): anchor config paths..."):
config paths anchored to the config file rather than the process cwd, and
startup failures now reach stderr instead of vanishing.

---

## 1. Aggregate repeated network requests (highest token win observed)

`read_network_requests` on the broken page returned **248 rows**, of which the
diagnostically relevant content was three URLs refetched nine times each. Sixty
rows were printed at roughly 4,000 tokens; the actual signal was one sentence:

```
[turbopack]_..._hmr-client_ts_10z625~._.js   x9   200   (first 0.4s, last 11.2s)
```

A `get-network-requests` that returns **unique URL -> {count, statuses, first_ms,
last_ms, total_bytes}** by default, sorted by count descending, would have put
the answer in the first row instead of burying it across four screens. Add
`expand: <url>` for the rare case where individual timings matter.

The repeat-count IS the diagnosis for a whole class of bugs (retry storms,
reconnect loops, render loops, N+1 fetches). Today that signal is only visible
by eyeballing duplicates in a long list -- exactly what an agent is worst at.

## 2. Make hydration a first-class predicate

The entire investigation reduced to one question: *did React attach to this DOM
node?* Answering it took six round trips of hand-written JS Fiber probing, and I
got it **wrong** on the first pass -- `__REACT_DEVTOOLS_GLOBAL_HOOK__.renderers.size`
was 3, which I read as "hydrated" when it was counting extension renderers. The
reliable test is per-element:

```js
Object.keys(el).filter(k => k.startsWith('__react'))  // [] => never hydrated
```

BrowserNERD already does React Fiber reification, so it should expose this
directly -- e.g. `react_hydrated(Ref)` / `hydration_gap(Ref)` as Mangle facts,
and a `hydrated` boolean on every element returned by `get-interactive-elements`.

An element that is visible, enabled, and has no onClick handler attached is a
**dead control**, and that is invisible to screenshots, to the accessibility
tree, and to console logs. It is arguably the single most under-served failure
mode in frontend debugging, and BrowserNERD is already holding the data needed
to report it. A `find-dead-controls` tool returning "10 buttons rendered, 0
hydrated" would have ended this session's investigation in one call.

## 3. Surface origin-side errors, not just browser-side

Every browser-visible signal was clean: HTTP 200 on all 248 requests, zero
console errors, correct DOM, correct CSS. The truth existed only in the reverse
proxy's log:

```
malformed HTTP response "Unauthorized"  originService=http://cross-thread-frontend:9003
```

BrowserNERD already has `docker.containers` wired into the workspace config. The
uplift is to correlate on **failure**: when a WebSocket upgrade fails, when a
request stalls, or when a page loads without hydrating, automatically pull the
matching window from those container logs and include it. The existing
`log_window: 30s` config is exactly the right primitive -- it just needs to fire
on these conditions rather than only on explicit request.

## 4. Report WebSocket lifecycle

Nothing in the browser tooling reported "the HMR WebSocket never connected."
Failed and retrying WS handshakes are invisible in a request list that only
counts HTTP. Given that HMR, live reload, agent chat, and hygiene scans in this
codebase all ride WebSockets, `websocket_connect_failed(Url, Reason)` and
`websocket_retry_count(Url, N)` would be high-value facts.

## 5. Startup diagnosability (partly fixed)

Fixed: config-path anchoring and stderr reporting of fatal errors. Still worth
doing:

- **Self-check subcommand.** `browsernerd.exe -doctor -config X` that prints
  resolved config, verifies every path exists and is writable, verifies the
  Mangle schema parses, and exits nonzero with a summary. The failure here was
  a missing file whose path was silently computed from the wrong base -- a
  doctor command names that in one line.
- **Version stamping.** The shipped `bin/browsernerd.exe` predated its own
  source by months; it was missing the `-trust-workspace-config` flag that
  `main.go` defines. `-version` reporting a build stamp, and a CI check that
  the committed binary matches source, would catch that drift.
- The `bin/` directory contains `browsernerd.exe`, `browsernerd.exe~`, and
  `server.exe` -- three binaries of different vintages with no indication which
  is canonical. Ship one.

## 6. Progressive disclosure is right; extend it to results

`progressive_only: true` keeping the tool surface at 11 is a good call. The same
discipline should apply to tool *output*: default to a summary with an explicit
`detail` or `expand` parameter, rather than returning full lists and trusting
the caller to cope. Item 1 is the concrete instance, but the pattern generalizes
to DOM snapshots and console dumps.

## 7. Config ergonomics

- `mcp-server/config.yaml` uses Windows-style relative paths (`data\\...`). Now
  that they anchor to the config file this is safe, but the example config
  should state the anchoring rule explicitly so authors stop guessing.
- The workspace template comments `schema_path: ".browsernerd/schemas/project.mg"`
  while `DefaultConfig` uses `schemas/browser.mg`. Two different conventions for
  the same field is what produced the resolution bug. Worth unifying.
