---
title: BrowserNERD Delivery and Verification
created: 2026-07-23
last_updated: 2026-09-24
doc_type: delivery-spec
subsystem: browsernerd
read_when: Preparing releases, dependency updates, CI, or completion claims
indexes: [browsernerd-corpus, delivery]
---

# BrowserNERD Delivery and Verification

Production Go files remain below 1,500 lines. Progressive tools, audits,
sessions, and repository tracing are split by responsibility. The nearest
`agents.md` files describe ownership after large refactors.

The 2026-10-11 direct-dependency baseline is Go language level 1.27.0 with
toolchain 1.27.2, Mangle 0.5.0, Rod 0.116.2, and mcp-go 0.57.0.

Automated gates are formatting, `go vet ./...`, `go test ./...`, and
`go build ./cmd/server`, plus focused security and lifecycle regressions.

## Building and knowing what is running

`go run ./cmd/rebuild` (from `mcp-server/`) builds `bin/browsernerd.exe`
stamped with `internal/buildinfo.Version`. A running MCP server holds that file
open; Windows refuses to overwrite a running executable but allows renaming
it, so the tool moves the held binary aside to
`bin/browsernerd.<timestamp>.old.exe`, builds into its place, deletes older
moved-aside binaries no longer running, and puts the previous binary back if
the build fails. The next server spawn (a `/mcp` reconnect) runs the new build.

Every binary can name itself: `browsernerd.exe -version` prints version, VCS
revision (with `+dirty` for an uncommitted tree), build time and Go version,
and the MCP server identifies as `<version>+<revision>`. At start the server
compares its own build time with the newest non-test `.go`/`.mg` file of the
module it was built from (`bin/` sits under the module root); when source is
newer, `launch-browser` carries a `warning` and the doctor lists it as a
problem.

`browsernerd.exe -doctor` (same config flags as the server) and
`browser-observe mode:doctor` run the self-diagnosis in
`internal/mcp/server_doctor.go`: binary identity and staleness, resolved schema
and writable paths, whether each credential variable fixtures resolve through
`value_env` is set (never its value or length), browser connection and
active/detached session counts, the fact engine (`ready`, `facts`, `limit`,
`by_class`, `evicted`, `rules`, `predicates`, `modules`, `project_modules`,
`last_eval_error`), Docker log reachability, calls in flight, recent failures
and the slowest recent calls. It leads with a `problems` list - a stale
binary, a missing directory or project module, an unset credential variable, a
failed last Mangle evaluation, evicted failure evidence, unreachable Docker, a
call still running past its timeout - and `status: ok | degraded`; the CLI
exits nonzero when degraded. The CLI checks its own environment, so run it with the MCP server's
env block to mirror the server.

## Every call has a deadline

A tool call runs under `mcp.tool_timeout` (default 2m, floor 5s) plus the
waits it declares - every `duration_ms` and `timeout_ms` in its arguments,
capped at 30m. A call still running at its deadline is answered with
`{success:false, status:"timeout", timed_out:true, error, next}` and its work is
left running (a CDP call blocked on a frozen renderer cannot be interrupted);
the doctor lists it as in flight and timed out, and names the recovery:
close-session, then a fresh `session_create`. A panic inside a tool becomes that
call's error instead of ending the server.

## Proof

A change is verified through the real tool call, not only `go test`: rebuild,
reconnect, and exercise it through MCP. `internal/mcp/probe_harness_test.go`
drives `testdata/probe/index.html` - one block per failure agents hit on the
live frontend - in headless Chrome with a real engine, and live tests pin each
tool's token budget on that page.

When Chrome is available, the live gate launches one browser, opens two shared
tabs, creates an isolated tab, launches a second browser, creates and closes a
tab there, then closes all remaining resources and confirms the inventories
are empty.

Significant published changes update the version. Runtime data and binaries
stay out of source control. Stashes, branches, and worktree status are checked
before push. Releases warn that traces produced by older builds may contain
plaintext credentials.
