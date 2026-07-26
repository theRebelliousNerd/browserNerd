# Security Boundaries

Redaction and path confinement are shared infrastructure. Add sensitive-key
coverage here rather than duplicating ad hoc checks in callers.

Path resolution must reject traversal and symlink escape and create private
directories and files. Redaction must handle nested maps, arrays, headers,
URLs, authorization schemes, tokens, credentials, and sensitive input
metadata. Tests cover every added boundary and bypass attempt.
