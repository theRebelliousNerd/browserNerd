---
title: BrowserNERD Token-Efficient Testing
created: 2026-07-23
last_updated: 2026-09-24
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing flight recording, generated tests, assertions, replay, or failure diagnosis
indexes: [browsernerd-corpus, testing]
---

# BrowserNERD Token-Efficient Testing

Status: native create, inspect, and run workflow shipped.

Tests are portable YAML fixtures containing the same operations accepted by
`browser-act` plus Mangle assertions. The progressive `browser-test` tool
creates a fixture from privacy-safe action facts or normalizes an inline one
(`operation: create`), validates and summarizes it (`inspect`), and replays it
(`run`, the default), returning a bounded result. `run-test` and
`generate-test` are the focused tools behind it when the full surface is
enabled.

## The fixture

```yaml
name: private login
operations:
  - {type: session_create, browser_id: "<browser>", isolated: true, url: "https://example.test/login"}
  - type: fill
    fields:
      - {label: Email, value: qa@example.test}
      - {label: Password, value_env: TEST_LOGIN_PASSWORD}
    submit_button: "Sign in"
assertions:
  - {name: backend accepted and URL changed, query: "login_succeeded(S)", expect: present}
  - {name: no visible errors, query: "user_visible_error(S, _, _, _)", expect: absent}
```

A fixture has exactly the keys `name`, `session_id`, `operations` and
`assertions`; an assertion has `name`, `query`, `expect` (`present`, the
default, or `absent`) and `scope` (`fresh` or `current`). Steps written under
`actions` are read as `operations`, and every fixture this server writes
carries `operations`; a fixture with both is refused. An unknown fixture or
assertion key, an invalid `expect` or `scope`, and a fixture with neither
operations nor assertions are refused before anything runs. The operations
meet the validation `browser-act` applies to a batch
([declarative automation](declarative-browser-automation.md)), so a
misspelled field fails the fixture instead of being skipped, and replay runs
with `browser-act`'s upload path policy and credential redactor.

An initial `session_id` is optional when static validation proves that a
session-creating operation precedes every operation that needs a page.
`session_create` and `session_attach` establish the batch's session;
`session_fork` requires an explicit source or an established session.
Browser lifecycle and sleep do not establish a page. An unanchored page
operation anywhere in the normalized sequence rejects the entire fixture
before any operation runs, including when `stop_on_error` is false.
If an establishing operation fails, later page operations cannot borrow an
unrelated tab. Routing stays local to the batch; no global default changes.

A fixture with operations and no assertions (the key left out or `[]`) is a
replay: a login replay needs its `value_env` steps and nothing checked. It
runs and reports `status: replayed` with `replay_only: true` and `replayed`,
never `passed`, because it proves the steps ran - every locator resolved,
every typed value read back - and nothing about what they caused. `create`
and `inspect` say so in a warning.

## Credentials

Credential inputs use `value_env` on an `interact` operation or a `fill`
field, for example `value_env: BROWSERNERD_TEST_PASSWORD`. The environment
value is resolved only in an execution copy and is never written back into
the fixture. A `value_env` naming a variable that is unset or empty in the
BrowserNERD server's environment stops the run before its first step with an
error naming the variable, never a value: typed into a login form it would
submit an empty password, and the failure would read as a wrong one. Every
resolved value is scrubbed from the result, and credential facts stay
redacted.

## Running

Execution reads a per-assertion baseline, marks the run's start once the
clock has moved past the baseline read, replays through `browser-act`, and
evaluates complete bounded queries. Action tests default to `scope: fresh`;
assertion-only tests default to `current`, every buffered row.

The receipt's `session_id`, assertion session binding and failure diagnosis
use the actual session reached by replay, including after create, attach,
fork or focus. When a fixture has an effective session, query variable `S`
is reserved for that session and is bound before evaluation using the Mangle
syntax tree. Quoted text containing `S` is untouched. Queries without `S`
keep their explicit meaning; an assertion-only fixture without a session
queries the retained engine without a session binding. Fresh baselines are
evaluated with the same final-session binding as the result, so another
session's retained state or newly arriving events cannot satisfy `S`.
Each bound assertion reports its effective session_id. A fixture that switches
session uses the final session for all post-replay assertions; assertions
about a different session must explicitly name it without the reserved `S`.

A fresh assertion counts the rows that are true for the run:

- a row the baseline did not have - the run caused it;
- a row the baseline had that still holds without any event from before the
  run - a state, such as the tab's `current_url`, which the engine keeps only
  at its latest value. `QuerySince` evaluates the query over the facts
  stamped after the run's start, the eternal facts, and each superseding
  predicate's current fact, and a carried row counts as `still_true` when
  that view still derives it.

A row that only an earlier event supports - a failed request from before the
run - does not count, so errors that existed before the test cause no false
failure. A state that never changed does count: an absence check on
`current_url(S, "/login")` fails while the tab is still on /login, and a
presence check on the dashboard URL passes when the run started there.
Subtracting every baseline row would read an unchanged state as gone and
pass that absence check with its fact true. Each fresh result carries
`matched` (rows that count), `total_matched` (every buffered row) and
`still_true` when carried state is among the matches. The result carries
`replay` (the `browser-act` response, `results` and `effects` included),
`assertions` with each one's outcome, `passed`, and on failure a `diagnosis`.

## Capturing a fixture

`create` with a `session_id` and no inline test turns the session's
navigation, input and click facts into operations. Consecutive keystrokes into
one field merge into a single `type` carrying the final value, and a redacted
input becomes a `value_env` placeholder. A click on an element with no id or
name cannot be replayed by ref; it is left out and counted as
`skipped_clicks`, with a note to add it with a `text` or `label` locator.
`include_assertions` (default true) adds two absence checks - no visible
errors, no failed requests - which prove nothing alone; the author adds the
presence assertion that states what the flow is for.

Failure diagnosis returns compact causal facts: failed requests, console
errors, user-visible toasts, route or component mismatches, spec violations,
and probable frontend or backend code locations. Evidence handles permit
drill-down without attaching every trace row.

Declarative fixtures are the source of truth, run through the MCP workflow
and the Go-native stdio verifier (`cmd/smoke`). The Python evaluation
directories are external benchmark adapters, not the product test runner. Framework export, fixture versioning, and isolated replay profiles
remain future extensions.
