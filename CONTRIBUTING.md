# Contributing

Thank you for your interest in contributing to Meegle CLI.

## Prerequisites

- Go 1.21+
- Node.js >= 16 (required for NPM distribution builds)

## Setup

After cloning, run once to enable the pre-commit hook:

```bash
make setup
```

This configures a git hook that automatically adds the license header to new source files on commit.

## Development

```bash
# Build (meegle + meego-cli with dev ldflags)
make build

# Build meegle only
go build ./cmd/meegle

# Run all tests
go test ./...

# Cross-compile all platforms (outputs to npm/meegle/bin/)
make meegle-all

# Lint
go vet ./...
```

## Directory Structure

- `cmd/meegle/` — CLI entry point
- `internal/products/meegle/` — Meegle product implementation
- `pkg/framework/` — Multi-product CLI framework
- `npm/meegle/` — NPM distribution package

## License Header

All source files must include the following header:

```
// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT
```

The pre-commit hook adds it automatically. To check manually: `make lint-license`.

## Commit Convention

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `refactor:`, `test:`
