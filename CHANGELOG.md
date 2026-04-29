# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
post-release changes accumulate under `[Unreleased]` and graduate to a
versioned section on each npm release.

## [Unreleased]

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
