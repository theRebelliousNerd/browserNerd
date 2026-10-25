---
title: BrowserNERD Progressive MCP Tool Contract
created: 2026-07-23
last_updated: 2026-10-24
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
  `hidden`, `grids`, `react`, `dom_snapshot` and `input_trace`. Contract:
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

Every valid browser-reason response carries `scope` and `scope_since_ms`,
including summary view. The label and each verdict state the effective
navigation, action or session-history window; the timestamp records the
actual lower bound, with `0` meaning unbounded retained history. These fields
describe the evidence selection without changing topic defaults.

A failure always carries a reason. Input that cannot mean what the caller
intended - an unknown `browser-act` operation type or field, an unknown
fixture or assertion key, an unknown `browser-reason` topic - is refused with
an error naming what is accepted, and the closest match when there is one,
instead of being ignored.

The `react` and `dom_snapshot` observation modes advertise `gate_reason` and
`approved_by_handle` and pass them to the focused extraction tools. Deep
inspection requires recent, same-session browser-reason evidence: a matching
authorized reason or disclosure handle; `explicit_user_intent` requires a
handle. Missing, expired, or foreign-session evidence remains denied.
Only the actual `browser-reason` producer issues authorization. Its engine-owned
grant ledger is independent of sampled/evicted Mangle telemetry. Raw facts and
non-reason disclosure handles never authorize a gated tool. Each atomic issued
bundle has a ten-minute expiry; refreshing exact keys does not extend other
keys. Live grants are never evicted to admit a new bundle.

Every valid reason response distinguishes expansion `evidence_handles` from
`gate_grants`: `available` describes issuance of this response's whole bundle,
`expires_at_ms` names its actual expiry, and nonempty `reasons` lists authorized
reason names. Failed issuance has `available: false` and a named `reason`;
expansion handles and the diagnosis remain usable. An older independently
issued grant may remain valid until its own expiry when a refresh is refused.
The ledger permits at most 256 sessions, 4096 distinct handle/reason keys and
1048576 accounted bytes. A session accounts for its UTF-8 key bytes plus 64;
each grant accounts for its key bytes plus 64. Session/handle keys are bounded
to 256 bytes. Expired entries are removed lazily on issuance or lookup;
capacity refusal never partially publishes or refreshes a bundle.
Every view preserves a denied, failed, or timed-out extraction as
`success: false`, with its reason and gate guidance. No inspection count or
successful observation handle is manufactured on failure.

Both modes and their focused tools accept one `root_selector` or `root_ref`
(omit for a bounded document prefix), `max_nodes` (1..500, default 200),
`max_depth` (1..50, default 12), `max_items` (1..200, default 20), and
`max_snapshot_bytes` (1024..32768, default 8000). `visible_only` defaults to
false for these modes. `emit_facts` optionally records captured rows; it
defaults to false for explicit inspection. Root selectors must match exactly
one element; refs use the existing session ref registry and resolver. Missing,
ambiguous, stale, or conflicting roots are refused. No user JavaScript is
accepted. Options are passed as JSON arguments to fixed internal scripts.

`input_trace` uses the same disclosure gate for `trace_action: arm | read |
stop`. Arm resolves one exact target via `root_ref` or `root_selector`; read
and stop require the returned same-session `trace_id`. Its fixed event set,
capture/response budgets, trace-local node identities, cancellation limits,
cleanup and terminal retention are defined in
[observation and interaction](browser-observation-and-interaction.md#input_trace).
Trace evidence describes delivery/cancellation, never callback execution.

The response returns structured `nodes` directly, independently of Mangle
retention. Every view carries `scope`, traversal/budget metadata, `scanned`,
`captured`, `returned`, `capture_complete`, `response_complete`, `complete`,
`truncated` and named `incomplete_reasons`. Summary omits rows and names that
response omission; compact and full obey the same explicit row/byte bounds.
An incomplete successful capture has `status: partial` on the progressive
receipt; `success: true` means the bounded inspection ran, not that it was complete.
Traversal and field limits are explicit, including an inaccessible React tree.
A default document prefix never claims to represent unvisited main content.
Optional fact storage reports attempted/submitted evidence and retention as
unknown; submission is not proof that a fact survived engine eviction.

Browser/session lifecycle routing fields are exposed in the progressive
provider schema rather than requiring a focused-tool surface. Compact action
receipts preserve created browser/session IDs and isolation metadata. A
create-first fixture may omit its initial session when its normalized
sequence establishes one before any page operation; a test receipt and its
assertions/diagnosis identify the actual final replay session. The assertion
binding contract is in [token-efficient testing](token-efficient-testing.md).

`browser-mangle` accepts `session_id` only for `read` and `export_flight`.
Scoped `read` matches the first fact argument to that exact session before
applying its recent-row limit; facts with no arguments or a different first
argument are excluded. Unknown session IDs return no matching facts rather
than falling back to global data. Its receipt identifies the requested session.
Omitting the session keeps the explicitly global retained-engine read. A
provided empty, whitespace-only or non-string session_id is InvalidParams;
it cannot accidentally become a global read.
A nonempty session_id on query, evaluate, temporal, waits, schema or writes
is refused with InvalidParams before evaluation or mutation. Raw queries
remain explicit Mangle queries; the fixture's reserved-S binding is not
implicitly applied to raw browser-mangle queries. Browser context isolation
does not partition the server's shared fact engine.

Spec excerpts are attached to observe and act results only when the caller
passes `include_specs: true`, because they would otherwise cost tokens on
every call.

## Token budgets

Schema and description text is paid on every turn, so tool descriptions stay
short and a field description names only what a caller would get wrong.
The `browser-observe` provider definition stays within 1,100 estimated tokens
(description and JSON schema bytes divided by four), enforced by
`TestObserveToolDefinitionBudget` without reducing its advertised capabilities.
The live probe tests drive `testdata/probe/index.html` in headless Chrome and pin
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
