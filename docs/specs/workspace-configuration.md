---
title: BrowserNERD Workspace Configuration
created: 2026-07-23
last_updated: 2026-07-23
doc_type: configuration-spec
subsystem: browsernerd
read_when: Changing config discovery, path resolution, project corpora, or launch profiles
indexes: [browsernerd-corpus, configuration]
---

# BrowserNERD Workspace Configuration

Status: shipped.

Configuration precedence is CLI, explicit config, discovered workspace config,
then defaults. Discovery walks a bounded number of parents for
`.browsernerd/config.yaml`. A discovered file is untrusted unless the user
explicitly passes `--trust-workspace-config`.

Untrusted workspace configuration may tune in-workspace schemas, traces,
repository bounds, and spec corpora. It may not choose a browser executable,
launch arguments, attach endpoint, auto-start behavior, or paths outside the
workspace. This prevents a cloned repository from turning discovery into
process execution.

Mangle `schema_path` accepts a `.mg` file, a schema module directory, or a
confined manifest. Spec delivery accepts named generic Markdown sources with
roots, optional index hints, include/exclude patterns, and result/file/byte
limits. Cross-Thread specs and indexes are ordinary read-only configured
sources, not hard-coded platform dependencies.

Default runtime data belongs in the operating-system cache outside the source
repository. Model-directed writes are further restricted by the security path
policy.
