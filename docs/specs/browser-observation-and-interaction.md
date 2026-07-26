---
title: BrowserNERD Observation and Interaction
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing navigation, element discovery, actions, screenshots, or JS
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Observation and Interaction

Status: shipped.

Browser observation includes sparse page state, navigation links, interactive
elements, hidden content, virtualized grids, screenshots, React structure,
DOM snapshots, session inventory, and composite views. Filters are validated
against an allowlist and passed into page evaluation as data, never
interpolated as JavaScript source.

`browser-act` executes ordered operations for navigation, interaction, form
fill, keyboard input, history, waits, stable-state detection, session/browser
lifecycle, approved JavaScript, and Mangle-derived plans. It returns per-step
success in compact or full views and can attach relevant configured specs
after the action.

Screenshots support PNG and JPEG accurately. Overlay drawing occurs on a PNG
working image and final encoding follows the requested format. Model-directed
save paths are confined to configured writable roots, including symlink
resolution.

Arbitrary JavaScript is disabled by default and, when a trusted operator
enables it, remains an explicitly gated diagnostic escape hatch. Routine
automation should prefer semantic refs and native operations because those
produce normalized facts, safer logs, and replayable tests.
