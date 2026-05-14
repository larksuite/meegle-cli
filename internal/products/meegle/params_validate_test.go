// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"reflect"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

func newValidateState(paramTypesJSON, fixedParamsJSON string) *pipeline.PipelineContext {
	tags := map[string]string{}
	if paramTypesJSON != "" {
		tags["mcp_param_types"] = paramTypesJSON
	}
	if fixedParamsJSON != "" {
		tags["mcp_fixed_params"] = fixedParamsJSON
	}
	return &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{Meta: registry.NodeMeta{Tags: tags}},
		},
	}
}

func TestFindUnknownParams_AllValid(t *testing.T) {
	state := newValidateState(`{"work-item-id":"string","project-key":"string"}`, "")
	params := map[string]any{"work_item_id": "1", "project_key": "p-1"}
	if got := findUnknownParams(state, params); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFindUnknownParams_OneUnknown(t *testing.T) {
	state := newValidateState(`{"work-item-id":"string","project-key":"string"}`, "")
	params := map[string]any{"work_item_id": "1", "name": "x"}
	want := []string{"name"}
	if got := findUnknownParams(state, params); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindUnknownParams_MultipleUnknownSorted(t *testing.T) {
	state := newValidateState(`{"work-item-id":"string"}`, "")
	params := map[string]any{"work_item_id": "1", "priority": "P1", "assignee": "u-1"}
	want := []string{"assignee", "priority"}
	if got := findUnknownParams(state, params); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindUnknownParams_NoSchemaSkips(t *testing.T) {
	// Defensive: missing schema must NOT trigger false-positive warnings.
	state := newValidateState("", "")
	params := map[string]any{"anything": "goes"}
	if got := findUnknownParams(state, params); got != nil {
		t.Errorf("expected nil when schema absent, got %v", got)
	}
}

func TestFindUnknownParams_MalformedSchemaSkips(t *testing.T) {
	state := newValidateState(`not-json`, "")
	params := map[string]any{"anything": "goes"}
	if got := findUnknownParams(state, params); got != nil {
		t.Errorf("expected nil for malformed schema, got %v", got)
	}
}

func TestFindUnknownParams_FixedParamsAreValid(t *testing.T) {
	// mcp_fixed_params keys are injected at registry level (sugar / batch
	// commands) — they must not be flagged as unknown.
	state := newValidateState(`{"work-item-id":"string"}`, `{"action":"this_week"}`)
	params := map[string]any{"work_item_id": "1", "action": "this_week"}
	if got := findUnknownParams(state, params); got != nil {
		t.Errorf("fixed params should be valid, got %v", got)
	}
}

func TestFindUnknownParams_NilState(t *testing.T) {
	if got := findUnknownParams(nil, map[string]any{"x": 1}); got != nil {
		t.Errorf("expected nil for nil state, got %v", got)
	}
}

func TestFormatUnknownParamsWarning_Empty(t *testing.T) {
	if got := formatUnknownParamsWarning("update_workitem", nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFormatUnknownParamsWarning_Single(t *testing.T) {
	got := formatUnknownParamsWarning("update_node_subtask", []string{"name"})
	for _, want := range []string{"warning", "update_node_subtask", "name", "--help"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning missing %q, got %q", want, got)
		}
	}
}

func TestFormatUnknownParamsWarning_Multiple(t *testing.T) {
	got := formatUnknownParamsWarning("tool", []string{"a", "b"})
	if !strings.Contains(got, "a, b") {
		t.Errorf("expected comma-separated keys, got %q", got)
	}
}
