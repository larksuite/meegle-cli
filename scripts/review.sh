#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT

# Pre-submit review script. Runs the same gates as CI locally so agents / humans
# can self-check before opening a PR. Fails fast on the first failing step.
#
# Usage: ./scripts/review.sh [--skip-test]
#   --skip-test   skip `make test` (useful for docs-only changes)

set -euo pipefail

cd "$(dirname "$0")/.."

SKIP_TEST=0
for arg in "$@"; do
  case "$arg" in
    --skip-test) SKIP_TEST=1 ;;
    -h|--help)
      sed -n '1,10p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 2
      ;;
  esac
done

step() {
  printf '\n\033[1;34m==> %s\033[0m\n' "$1"
}

step "lint"
make lint

step "lint-license"
make lint-license

step "lint-thirdparty"
make lint-thirdparty

if [[ "$SKIP_TEST" -eq 0 ]]; then
  step "test"
  make test
else
  step "test (skipped)"
fi

step "review OK"
