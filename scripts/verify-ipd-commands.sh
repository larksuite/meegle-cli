#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT

# verify-ipd-commands.sh
#
# Verifies the 11 IPD MCP commands added by feat/ipd-mcp-cli-commands:
#   deliverable list
#   resource create / meta-fields
#   wbs list-draft-rows / list-instance-rows / create-draft / edit-draft
#       / publish-draft / reset-draft / get-draft-progress
#       / list-element-templates
#
# Three phases (no backend mutation):
#   1. availability — `<cmd> --help` exits 0 (proves dynamic-discovery + mapper wired)
#   2. schema       — `meegle inspect <res>.<sub>` prints required/optional params
#   3. dry-run      — `<cmd> --dry-run` returns the normalized backend payload
#                     so the caller can eyeball it against the MCP tool's input
#                     schema (printed in the same block).
#
# Pure smoke test. End-to-end CLI-vs-MCP structural comparison lives in
# `test/crosstest/ipd_test.go` (requires real auth + project access).

set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &>/dev/null && pwd -P)"
cd "$ROOT"

BIN="${BIN:-./dist/meegle}"
PROJECT_KEY="${PROJECT_KEY:-664474a4fac0ad797f358562}"   # crosstest default project
WORK_ITEM_TYPE="${WORK_ITEM_TYPE:-story}"
WORK_ITEM_ID_DEFAULT="${WBS_TEST_WORK_ITEM_ID:-0000000000}"

# Build only if missing — fast no-op on repeat runs.
if [ ! -x "$BIN" ]; then
    echo "==> Building $BIN"
    make build >/dev/null
fi

# Each row:  <resource> <sub> | <MCP tool>            | <dry-run-args>
#
# Mutating commands omit a dry-run args column so they only get availability +
# schema checks (we never want to accidentally create / publish / reset against
# a real workitem from a smoke test). Use `--params @file.json` manually when
# you need to send real payloads.
SPEC=$(cat <<EOF
deliverable list                 | list_deliverables                | --project-key=$PROJECT_KEY
resource create                  | create_resource_work_item        |
resource meta-fields             | get_resource_work_item_type_conf | --project-key=$PROJECT_KEY --work-item-type-key=$WORK_ITEM_TYPE
wbs list-draft-rows              | list_wbs_draft_rows              | --project-key=$PROJECT_KEY --work-item-id=$WORK_ITEM_ID_DEFAULT
wbs list-instance-rows           | list_wbs_instance_rows           | --project-key=$PROJECT_KEY --work-item-id=$WORK_ITEM_ID_DEFAULT
wbs create-draft                 | create_wbs_draft                 |
wbs edit-draft                   | edit_wbs_draft                   |
wbs publish-draft                | publish_wbs_draft                |
wbs reset-draft                  | reset_wbs_draft                  |
wbs get-draft-progress           | get_wbs_draft_operation_progress |
wbs list-element-templates       | list_element_template            | --project-key=$PROJECT_KEY --work-item-type=$WORK_ITEM_TYPE --element-type=node
EOF
)

# Pretty printer.
hr() { printf '%.0s─' {1..80}; echo; }
# Trim leading + trailing whitespace from $1.
trim() { local s="$1"; s="${s#"${s%%[![:space:]]*}"}"; s="${s%"${s##*[![:space:]]}"}"; printf '%s' "$s"; }
ok=0; fail=0; skipped_dry=0
declare -a failures=()

phase1_availability() {
    echo "Phase 1 — availability (--help exits 0, proves dynamic discovery + mapper)"
    hr
    while IFS='|' read -r cmd _ _; do
        cmd="$(trim "$cmd")"
        if "$BIN" $cmd --help >/dev/null 2>&1; then
            printf "  ✓ %-32s\n" "$cmd"
            ok=$((ok+1))
        else
            printf "  ✗ %-32s [help FAIL]\n" "$cmd"
            fail=$((fail+1))
            failures+=("availability: $cmd")
        fi
    done <<< "$SPEC"
    echo
}

phase2_schema() {
    echo "Phase 2 — schema introspection (CLI flags + MCP tool name)"
    hr
    while IFS='|' read -r cmd mcp _; do
        cmd="$(trim "$cmd")"
        mcp="$(trim "$mcp")"
        res="${cmd%% *}"
        sub="${cmd#* }"
        echo "  [$cmd]   →   MCP tool: $mcp"
        if ! "$BIN" inspect "${res}.${sub}" 2>&1 | sed 's/^/    /'; then
            failures+=("schema: $cmd")
            fail=$((fail+1))
        fi
        echo
    done <<< "$SPEC"
}

phase3_dryrun() {
    echo "Phase 3 — dry-run payload (only for commands with safe read-only defaults)"
    hr
    while IFS='|' read -r cmd mcp args; do
        cmd="$(trim "$cmd")"
        mcp="$(trim "$mcp")"
        args="$(trim "$args")"
        if [ -z "$args" ]; then
            printf "  ⊘ %-32s [skipped: mutating or needs WBS-enabled workitem]\n" "$cmd"
            skipped_dry=$((skipped_dry+1))
            continue
        fi
        if out=$("$BIN" $cmd $args --dry-run --format json 2>&1); then
            printf "  ✓ %-32s [dry-run OK]\n" "$cmd"
            # Show the request shape — what would be sent to MCP tool $mcp.
            echo "$out" | sed 's/^/      /' | head -20
            ok=$((ok+1))
        else
            printf "  ✗ %-32s [dry-run FAIL]\n" "$cmd"
            echo "$out" | sed 's/^/      /' | head -10
            fail=$((fail+1))
            failures+=("dry-run: $cmd")
        fi
        echo
    done <<< "$SPEC"
}

phase1_availability
phase2_schema
phase3_dryrun

echo
hr
echo "Summary"
echo "  ok       : $ok"
echo "  failed   : $fail"
echo "  dry-skip : $skipped_dry  (mutating / needs WBS-enabled workitem)"
if [ "$fail" -gt 0 ]; then
    echo
    echo "Failures:"
    printf '  - %s\n' "${failures[@]}"
    echo
    echo "For end-to-end CLI-vs-MCP structural comparison run:"
    echo "  go test -tags=crosstest ./test/crosstest/ -run TestIPDCrossTest -v"
    exit 1
fi

echo
echo "All ${ok} checks passed."
echo
echo "Next: run the structural-compare crosstest against a live MCP server:"
echo "  go test -tags=crosstest ./test/crosstest/ -run TestIPDCrossTest -v"
