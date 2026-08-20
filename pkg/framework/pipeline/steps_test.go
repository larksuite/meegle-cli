// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pipeline

import (
	"context"
	"reflect"
	"testing"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

func TestPipelineStepsMergeValidateAndDispatch(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{
			Name:       "update",
			HandlerRef: "core.workitem.update",
			Args:       []registry.ArgDef{{Name: "id", Required: true}},
			Flags:      []registry.FlagDef{{Name: "query", Type: registry.FlagTypeString, Required: true}},
		},
		Args: []string{"WI-1"},
		Flags: map[string]any{
			"params": `{"fields":{"status":"done"}}`,
			"set":    []string{"fields.priority=1"},
			"query":  "abc",
			"format": "json",
		},
		ExplicitFlags: map[string]any{"query": "abc", "params": `{"fields":{"status":"done"}}`, "set": []string{"fields.priority=1"}, "format": "json"},
	}
	exec := executor.NewDirectExecutor(map[string]executor.Handler{
		"core.workitem.update": func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
			_ = ctx
			return &executor.RawResult{Data: req.Values}, nil
		},
	})
	pipe := &Pipeline{Steps: []PipelineStep{
		&ParamMergeStep{},
		&ParamValidateStep{},
		&ExecutorStep{Executor: exec},
	}}
	state := &PipelineContext{Parsed: parsed}
	if err := pipe.Execute(context.Background(), state); err != nil {
		t.Fatalf("execute pipeline: %v", err)
	}
	if state.Result == nil {
		t.Fatal("expected executor result")
	}
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", state.Result.Data)
	}
	if got := data["id"]; got != "WI-1" {
		t.Fatalf("id = %#v", got)
	}
	if got := data["query"]; got != "abc" {
		t.Fatalf("query = %#v", got)
	}
	fields, ok := data["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v", data["fields"])
	}
	if got := fields["status"]; got != "done" {
		t.Fatalf("status = %#v", got)
	}
	if _, exists := data["format"]; exists {
		t.Fatalf("builtin format flag should not be dispatched: %#v", data)
	}
}

func TestParamValidateStepAggregatesMissingRequiredInputs(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{
			Flags: []registry.FlagDef{
				{Name: "project-key", Required: true},
				{Name: "work-item-type", Required: true},
				{Name: "user-key", Required: true},
			},
			Args: []registry.ArgDef{
				{Name: "source", Required: true},
				{Name: "destination", Required: true},
			},
		},
		ExplicitFlags: map[string]any{"project-key": "demo"},
		Args:          []string{"input.json"},
	}

	err := (&ParamValidateStep{}).Execute(context.Background(), &PipelineContext{Parsed: parsed})
	if err == nil {
		t.Fatal("expected missing required input error")
	}
	if got, want := err.Error(), "missing required inputs: --work-item-type, --user-key, <destination>"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	cliErr := frameworkerrors.As(err)
	if cliErr == nil || cliErr.Code != frameworkerrors.CodeParamRequired {
		t.Fatalf("error = %#v, want %s", err, frameworkerrors.CodeParamRequired)
	}
	if cliErr.ExitCode() != 1 || cliErr.IsRetryable() {
		t.Fatalf("exit/retryable = %d/%v, want 1/false", cliErr.ExitCode(), cliErr.IsRetryable())
	}
}

func TestParamValidateStepPreservesSingleMissingMessage(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{Flags: []registry.FlagDef{{Name: "project-key", Required: true}}},
	}

	err := (&ParamValidateStep{}).Execute(context.Background(), &PipelineContext{Parsed: parsed})
	if err == nil {
		t.Fatal("expected missing required parameter error")
	}
	if got, want := err.Error(), "missing required parameter --project-key"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// Regression: keys contributed by --params must be promoted into ExplicitFlags
// so downstream steps that filter by ExplicitFlags do not silently drop them.
func TestParamMergeStepPromotesParamsKeysToExplicitFlags(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{Name: "update", HandlerRef: "core.update"},
		Flags: map[string]any{
			"work-item-id": "123",
			"params":       `{"role_operate":[{"op":"add","role_key":"PM"}]}`,
		},
		ExplicitFlags: map[string]any{
			"work-item-id": "123",
			"params":       `{"role_operate":[{"op":"add","role_key":"PM"}]}`,
		},
	}
	state := &PipelineContext{Parsed: parsed}
	if err := (&ParamMergeStep{}).Execute(context.Background(), state); err != nil {
		t.Fatalf("ParamMergeStep: %v", err)
	}
	if _, ok := state.Parsed.ExplicitFlags["role_operate"]; !ok {
		t.Fatalf("role_operate from --params missing in ExplicitFlags: %#v", state.Parsed.ExplicitFlags)
	}
	gotRO, _ := state.Parsed.ExplicitFlags["role_operate"].([]any)
	wantRO := []any{map[string]any{"op": "add", "role_key": "PM"}}
	if !reflect.DeepEqual(gotRO, wantRO) {
		t.Fatalf("role_operate value = %#v, want %#v", gotRO, wantRO)
	}
}

// Regression: keys contributed by --set must be promoted into ExplicitFlags,
// symmetric with --params, so downstream explicit filters treat them the same
// as a user-typed CLI flag.
func TestParamMergeStepPromotesSetKeysToExplicitFlags(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{Name: "update"},
		Flags: map[string]any{
			"set": []string{"name=foo"},
		},
		ExplicitFlags: map[string]any{
			"set": []string{"name=foo"},
		},
	}
	state := &PipelineContext{Parsed: parsed}
	if err := (&ParamMergeStep{}).Execute(context.Background(), state); err != nil {
		t.Fatalf("ParamMergeStep: %v", err)
	}
	if got := state.Parsed.ExplicitFlags["name"]; got != "foo" {
		t.Fatalf("ExplicitFlags[name] = %#v, want foo", got)
	}
	if got := state.Parsed.Flags["name"]; got != "foo" {
		t.Fatalf("Flags[name] = %#v, want foo", got)
	}
}

// Regression: explicit CLI flags must beat --params for the same key, and the
// CLI value must remain in ExplicitFlags untouched.
func TestParamMergeStepCLIFlagBeatsParams(t *testing.T) {
	parsed := &frameworkrouter.ParsedCommand{
		Node: &registry.CommandNode{Name: "update"},
		Flags: map[string]any{
			"name":   "from-cli",
			"params": `{"name":"from-params"}`,
		},
		ExplicitFlags: map[string]any{
			"name":   "from-cli",
			"params": `{"name":"from-params"}`,
		},
	}
	state := &PipelineContext{Parsed: parsed}
	if err := (&ParamMergeStep{}).Execute(context.Background(), state); err != nil {
		t.Fatalf("ParamMergeStep: %v", err)
	}
	if got := state.Parsed.Flags["name"]; got != "from-cli" {
		t.Fatalf("Flags[name] = %#v, want from-cli (CLI must beat --params)", got)
	}
	if got := state.Parsed.ExplicitFlags["name"]; got != "from-cli" {
		t.Fatalf("ExplicitFlags[name] = %#v, want from-cli", got)
	}
}
