---
title: BrowserNERD Security and Privacy
created: 2026-07-23
last_updated: 2026-10-24
doc_type: security-spec
subsystem: browsernerd
read_when: Changing capture, persistence, workspace config, JavaScript evaluation, Mangle, or file exports
indexes: [browsernerd-corpus, security]
---

# BrowserNERD Security and Privacy

## Objective

Attaching an agent to an authenticated browser must not turn credentials or
local authority into queryable or persisted data.

## Capture boundary

- Authorization, Cookie, Set-Cookie, proxy credentials, API keys, CSRF tokens,
  and configured sensitive headers are redacted before fact creation.
- URLs have sensitive query parameter values redacted.
- Password, token, hidden, payment, and credential-like input values become
  `[REDACTED]`.
- Console, toast, DOM, request, response, and session metadata pass through the
  same redactor before persistence or export.
- A failed response's body snippet (`net_failure_body`, at most 1 KB of a
  text or JSON body) is redacted before it becomes a fact.
- WebSocket frame payloads are never captured; `ws_event` records the
  lifecycle only.
- Explicit gated input traces capture fixed native event metadata only.
  Trace-local node refs carry no DOM attributes or text. Character keys,
  input/composition events, input values, callback source/arguments and
  application state are excluded. Selector/ref strings resolve the exact
  target but are not echoed into the receipt. Traces remain bounded in
  memory, expire after their terminal retention TTL, and create no facts or
  persisted payloads. Final cancellation is unknown until a later task
  confirms it; cleanup never replaces missing evidence with false.
- `browser-reason` `network` with `body: true` returns a response body's
  shape - keys, array lengths, clipped and redacted scalar samples - never
  the body, and never the value of a key that names a token, password, key or
  cookie.
- `browser-observe` `mode: storage` returns cookie and web-storage names,
  sizes, flags and expiry, and only the `exp` and `iat` claims of a JWT-shaped
  value; no stored value, and no other claim, leaves the page.
- The doctor reports whether each `value_env` credential variable is set,
  never its value or length, as names listed under `credentials.set` and
  `credentials.unset`: a variable name used as a key would itself be blanked
  by the redactor.
- Every tool result crosses the redactor after the tool returns, so a test of
  what an agent receives reads the result through it
  (`throughServerRedactor` in `internal/mcp/server_calls_test.go`).
- Request and response lifecycle facts remain correlated; sampling cannot
  independently drop one side.

## Persistence boundary

- Runtime directories use owner-only permissions.
- Trace and session files use owner-only permissions.
- Recorder tool arguments and results are recursively sanitized.
- Flight exports sanitize facts and native or Docker log entries again at the output boundary.
- Persisted browser-audit plans and findings use private directories and files.
- Runtime data defaults to the operating-system user cache, outside source
  repositories.

## Network exposure

The stdio transport is the default. The Streamable HTTP transport
(`mcp.http_port`, or `-http-port`) serves MCP at `/mcp` and listens on
`mcp.http_host` (or `-http-host`), default `127.0.0.1`: a server that drives a
browser and reads its facts is reachable from other machines only when an
operator names `0.0.0.0` or an interface. Requests arriving over loopback
with a Host header other than localhost are refused, which closes the
DNS-rebinding route from a web page to the server.

## Authority boundary

Auto-discovered `.browsernerd/config.yaml` is data, not authority. Unless the
caller passes `--trust-workspace-config`, it cannot provide browser launch or
attachment settings, read specs or schemas outside the selected workspace, or
write runtime data outside that workspace. It also cannot enable arbitrary
JavaScript evaluation.

Configured native log files use bounded reads, never shell commands. Untrusted
workspace sources remain inside the workspace even after symlink retargeting;
explicitly trusted profiles may select external regular files. Read errors,
missing timestamps, truncation and partial source failures remain visible while
all evidence and diagnostic errors cross the credential redactor. Doctor lists
source identities and types without returning file contents or credentials.

## Script, file, and logic boundaries

Untrusted strings are page-evaluation arguments, never JavaScript source.
Arbitrary `evaluate-js` execution is disabled by default because a page script
can read credential-bearing DOM and application state. An operator must enable
`security.allow_unsafe_javascript` in an explicitly trusted config; the
progressive disclosure gate still applies, and returned structures are
redacted again.
Screenshot and export targets must remain under configured writable roots,
including after traversal and symlink resolution. A file handed to a page by
`browser-act`'s `upload` op is read through `PathPolicy.ResolveForUpload`: it
must resolve, symlinks included, inside the workspace root; no path component
under that root may start with a dot (`.env`, `.git`, `.credentials`,
`.mcp.json`); a name carrying `secret`, `credential`, `password`, `token`,
`private_key` or `id_rsa`, or a `.pem`, `.key`, `.p12`, `.pfx` or `.kdbx`
extension, is refused; and it must be a regular file of at most 50 MB.
User-submitted Mangle work
has byte, clause, premise, rule, fact, result, and time limits, and is evaluated
on isolated state before commit.

## Verification

Regression tests cover recursive redaction, sensitive inputs, private
permissions, path traversal and symlinks, upload path refusals, workspace launch requests, JavaScript
injection attempts, disabled unsafe JavaScript, private audit persistence,
Mangle limits, and result bounds.
