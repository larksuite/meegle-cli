# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
post-release changes accumulate under `[Unreleased]` and graduate to a
versioned section on each npm release.

## [Unreleased]

## [v1.0.9] - 2026-06-11

### Added

- `attachment +download` now performs an extra integrity check on the downloaded file and aborts — writing nothing to disk — when the file fails validation or cannot be verified. A failed check returns a `CLIENT_FILE_SIGN_MISMATCH` error and an unverifiable response returns `CLIENT_FILE_SIGN_UNVERIFIED`; both carry a retry hint.

## [v1.0.8] - 2026-06-09

### Added

- `--envelope` now surfaces the backend `logid` under `meta.logid` when present. Previously the MCP server emitted the trace id as a `logid:` content entry, which the CLI stripped into `Response.LogID` but only forwarded to `slog.Debug` — and since no default slog handler is configured, the id was silently dropped. Oncall now has a way to ask end users for an argos-lookupable trace id: re-run with `--envelope` and read `meta.logid`. The id is still suppressed without `--envelope` so default output stays clean for piping. Only covers the single-call path for now; batch (`+batch-get`) per-row logid remains a follow-up

## [v1.0.7] - 2026-06-09

### Fixed

- `meegle` now keeps local commands (`auth`, `config`, `inspect`, `completion`, `url`) bootable when server-side tool discovery fails and no command cache exists. Dynamic business commands now return a clear `TOOL_DISCOVERY_FAILED` server error instead of causing CLI bootstrap to fail before local command handlers can run.

## [v1.0.6] - 2026-05-28

### Changed

- `auth status` now issues a lightweight `tools/list` call to validate the token against the server, instead of only checking whether a token string is present locally. Cron / CI preflights that previously saw `authenticated: true` followed by an immediate `401` on the next business call now get the correct verdict up front. JSON output gains a `reason` field on failure (`no local token` / `token rejected by server` / `server unreachable: <err>`). A new exit code `2` indicates the server was unreachable (network, timeout, 5xx) — distinct from exit `1` which still covers missing or rejected tokens. There is no opt-out flag: offline status inspection is intentionally no longer supported.

## [v1.0.5] - 2026-05-20

### Added

- New `deliverable` domain exposing the upstream `list_deliverables` MCP tool — `deliverable list` returns deliverables along with their root and source work items
- New `resource` domain for Meegle's resource-library feature — `resource create` creates a resource template (resource instance) under a resource-library-enabled work item type, `resource meta-fields` lists the resource library configuration (resource fields and roles)
- New `wbs` domain covering plan-table draft + instance workflows: `wbs list-draft-rows` / `wbs list-instance-rows` filter and project rows, `wbs create-draft` creates a new draft for a work item instance, `wbs edit-draft` applies one atomic operation to a single draft row (add / delete / restore / sort / rename / owner / schedule via `--params`), `wbs publish-draft` publishes a draft online, `wbs reset-draft` discards unpublished changes, `wbs get-draft-progress` polls async-operation progress, `wbs list-element-templates` lists element templates (resource nodes and tasks) from the flow resource library

## [v1.0.4] - 2026-05-19

### Fixed

- Object-typed parameters discovered from the MCP tool schema (e.g. `subtask update --schedule`, `workflow update-node --schedule`) are now JSON-decoded into a real object before being sent to the backend; previously the raw JSON string was forwarded verbatim, so the server silently dropped the field and downstream attributes (e.g. `start_date` / `end_date` on a subtask schedule) ended up empty. Malformed JSON is still passed through unchanged so the backend can surface a clear validation error instead of a no-op (issue #14)

## [v1.0.3] - 2026-05-18

### Fixed

- `User-Agent` now reports the semantic version injected via `-ldflags "-X main.version=..."` (e.g. `meegle-cli/1.0.3`) instead of Go's `debug.ReadBuildInfo()` pseudo-version. Previously backends observed UAs like `meegle-cli/v0.0.0-20260417123425-9973fd9cecc4+dirty` whenever a build was made off a non-tagged commit or with uncommitted files in the worktree, making per-version traffic attribution unreliable on the server-side dashboard. The fix landed on `main` after the v1.0.2 release commit was tagged and published, so the CHANGELOG entry was retroactively moved out of the v1.0.2 section to match what npm actually shipped

## [v1.0.2] - 2026-05-14

### Added

- New `view list-multi-project-workitems` command exposing the upstream `list_multi_project_view_workitems` MCP tool — lists work items the caller can access under a multi-project ("panoramic") view, identified by a `multiProjectView` URL's `view_id`; takes `--project-key`, `--view-id`, and an optional `--page-num` (50 items per page, 1-based)
- Top-level parameters merged via `--params` / `--set` are now validated against the tool's declared flag set. Unknown arguments emit a one-line stderr warning at run time and appear under `validation.unknown_params` in `--dry-run` output, pointing users at `--params '{"fields":[...]}'` for work-item field values. Validation is advisory: unknown keys are still forwarded to the backend so a stale local tool-schema cache cannot block legitimate calls (refresh with `--refresh`)

### Changed

- Clarified `--params` documentation: the built-in flag's `--help` description and the README now state that top-level keys are merged as CLI flags (not a free-form payload), with a common-pitfall example showing that work-item field values must be wrapped in `fields[]`

## [v1.0.1] - 2026-04-28

### Added

- New `attachment` domain exposing Lark project's two-stage attachment protocol — basic `attachment prepare-upload` / `attachment prepare-download` for raw signed-URL preprocess payloads, and end-to-end shortcuts `attachment +upload` / `attachment +download` that chain the preprocess with the signed HTTP transfer client-side. Supports four resource types via `--resource-type`: `15` (workitem attachment field), `16` (rich-text field image), `13` (comment attachment), `14` (comment image); use `--work-item-id` for existing workitems and `--work-item-type` for the create-with-attachment path
- `meegle url decode` subcommand for parsing Meego URLs into structured fields, including trailing-wildcard URL rules and a `setting_other` fallback for unknown views
- `--params @file.json` syntax on every dynamic command so complex JSON payloads can be loaded from a file instead of a shell-escaped string
- `--refresh` global flag forces a fresh tool-schema discovery, bypassing the `~/.meegle/cache/tools.json` cache; `auth login` now also invalidates the cache so the next invocation picks up the new identity's command set

### Fixed

- Tool-schema cache now honors its 24h `DefaultTTL`. Previously `resolveTools` returned any cache hit regardless of age, so server-side schema changes (new flags, renamed parameters, new commands) only surfaced after manually deleting `~/.meegle/cache/tools.json`. A stale cache now triggers a fresh discovery, falling back to the stale snapshot only when the server is unreachable so offline users keep their last-known command set
- `meegle auth status` and the first-run auth check now route through the same `ResolveIdentity` path as runtime commands, so config-file tokens, env-var tokens, and store-backed tokens are treated consistently
- The OS-native credential store (macOS keychain, Linux `secret-tool`, Windows DPAPI) is now wrapped in a `FallbackStore` that transparently switches to the encrypted file store on the first runtime Save/Load failure — fixes the silent token-loss in sandboxed macOS, locked-keychain SSH sessions, and headless Linux containers reported in [larksuite/meegle-cli#3](https://github.com/larksuite/meegle-cli/issues/3) without writing a sentinel probe entry to the user's keychain on every CLI invocation
- `SecretToolStore.Load` no longer treats libsecret's "item absent from keyring" exit (exit 1, empty stdout, empty stderr) as a primary-store failure, so a fresh Linux user's first CLI run no longer permanently flips `FallbackStore` to the encrypted file store
- Failed token-store writes after a successful refresh are no longer silently dropped — a stderr warning is emitted so the next CLI invocation does not silently re-trigger a 401 / re-login loop

## [v1.0.0] - 2026-04-22

Initial public release of Meegle CLI, published on npm as `@lark-project/meegle`.

### Highlights

- **Broad coverage** — 12 business domains and 40+ commands across work items, workflow, subtasks, comments, work hours, relations, my-work, views, charts, team, user, and project
- **Agent-native** — bundled AI Agent Skill for Trae, Claude Code, Cursor, Windsurf, Gemini CLI, and Copilot; structured JSON output designed for both humans and agents
- **Two-layer parameters** — ergonomic `--flag-name` for everyday use, with a `--params <json>` fallback for complex payloads
- **Flexible output** — `json` / `table` / `ndjson` / `raw` formats, with `--select` dot-path projection for piping to other tools
- **Non-interactive auth** — OAuth browser login plus a `--device-code` flow for CI and agent shells
- **Dry-run previews** — `--dry-run` on mutation commands to inspect the exact payload before sending
- **Secure by default** — OS keychain credential storage, `${VAR}` env-var templating so secrets stay out of config files, and multi-profile switching for staging / production
