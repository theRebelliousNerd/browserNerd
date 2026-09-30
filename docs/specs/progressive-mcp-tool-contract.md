---
title: BrowserNERD Progressive MCP Tool Contract
created: 2026-07-23
last_updated: 2026-09-30
doc_type: interface-spec
subsystem: browsernerd
read_when: Changing tool registration, schemas, response views, or evidence handles
indexes: [browsernerd-corpus, mcp]
---

# BrowserNERD Progressive MCP Tool Contract

Status: shipped.

The default MCP surface contains 11 tools: `launch-browser`,
`shutdown-browser`, `close-session`, `browser-observe`, `browser-act`,
`browser-reason`, `browser-audit`, `browser-mangle`, `browser-test`,
`get-specs`, and `check-specs`. Setting `mcp.progressive_only: false` adds the
37 focused lower-level tools, 48 in all. The progressive tools delegate to the
focused ones, so both surfaces run one implementation.

## Roles

- `browser-observe` sees a page, small by default, and drills down by `mode`:
  `composite` (default), `text`, `interactive`, `screenshot`, `layout`,
  `storage`, `sessions`, `doctor`, and the narrow slices `state`, `nav`,
  `hidden`, `grids`, `react` and `dom_snapshot`. Contract:
  [observation and interaction](browser-observation-and-interaction.md);
  `layout` and the `browser-act` `viewport` operation:
  [responsive layout audit](responsive-layout-audit.md).
- `browser-act` runs an ordered `operations` array. The batch is validated as
  a whole before anything runs, every operation is time-bounded, and the
  result reports what the batch caused in an `effects` digest. Contract: the
  same spec; mechanics: [declarative automation](declarative-browser-automation.md).
- `browser-reason` answers one question per `topic` with a diagnosis - a
  one-line `verdict`, the `root_cause` behind it, and the one `next` call worth
  making - not a dump of facts. Contract:
  [flight recorder and evidence](flight-recorder-and-evidence.md).
- `browser-audit` persists a phased browser-to-code investigation
  ([contract audit](contract-audit-and-repo-tracing.md)).
- `browser-test` creates, inspects and runs declarative fixtures
  ([token-efficient testing](token-efficient-testing.md)).
- `browser-mangle` is the bounded escape hatch for raw facts, queries, rules,
  waits and flight exports.
- `get-specs` and `check-specs` deliver and check configured spec invariants
  ([configurable spec delivery](configurable-spec-delivery.md)).

## Response shape

Progressive tools accept `view: summary | compact | full`, default `compact`.
Summary returns status and counts, compact adds what the next decision needs,
and full returns bounded underlying evidence. A response carries `success`,
`status`, a one-line `summary`, its payload, and `evidence_handles` where a
drill-down is useful; an agent expands a handle instead of repeating the call
at `full`. When one follow-up is clearly next, the response names it as a
ready call `{tool, args}` with a `reason` where one helps: `next` on
browser-reason, `next_step` and `recommendations` on browser-observe.

A failure always carries a reason. Input that cannot mean what the caller
intended - an unknown `browser-act` operation type or field, an unknown
fixture or assertion key, an unknown `browser-reason` topic - is refused with
an error naming what is accepted, and the closest match when there is one,
instead of being ignored.

Spec excerpts are attached to observe and act results only when the caller
passes `include_specs: true`, because they would otherwise cost tokens on
every call.

## Token budgets

Schema and description text is paid on every turn, so tool descriptions stay
short and a field description names only what a caller would get wrong. The
live probe tests drive `testdata/probe/index.html` in headless Chrome and pin
each default response's cost (`internal/mcp/observe_probe_live_test.go`,
`reason_probe_live_test.go`, `act_live_test.go`):

| Call | Budget (tokens) |
|---|---|
| `browser-observe` composite, compact | 900 |
| `browser-observe` `mode: text` | 700 |
| `browser-observe` `mode: screenshot`, default | 150 |
| `browser-observe` `mode: screenshot`, `annotate: true` | 1,200 |
| `browser-observe` `mode: sessions`, one tab | 150 |
| `browser-act` batch whose click triggers a failure | 250 |
| `browser-reason` `why_failed`, compact | 600 |
| `browser-reason` `health`, compact | 300 |
| `browser-reason` `network`, summary | 250 |

## Deadlines and redaction

Every call runs under a deadline and answers with a structured timeout
instead of hanging ([delivery and verification](delivery-and-verification.md)).
Every tool result crosses the credential redactor before MCP delivery. Tool
schemas, registration counts, README references, and tests change together
whenever this contract changes.
