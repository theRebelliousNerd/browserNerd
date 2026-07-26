---
title: BrowserNERD Secure Declarative Browser Automation
created: 2026-07-23
last_updated: 2026-07-23
doc_type: spec
subsystem: infra
read_when: Changing BrowserNERD security, browser automation, spec delivery, test generation, or workspace configuration
indexes: [browser-automation, security]
---

# BrowserNERD Secure Declarative Browser Automation

**Source:** `dev_tools/BrowserNERD/mcp-server/`
**Status:** Active implementation
**Last Verified:** 2026-07-23

## Purpose

BrowserNERD is a standalone development tool that turns browser, repository,
and specification state into bounded Mangle facts. Agents use those facts for
token-efficient automation, synchronization, testing, conformance checks, and
causal diagnosis.

The differentiator is not another imperative browser driver. It is a
declarative state and reasoning layer:

```text
browser events + repository evidence + specs
                    |
                    v
          privacy-safe Mangle facts
                    |
          +---------+---------+
          |                   |
   declarative waits    spec/test conformance
          |                   |
          +---------+---------+
                    |
             causal diagnosis
```

BrowserNERD remains fully isolated from every target repository's runtime
packages and resources. A repository may configure and invoke BrowserNERD as a
development tool, but BrowserNERD must remain reusable across unrelated
projects. Cross-Thread's `docs/specs` and `docs/indexes` are one configured
input corpus, not BrowserNERD-owned platform surfaces.

## Non-Negotiable Trust Boundaries

1. Secret material never enters Mangle facts, recorder traces, tool logs,
   exported flight data, or persisted session metadata.
2. Request and response headers are allowlisted or redacted before ingestion.
   `authorization`, cookies, API keys, proxy credentials, CSRF tokens, and
   equivalent custom secret headers are always redacted.
3. Input values from password, hidden, token, credential, and payment fields
   are represented only as redacted metadata.
4. Recorder and runtime state files are owner-only (`0600`) inside
   owner-only directories (`0700`).
5. Model-supplied paths cannot escape configured writable roots.
6. Workspace discovery never grants execution authority. A discovered
   repository config cannot control `browser.launch`; executable settings
   require an explicit trusted config or explicit workspace opt-in.
7. Browser-side scripts pass untrusted values as data. No user-provided value
   is interpolated into JavaScript source.
8. Mangle rules and queries have configurable size, complexity, result, fact,
   and execution-time limits. Panics are converted to explicit errors.
9. Every browser session can be closed independently. Maximum session count
   and idle reaping prevent unbounded contexts, goroutines, and pollers.

## Configuration Contract

Project configuration remains YAML under `.browsernerd/config.yaml`, but
authority is separated from discovery:

```yaml
security:
  writable_roots: [".browsernerd/data"]
  redact_sensitive_data: true
  extra_sensitive_keys: []

browser:
  multi_tab_default: true
  max_tabs: 32
  max_browsers: 4
  idle_tab_timeout: "30m"
  enable_header_ingestion: true

mangle:
  evaluation_timeout: "2s"
  max_query_bytes: 32768
  max_rule_bytes: 65536
  max_rule_clauses: 16
  max_premises: 64
  max_rules: 128
  max_created_facts: 10000
  max_query_results: 1000

specs:
  enabled: true
  max_files: 2000
  max_file_bytes: 2097152
  max_results: 12
  max_excerpt_bytes: 1200
  sources:
    - name: project-specs
      roots: ["docs/specs"]
      indexes: ["docs/indexes/spec-catalog.md"]
      include: ["**/*.md"]
      exclude: ["**/archives/**"]
```

All source paths resolve against the explicitly selected workspace. Sources
are independently selectable by name. Defaults support `.browsernerd/specs`
without making Cross-Thread paths part of BrowserNERD itself.

## Spec and Index Ingestion

The ingestion layer must support multiple configured sources and preserve:

- provenance frontmatter such as `title`, `last_updated`, `doc_type`,
  `subsystem`, `read_when`, and `indexes`;
- explicit BrowserNERD bindings and Mangle invariants when present;
- structured Markdown headings, file references, routes, component names,
  selectors, and verification sections as delivery metadata;
- generated Markdown indexes as lightweight discovery maps;
- bounded scanning and deterministic ordering.

Index files prioritize candidate documents before the bounded source scan.
They are advisory, not authority: missing or stale indexes cannot hide files
present under a configured root.

`get-specs` returns only relevant excerpts, bindings, and invariants for a
file, line range, component, route, selector, subsystem, document type, or
configured source. `check-specs` evaluates checkable invariants against the
current session and reports causal evidence for violations.

## Declarative Automation Contract

- `wait-for-condition` and subscriptions only trigger from facts derived after
  the wait/subscription begins unless history is explicitly requested.
- Correlated request/response facts are sampled as a unit. Throttling cannot
  leave an orphaned response or silently erase a request needed for network
  idle and failure reasoning.
- Action responses include a bounded state delta: navigation, requests,
  responses, console errors, toasts, and relevant DOM/fiber changes since the
  action began.
- `generate-test` records privacy-safe action intent plus Mangle assertions.
- `run-test` replays actions, evaluates bounded queries, and emits structured
  pass/fail facts plus compact causal diagnosis.
- Query and assertion failures are explicit; no fallback may turn evaluation
  failure into a passing or ordinary failed assertion.

## Lifecycle Contract

- Tools expose `close-session`, `session-focus`, and browser inventory.
- Closing cancels all stream goroutines and pollers, closes the page/context,
  removes persisted metadata, and is idempotent.
- Shared-context tabs are the default; isolated tabs are opt-in.
- Multiple independent Chrome instances are selectable by `browser_id`.
- Session creation fails fast at `max_tabs`; browser launch fails fast at
  `max_browsers`.
- Idle reaping is configurable and disabled only by an explicit zero timeout.
- Browser shutdown closes every session before closing Rod.

## File and Module Boundaries

No production Go file may exceed 1,500 lines. Existing oversized files are
split by responsibility:

- browser audit orchestration, persistence, plan execution, and report
  rendering;
- progressive observe, act, reason, Mangle operations, and flight export;
- session lifecycle, event capture, DOM capture, and storage transfer;
- Mangle parsing, evaluation, subscriptions, bounds, and fact buffering.

Security helpers live in dedicated packages or focused files and are reused by
browser ingestion, recorder output, MCP logging, and export code.

## Dependency Policy

- Use the latest stable supported Go toolchain with security fixes.
- Use the latest tagged direct dependency releases that pass the full suite.
- Resolve transitive versions through `go mod tidy`; do not independently pin
  incompatible Rod helper modules.
- Record deliberate holds when an upstream project has no newer compatible
  tag.
- CI runs formatting, vet, tests, and a build on Windows and Linux.

As verified on 2026-07-23, the targets are Go 1.26.5,
`codeberg.org/TauCeti/mangle-go` v0.5.0,
`github.com/go-rod/rod` v0.116.2, and
`github.com/mark3labs/mcp-go` v0.57.0.

## Verification

Required gates:

1. Focused regression tests for each security boundary.
2. Browser lifecycle and fresh-fact subscription tests.
3. Config tests proving discovered workspace configs cannot launch commands.
4. Spec ingestion tests against configurable generic fixtures and the live
   Cross-Thread `docs/specs` plus `docs/indexes` formats.
5. Screenshot tests for PNG and JPEG.
6. Path-confinement tests for absolute paths, traversal, and symlinks.
7. Mangle timeout, size, panic, and result-limit tests.
8. `go test ./...`, `go vet ./...`, and `go build ./cmd/server`.
9. A real Chrome smoke test when Chrome is available.
