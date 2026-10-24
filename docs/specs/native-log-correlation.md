---
title: Native and Container Log Correlation
created: 2026-10-22
last_updated: 2026-10-22
doc_type: spec
subsystem: docs
read_when: Working on docs features this spec covers
indexes: []
---

# Native and Container Log Correlation

BrowserNERD correlates a browser failure with the configured application's server
logs independently of the application language, framework, or deployment topology.
File sources and Docker sources feed the same bounded, redacted evidence contract.

## Configuration

A project or machine profile may configure native sources:

```yaml
logs:
  files:
    - name: api
      path: logs/api.log
    - name: web
      path: logs/web.log
  log_window: 30s
  max_read_bytes: 1048576
  max_entries: 2000
```

Names are user-selected source identities. Paths resolve through the normal
configuration precedence and path-resolution rules. There are no hardcoded
application paths, routes, service names, credentials, or tenant assumptions.
Docker sources remain independently configurable through the docker section;
both source types can be active together.

Untrusted auto-discovered workspace file sources are confined to the workspace,
including resolved symlinks, and cannot introduce executable commands. Explicitly
selected machine profiles and trusted workspace configuration may select external
log files. File readers recheck confinement at query time, refuse non-regular
files, and enforce per-source read and entry bounds. All configured limits and
durations are validated; invalid settings fail startup clearly.
Source names must be unique across native files and enabled Docker containers.
Native files are read concurrently so a large or unavailable sibling does not
consume another file's collection opportunity. Docker queries are independently
bounded to 900 milliseconds, 1 MiB and 2,000 entries per container; hitting a
byte, line or entry cap reports truncated evidence.

## Shared Source Contract

The transport-independent log entry carries source identity, measured timestamp,
severity, optional tag, message and original text. A source queries entries between
two instants under the caller's context and reports its correlation window.
A combined source preserves evidence from healthy sources and reports partial
source failures explicitly. All-source failure is unavailable, never an empty
success. Per-source failures are isolated so one unavailable transport does not
suppress another source's working evidence.

Native reads reopen bounded file tails per query so append and rotation are
visible. Supported timestamp formats include RFC3339/ISO8601 text timestamps,
common space-separated timestamped logs, and structured JSON timestamp/time
fields with message/msg and level/severity fields. Timestamps with an explicit
offset keep that offset; timestamps without an offset use local time. Multiline
continuations attach to the preceding timestamped entry. Untimestamped messages
never receive a fabricated current timestamp; an uncorrelatable file reports
that limitation explicitly. Cancellation and byte/entry caps bound every read.

## Reporting and Wiring

The server constructs sources from validated configuration and supplies the
combined source to browser-reason, error diagnostics, and optional server-log
flight exports. Existing browser console, network, toast, dialog and WebSocket
capture does not depend on whether any log source is configured.

Correlation matches the observed request path and event time using the same
rules for every source. Tracebacks keep their associated request context;
compact diagnoses include the relevant exception and bounded evidence.
Source names identify evidence without claiming a framework-specific cause.
Unavailable, partially available, missing-timestamp and truncated evidence stays
visible in the diagnosis. Doctor reports configured source types and names
without dumping file contents or credentials. Redaction applies before evidence
reaches MCP responses, facts, diagnostics or exports.
Redaction includes credentials inside quoted JSON log records and embedded JSON
fragments, including the original-text field. A Docker source with no measured
health evidence is unassessed rather than healthy.

## Verification

Scoped Go tests exercise native JSON and text logs, timestamp windows, multiline
exceptions, append and rotation, cancellation, bounded reads, unavailable and
partially failing sources, symlink confinement, redaction, and configuration
precedence. Server wiring tests prove that a native source reaches browser-reason
and the export/diagnostic readers; a helper never called by the server is incomplete.

A deterministic local HTTP application supplies a failing request and writes a
matching timestamped native log. An MCP smoke test must report the observed
failure with that native evidence, with Docker disabled. Generic fixture source
names and routes prove application independence. Verify the built binary through
MCP before claiming the capability is live.

Run `go run ./cmd/smoke -mode native-logs -server bin/browsernerd -timeout 90s`
from `mcp-server` to exercise the generic application through a real browser and
the stdio MCP interface. The smoke verifies the HTTP 500, matching exception,
native source listing, Docker-disabled configuration and exported log evidence.
