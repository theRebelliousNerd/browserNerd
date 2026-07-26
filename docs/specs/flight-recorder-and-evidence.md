---
title: BrowserNERD Flight Recorder and Evidence
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing event capture, diagnostics, temporal reasoning, or exports
indexes: [browsernerd-corpus, diagnostics]
---

# BrowserNERD Flight Recorder and Evidence

Status: shipped.

BrowserNERD listens to CDP network, console, navigation, DOM, input, and UI
events, normalizes them as timestamped facts, and derives compact causal
conclusions. Common conclusions include failed and slow requests, error
chains, toast-after-API-failure, repeated user-visible errors, cascading
failures, and browser-to-container correlations.

Capture is asynchronous and bounded. Low-value, high-volume predicates can be
sampled. Request and response lifecycle pairs cannot be sampled independently.
The page event poller backs off on repeated errors and terminates when its
session closes.

Progressive responses expose evidence handles for targeted expansion. Raw
flight export is an explicit `browser-mangle` operation and remains inside
configured writable roots. Trace directories use private permissions and
trace files use owner-only permissions.

Facts, recorder calls, persisted sessions, exports, and returned MCP payloads
all pass credential redaction. Evidence is useful only when its provenance,
time scope, session, truncation, and infrastructure errors remain visible.
