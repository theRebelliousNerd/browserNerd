---
title: BrowserNERD Configurable Spec Delivery
created: 2026-07-23
last_updated: 2026-07-23
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing documentation ingestion, indexes, matching, observe, or act
indexes: [browsernerd-corpus, specs]
---

# BrowserNERD Configurable Spec Delivery

Any project can expose its specification layout without BrowserNERD depending
on project runtime code or adopting one fixed directory model.

```yaml
specs:
  enabled: true
  sources:
    - name: product
      roots: ["docs/specs"]
      indexes: ["docs/indexes/spec-catalog.md"]
      include: ["**/*.md"]
      exclude: ["**/archive/**"]
  max_files: 2000
  max_file_bytes: 2097152
  max_results: 12
  max_excerpt_bytes: 1200
```

Each source is a named corpus. Indexes are parsed first for Markdown links and
provide ordering hints. Bounded root scans remain authoritative, so stale
indexes cannot hide documents.

All Markdown files are delivery context. BrowserNERD extracts common title,
summary, description, doc type, subsystem, read-when, tags, source, and binding
metadata. BrowserNERD invariant blocks are optional and make requirements
executable by `check-specs`.

Specs rank by exact file, route, component, and selector bindings, then feature
terms. Responses contain bounded relevant excerpts, not whole documents.
`get-specs` and `check-specs` are on the default MCP surface, and observe and
act can attach `spec_context`.

Untrusted workspace roots and indexes remain within that workspace. Trusted
config may add read-only external corpora such as Cross-Thread docs.
