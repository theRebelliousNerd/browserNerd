# Configuration

Configuration merges defaults, an optional discovered workspace file, and an
explicit config. Discovered workspace config has no process-launch or
out-of-workspace path authority unless the caller explicitly trusts it.

Maintain defaults and validation for browser lifecycle limits, Mangle limits,
named spec corpora, recorder paths, and writable roots. Resolve workspace
relative schema, spec, index, session, trace, and log paths consistently.
Runtime behavior belongs outside this package.

Arbitrary JavaScript is disabled by default. An untrusted discovered workspace
must never enable `security.allow_unsafe_javascript`; trusted operator config
is required.
