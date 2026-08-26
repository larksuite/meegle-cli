# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
post-release changes accumulate under `[Unreleased]` and graduate to a
versioned section on each npm release.

## [Unreleased]

## [v1.0.21] - 2026-08-26

### Added

- Added an interactive startup update notifier for npm-distributed CLI installs. At most once every 24 hours it checks the latest npm version, summarizes released `Added` and `Changed` entries from the GitHub CHANGELOG, and offers an arrow-key choice between immediate upgrade (recommended and selected by default) or a 24-hour deferral. Immediate upgrade installs the latest CLI first, then best-effort upgrades the Meegle Agent Skill through the existing install-wizard path; an unavailable Skill installer or failed Skill upgrade cannot fail the completed CLI upgrade. Non-interactive/CI, piped-output, install, and shell-completion invocations are skipped, and `MEEGLE_NO_UPDATE_CHECK=1` disables the check explicitly.
- Added non-MCP `ai-handoff availability` and `ai-handoff create-link` commands to both the CLI binary and programmatic command-string SDK. Availability reads the Handoff section of generic CLI config discovery; expected business rejections use `reject_code`/`reject_msg`, while dependency and transport failures use the standard error model. Successful config snapshots are cached per profile for up to 1 hour. Link creation accepts typed Project/WorkItemType/WorkItem/View/MeasureChart entities, always re-validates server-side, and returns an AI assistant URL.
- Handoff Config, Preference, and Create Link failures now surface the gateway `x-tt-logid` response header as `meta.logid` in the structured error envelope, so production-package failures can be traced without enabling or exporting debug logs.
- Successful Handoff Config, Preference, and Create Link calls now retain the same response-header LogID in result metadata, exposing it as `meta.logid` only when `--envelope` is requested while keeping default output unchanged.
- Create-link responses always include `available`: success returns HTTP 200 with `available=true` and the generated `url`; expected business rejection returns HTTP 200 with `available=false`, `reject_code`, and `reject_msg` and invalidates the local config cache. Unexpected failures continue to use the standard API error model.
- Added `preference handoff auto|ask|off`, backed by the server-side unified user preference service. The generic batch write API uses `type=handoff_suggestions` with a type-owned `{"mode":"off|ask|auto"}` JSON-string payload and reports only write success; current values are read through CLI config discovery. A successful write invalidates the local CLI config cache.
- Added the local `MEEGLE_AI_HANDOFF=disabled` hard-disable. It makes `availability` and valid `create-link` invocations return `available=false` with `reject_code=LOCAL_DISABLED` without requiring authentication, reading cached availability, or calling the Handoff API.
- Added compile-time enterprise CLI extensions without requiring a repository fork: public `cmd.Execute` / `cmd.ExecuteWithVersion` entry points, Credential providers, a single Transport interceptor, and Platform plugins for command observation, wrapping, lifecycle hooks, and restrictions.
- Added `meegle extension doctor|credentials|transport|plugins|policy|discovery` diagnostics that expose non-secret registration, compatibility, selector, rule, transport-baseline, and isolated dynamic-tool metadata.
- Added standalone no-extension and enterprise binaries under `examples/`, including public-module build and end-to-end MCP/OAuth/governance coverage.
- Added wire-level `tools/list` metadata parsing so previously unknown MCP tools with `metadata.resource` and `metadata.method` become executable dynamic commands in both CLI and SDK registries.
- Dynamic discovery now isolates malformed, oversized, duplicate, flag-conflicting, and static-command-shadowing tools per entry; known fallback paths remain immutable, help text is sanitized, Registry rebuild state is published coherently, and SDK callers can inspect skipped entries through `Client.DiscoveryIssues()`.
- Unknown tools without metadata now report a stable `missing_mapping` discovery issue in both CLI and SDK diagnostics instead of being silently omitted.
- Nullable JSON Schema parameter types such as `["string", "null"]` now remain available in CLI and SDK discovery; unions with multiple non-null types report `unsupported_schema_union` without affecting valid sibling tools.

### Changed

- Renamed the Handoff commands to `ai-handoff availability` and `ai-handoff create-link`; the earlier plus-prefixed forms are no longer registered.
- `ai-handoff create-link` keeps the CLI/facade `user_query/related_context` contract and the CLI flags `--query`/`--related-context`; facade converts it to the AI `query/entities` contract internally. Public context payloads use business identifiers (`project_key`, `work_item_type_key`, `work_item_id`, `view_id`, `chart_id`) instead of exposing AI's generic `key`; View keeps `work_item_type_key` optional.
- Successful `ai-handoff create-link` responses now replace the returned URL host with the active CLI login host while preserving the scheme, path, query, and fragment.
- Expanded local-command help with full `meegle` usage paths, required-flag markers, behavior/parameter details, and examples; `meegle inspect` now includes local and nested commands such as `ai-handoff create-link` and `preference handoff auto`.

### Security

- Transport extensions apply a 30-second timeout to provider and hook callbacks without shortening the caller-owned MCP, OAuth, or attachment request lifetime. They retain a 10-redirect limit and HTTPS downgrade protection, including when inherited redirect callbacks mutate both the destination and redirect history. Requests rejected before reaching the base transport now close their bodies, and extension callback panics are converted to controlled failures.
- MCP requests using a custom token header remove stale static credential headers and reject cross-origin redirects against an immutable source snapshot, so credentials cannot be duplicated or copied to another HTTPS origin.
- Hand-written Platform plugins that declare command restrictions with a fail-open policy now fail CLI startup before `Install` runs instead of silently disabling all of that plugin's policy rules.
- Platform metadata/Install and Startup callbacks now have a two-second safety boundary. Fail-open timeouts do not block later plugins, fail-closed timeouts stop execution, and timed-out Install callbacks cannot commit late registrations.
- Transport pre-hooks that replace a request Body now release both the original and replacement streams; post-hook TLS snapshots deep-clone certificate objects so extensions cannot mutate live response metadata.
- Extension startup and runtime failures retain safe `errors.Is` / `errors.As` matching without reflecting callback causes or panic values into public output; callback errors whose custom `Is`, `As`, `Unwrap`, `Error`, or payload methods panic are contained at Credential, Transport, Platform, formatter, and process-entry boundaries, while runtime and explicit abort failures expose stable structured error codes.

### Compatibility

- Broken profile variables now produce `CONFIG_ENV_UNRESOLVED` for dynamic business commands with or without a discovery cache, `inspect --profile` consistently uses the selected profile, and extension diagnostics report frozen resolution states instead of re-running providers.

- The no-extension wrapper is regression-tested against the official binary, and the official binary is checked against a pinned `main@6326b7d` contract for legacy help, version, completion, authentication status, output, and exit-code behavior. CLI extensions remain isolated from SDK clients.
- Restrict policy denials honor explicit structured output modes and expose the stable `CLIENT_COMMAND_DENIED` error envelope.
- The legacy `--version` flag is routed through the governed `version` command, and destructive WBS publish/reset operations are classified as `high-risk-write` for enterprise policy enforcement.
- Generated open-source trees are required to pass both `go build ./...` and `go test ./...`; source-only sync tooling and its tests are excluded from the published repository together.

### Fixed

- Tool discovery and server-side CLI configuration caches now share one profile-aware JSON file cache with atomic replacement, preventing concurrent CLI processes from exposing partially written cache files.
- Remote catalog resources can no longer shadow locally owned root commands such as `ai-handoff` and `preference` and abort the complete CLI startup; conflicting tools are isolated as `reserved_path` discovery issues while unrelated dynamic commands remain available.
- Map Facade invalid-parameter envelopes, including handoff query/context limit violations, to non-retryable `HANDOFF_API_INVALID_PARAM` errors and replace internal biz error IDs/causes/chains with a concise user-safe message and availability hint.
- Credential and Platform failures raised before CLI App construction now honor explicit JSON/NDJSON output and retain their stable error codes; the published readonly enterprise policy keeps `extension/**` diagnostics available for troubleshooting.
- Broken `${VAR}` profile placeholders no longer lock users out of help, version, login help, or configuration repair commands; credential-dependent business commands still fail with `CONFIG_ENV_UNRESOLVED`. Known local/recovery commands also defer Credential Provider resolution, so slow or unavailable OIDC providers no longer block them, while dynamic business commands remain fail-closed on ordinary errors, timeouts, and `BlockError`. A literal `--version` consumed as another flag's value is no longer rewritten as the version command. `dev` builds still reject non-empty `RequireCLI` constraints but now point enterprise developers to `ExecuteWithVersion` in the compatibility error chain.
- Transport extensions no longer cancel successful HTTP requests when `RoundTrip` returns: delayed `tools/list` and `tools/call` bodies remain readable until closed, invalid URLs produced by pre-hooks fail closed instead of panicking, and post-hooks receive body-free metadata snapshots so timeouts release the live response immediately without a response-body race or connection leak.
- Credential-bearing MCP requests now freeze the exact original origin for both default Bearer and custom token headers, preventing HTTPS downgrade, port-change, cross-domain forwarding, and multi-hop redirect-history mutation from leaking credentials.
- Credential redirect guards now retain the standard 10-hop limit even when they install a custom redirect callback; all JSON-RPC responses are bounded before decoding (`tools/list` at 8 MiB, other calls at 32 MiB); Transport post-hooks receive a cloned TLS connection state instead of a pointer into the live response.
- Multiple Restrict rules now compose as cumulative constraints; wrappers cannot return success after skipping, delaying, or duplicating `next`; a token-only Credential provider cannot hide a broken built-in configuration; first-run setup uses the Profile selected by the Credential provider; first-run completion is treated as success by Shutdown hooks and process entries without hiding fail-closed Shutdown failures; and spaced `RequireCLI` comparators such as `>= 1.2.0` are accepted.
- Wrappers can no longer turn a non-nil downstream command error into exit 0 by ignoring the result of synchronous or awaited asynchronous `next` calls.

## [v1.0.20] - 2026-08-20

### Changed

- Updated the bundled Meegle Agent Skill to the v5.4 guidance, including expanded MQL syntax, API examples, error handling, and work-item operation SOPs.

### Fixed

- Commands now report all missing required flags and positional arguments in one `CLIENT_MISSING_REQUIRED` error, in stable command-definition order, instead of failing one parameter at a time. Single-item wording, exit code, retryability, and help suggestions remain unchanged; JSON, NDJSON, and table error output all preserve the complete list.

## [v1.0.19] - 2026-08-04

### Fixed

- JSON and NDJSON output now keep URL query separators such as `&` literal instead of escaping them as `\u0026`, so returned login and verification URLs can be copied directly.

## [v1.0.18] - 2026-07-24

### Added

- Added `meegle --version` as an alias of `meegle version`, with identical plain version output.

### Fixed

- Required command parameters supplied through `--params` (for example `work_item_type` and `project_key` on `workitem create`) now pass CLI validation and are forwarded to the MCP tool. Both MCP `snake_case` and CLI `kebab-case` key forms are accepted. Previously Cobra rejected them before the CLI's parameter-merge pipeline ran, and the snake_case form was not matched to generated CLI flags.
- Programmatic command-string entry points now decode `\n` as real line breaks and preserve backslashes in unsupported escape sequences, preventing multiline comments from being stored with literal `nn`.

## [v1.0.17] - 2026-07-20

### Fixed

- Serialized store-backed OAuth refresh across CLI processes, rejected HTTP 200 error envelopes that omit `access_token`, retried requests with the newly persisted token, and guarded terminal-401 cleanup so a stale process can no longer overwrite or delete fresh credentials ([#40](https://github.com/larksuite/meegle-cli/issues/40)).

## [v1.0.16] - 2026-07-07

### Fixed

- The v1.0.15 package metadata and docs were updated, but its bundled binaries still came from v1.0.9.

## [v1.0.15] - 2026-07-07

### Added

- New `--auto-paginate` global flag. When set, the CLI inspects the first response for pagination signals (`next_page_token` or `pagination.has_more`) and automatically fetches subsequent pages, concatenating list arrays into a single merged payload. Supports both token-based and `page_num`-based pagination. A 200-page safety cap and a 3-consecutive-empty-page streak guard prevent runaway loops against stuck cursors; when the cap is hit the merged result is returned with `truncated: true` and a continuation token/page number in `meta`, alongside a stderr hint. Batch (`+batch-get`) and attachment shortcut commands are exempt — they manage their own multi-call execution paths.

## [v1.0.14] - 2026-07-01

### Fixed

- Restored the valid npm quick-start command to `npx @lark-project/meegle@latest install`. `npx @lark-project/meegle@latest` already invokes the package's `meegle` binary, so appending `meegle install` passes an invalid extra `meegle` subcommand to the CLI.

## [v1.0.13] - 2026-06-30

### Fixed

- Corrected the npm quick-start commands for the setup wizard. This was superseded by v1.0.14 after verifying the package invocation semantics across npx versions.

## [v1.0.12] - 2026-06-29

### Fixed

- The install wizard now always installs the AI Agent Skill instead of trying to detect a prior install. The previous name-substring check matched sibling skills such as `meegle-plugin` and silently skipped installing the core `meegle` skill. `skills add` is idempotent, so re-running it on an already-installed skill simply upgrades or no-ops.

## [v1.0.11] - 2026-06-26

### Added

- `meegle install` now runs a one-stop setup wizard for npm users: install or upgrade the CLI globally, install the AI Agent Skill, configure the host, and start browser or Device Code login. This enables `npx @lark-project/meegle@latest install` as the primary quick-start command.

### Fixed

- The install wizard now invokes the `skills` CLI through `npm exec --package=skills` so `skills add` and `skills ls` are parsed reliably across npm/npx versions. A real isolated install smoke test previously exposed that `npx -y skills add ...` could be misparsed as `add@latest`.

## [v1.0.10] - 2026-06-25

### Fixed

- `mywork todo` now adds an actionable troubleshooting suggestion when the backend returns `get action info fail`, pointing users to refresh command metadata and pass `--asset-key` for multi-workspace accounts.
- Meegle runtime flags such as `--refresh` and `--profile` are no longer forwarded as backend tool parameters when passed explicitly on dynamic commands.

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
