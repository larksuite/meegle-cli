# `pkg/framework/router`

Canonical command-routing contracts live here.

- `ParsedCommand` is the framework-standard output from routing.
- Structured parameter merge helpers for `--set` / `--params` live here.
- The concrete `cobra`-backed `CommandRouter` is introduced in Phase 3.
- Transitional parser package has been removed; test/programmatic parsing uses `router.ParseInput`.