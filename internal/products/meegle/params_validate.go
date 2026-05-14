// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

// findUnknownParams returns sorted snake_case keys in snakeParams that are
// not declared as flags of the current MCP tool. Returns nil when no schema
// info is available (defensive: avoid false positives on malformed cache).
func findUnknownParams(state *pipeline.PipelineContext, snakeParams map[string]any) []string {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	raw := state.Parsed.Node.Meta.Tags["mcp_param_types"]
	if raw == "" {
		return nil
	}
	var paramTypes map[string]string
	if err := json.Unmarshal([]byte(raw), &paramTypes); err != nil {
		return nil
	}
	valid := make(map[string]struct{}, len(paramTypes))
	for kebab := range paramTypes {
		valid[strings.ReplaceAll(kebab, "-", "_")] = struct{}{}
	}
	// Fixed params injected at registry level are also legitimate keys.
	if rawFixed := state.Parsed.Node.Meta.Tags["mcp_fixed_params"]; rawFixed != "" {
		var fixed map[string]any
		if err := json.Unmarshal([]byte(rawFixed), &fixed); err == nil {
			for k := range fixed {
				valid[k] = struct{}{}
			}
		}
	}
	var unknown []string
	for k := range snakeParams {
		if _, ok := valid[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// formatUnknownParamsWarning composes a single-line stderr-friendly warning.
// Returns "" when the unknown list is empty so callers can branch cheaply.
func formatUnknownParamsWarning(toolName string, unknown []string) string {
	if len(unknown) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"warning: unknown argument(s) for %s: %s — sent to backend as-is and likely ignored. See --help for valid flags.",
		toolName, strings.Join(unknown, ", "),
	)
}
