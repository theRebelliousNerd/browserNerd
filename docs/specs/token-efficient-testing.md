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
name: browserqa login
session_id: <session>
operations:
  - {type: navigate, url: "http://localhost:3001/login"}
  - type: fill
    fields:
      - {ref: "testid:auth-email", value: browserqa@crossthread.dev}
      - {ref: "testid:auth-password", value_env: BROWSERNERD_TEST_PASSWORD}
    submit_button: "testid:auth-sign-in-btn"
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
operations nor assertions are refused before anything runs, as is a run with
no assertions. The operations meet the validation `browser-act` applies to a
batch ([declarative automation](declarative-browser-automation.md)), so a
misspelled field fails the fixture instead of being skipped, and replay runs
with `browser-act`'s upload path policy and credential redactor.

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

Execution establishes a per-assertion baseline, replays through `browser-act`,
and evaluates complete bounded queries. Action tests default to `scope:
fresh`, so errors that existed before the test do not cause false failure.
Assertion-only tests default to the current fact state. The result carries
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
