---
title: BrowserNERD Security and Privacy
created: 2026-07-23
last_updated: 2026-07-23
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
- Request and response lifecycle facts remain correlated; sampling cannot
  independently drop one side.

## Persistence boundary

- Runtime directories use owner-only permissions.
- Trace and session files use owner-only permissions.
- Recorder tool arguments and results are recursively sanitized.
- Flight exports sanitize facts and Docker rows again at the output boundary.
- Persisted browser-audit plans and findings use private directories and files.
- Runtime data defaults to the operating-system user cache, outside source
  repositories.

## Authority boundary

Auto-discovered `.browsernerd/config.yaml` is data, not authority. Unless the
caller passes `--trust-workspace-config`, it cannot provide browser launch or
attachment settings, read specs or schemas outside the selected workspace, or
write runtime data outside that workspace. It also cannot enable arbitrary
JavaScript evaluation.

## Script, file, and logic boundaries

Untrusted strings are page-evaluation arguments, never JavaScript source.
Arbitrary `evaluate-js` execution is disabled by default because a page script
can read credential-bearing DOM and application state. An operator must enable
`security.allow_unsafe_javascript` in an explicitly trusted config; the
progressive disclosure gate still applies, and returned structures are
redacted again.
Screenshot and export targets must remain under configured writable roots,
including after traversal and symlink resolution. User-submitted Mangle work
has byte, clause, premise, rule, fact, result, and time limits, and is evaluated
on isolated state before commit.

## Verification

Regression tests cover recursive redaction, sensitive inputs, private
permissions, path traversal and symlinks, workspace launch requests, JavaScript
injection attempts, disabled unsafe JavaScript, private audit persistence,
Mangle limits, and result bounds.
