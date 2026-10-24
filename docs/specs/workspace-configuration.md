---
title: BrowserNERD Workspace Configuration
created: 2026-07-23
last_updated: 2026-10-22
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

Machine settings belong in the explicit `--config` file and project settings in
the workspace: native file sources (`logs.files`) and Docker log correlation
(`docker.enabled`, the project's `containers`) are project settings. Native
source paths resolve against their configuration layer: the workspace root or
the directory containing an explicit machine profile. Sources carry user-selected
names, measured timestamps and bounded evidence, following
[native log correlation](native-log-correlation.md). The machine config shipped as
`mcp-server/config.yaml` sets no `docker` block, so a project's correlation
survives it. An explicit config that does turn the workspace's Docker
correlation off, or names other containers, still wins as every later layer
does, and the load records it in `Config.Overrides` - naming the file that
overrode and the workspace file it overrode - which `doctor` lists under
`config.overrides` and as a problem.

`browser.isolated_display` is a machine launch setting. With `headless: false`
it requests a private headed display rather than the user's desktop. The Linux
backend requires Xvfb and Openbox; unsupported platforms and incompatible
headless/attach settings refuse the request. See
[browser lifecycle](browser-lifecycle-and-concurrency.md). Untrusted workspace
configuration cannot select it.

Untrusted workspace configuration may tune in-workspace schemas, traces,
repository bounds, and spec corpora. It may not choose a browser executable,
launch arguments, attach endpoint, auto-start behavior, or paths outside the
workspace. Native file sources are confined to the workspace, including symlink
resolution and rechecking on each read. Only regular files can be log sources;
no log setting invokes arbitrary commands. This prevents a cloned repository
from turning discovery into process execution or out-of-workspace file access.

The built-in Mangle schema is compiled into the binary, so a workspace never
carries a copy of it. Mangle `schema_path` names project modules loaded on top
of the built-in ones - a `.mg` file, a schema module directory, or a confined
manifest - and is empty by default; `.mg` files in the workspace's
`.browsernerd/schemas/` load as project modules without it, and a
`schema_path` set in a config layer takes precedence over that directory
([schema modules](mangle-schema-modules.md)). Spec delivery accepts named generic Markdown sources with
roots, optional index hints, include/exclude patterns, and result/file/byte
limits. Cross-Thread specs and indexes are ordinary read-only configured
sources, not hard-coded platform dependencies.

Default runtime data belongs in the operating-system cache outside the source
repository. Model-directed writes are further restricted by the security path
policy.
