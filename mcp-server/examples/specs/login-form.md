---
# Frontmatter: spec metadata + spec-wide invariants.
name: Login form
# The code this spec primarily describes. Inline invariants default to this
# file for their `from:`/`to:` line ranges unless they override with `in:`.
source: src/components/LoginForm.tsx
# How this spec binds to the running page. Any of component / route / selector;
# get-specs and check-specs can filter on these.
binding:
  - { kind: component, target: LoginForm }
  - { kind: route, target: /login }
# Invariants declared here apply to the whole feature (no line range). Each is a
# Mangle query scored by `expect`: "present" (default) passes on >=1 match,
# "absent" passes on 0 matches.
invariants:
  - name: no-visible-errors
    query: "user_visible_error(S, _, _, _)"
    expect: absent
---

# Login form

The login form takes an email and password and, on success, navigates to the
dashboard. Prose here is for the agent to read as intent - it is delivered
alongside the invariants.

## Submit gating

The submit button must stay disabled until both fields validate. The invariant
below governs lines 42-80 of the source file, so an agent editing that region
receives it from `get-specs`.

<!-- browsernerd:invariant name=submit-gated from:42 to:80 expect:present -->
Submit is only enabled once the form reports ready.
```query
form_ready(S)
```
<!-- browsernerd:end -->

## Successful login

<!-- browsernerd:invariant name=reaches-dashboard expect:present -->
A successful submit lands on the dashboard.
```query
login_succeeded(S)
```
<!-- browsernerd:end -->

## Validation helper

This invariant governs a *different* file than the spec's `source:`, via `in:`.

<!-- browsernerd:invariant name=validation-no-throw in:src/utils/validate.ts from:5 to:40 expect:absent -->
Validation must never surface a console error.
```query
console_event(S, "error", _, _)
```
<!-- browsernerd:end -->
