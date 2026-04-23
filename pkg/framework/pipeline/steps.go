// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pipeline

import (
	"context"
	"fmt"
	"strings"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

type ParamMergeStep struct{}

func (s *ParamMergeStep) Name() string { return "param_merge" }

func (s *ParamMergeStep) Execute(ctx context.Context, state *PipelineContext) error {
	_ = ctx
	if state == nil || state.Parsed == nil {
		return frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, "parsed command is required")
	}
	if state.Parsed.ExplicitFlags == nil {
		state.Parsed.ExplicitFlags = make(map[string]any)
	}
	merged, err := frameworkrouter.MergeStructuredParams(state.Parsed.Flags, state.Parsed.ExplicitFlags)
	if err != nil {
		return err
	}
	state.Parsed.Flags = merged
	return nil
}

type ParamValidateStep struct{}

func (s *ParamValidateStep) Name() string { return "param_validate" }

func (s *ParamValidateStep) Execute(ctx context.Context, state *PipelineContext) error {
	_ = ctx
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, "parsed command is required")
	}
	node := state.Parsed.Node
	flags := append(node.InheritedFlags(), node.Flags...)
	for _, flag := range flags {
		if flag.Required {
			if _, ok := state.Parsed.Flags[flag.Name]; !ok {
				return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamRequired, fmt.Sprintf("missing required parameter --%s", flag.Name))
			}
		}
		if len(flag.Enum) > 0 {
			if value, ok := state.Parsed.Flags[flag.Name]; ok && !containsEnum(flag.Enum, fmt.Sprint(value)) {
				return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, fmt.Sprintf("invalid value for --%s", flag.Name))
			}
		}
	}
	for index, arg := range node.Args {
		if !arg.Required {
			continue
		}
		if index >= len(state.Parsed.Args) {
			return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamRequired, fmt.Sprintf("missing required argument <%s>", arg.Name))
		}
	}
	return nil
}

type ExecutorStep struct {
	Executor executor.Executor
}

func (s *ExecutorStep) Name() string { return "executor" }

func (s *ExecutorStep) Execute(ctx context.Context, state *PipelineContext) error {
	if state == nil || state.Parsed == nil {
		return frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, "parsed command is required")
	}
	values := BuildInputValues(state.Parsed)
	state.Values = values
	if isDryRun(state.Parsed) {
		state.Result = &executor.RawResult{
			Data:     BuildNormalizedRequest(state.Input, state.Parsed, values),
			Metadata: map[string]any{"tool": "meta.dry_run", "dry_run": true},
		}
		return nil
	}
	if s == nil || s.Executor == nil {
		return frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "executor is not configured")
	}
	req := &executor.Request{
		Command: state.Parsed,
		Values:  values,
		Meta: executor.RequestMeta{
			DryRun:  isDryRun(state.Parsed),
			Output:  stringValue(state.Parsed.Flags["format"]),
			Select:  stringValue(state.Parsed.Flags["select"]),
			Verbose: boolValue(state.Parsed.Flags["verbose"]),
		},
	}
	if state.Input != nil {
		req.Source = executor.RequestSource{
			Channel: state.Input.Source.Channel,
			Command: state.Input.Source.Command,
			Product: state.Input.Source.Product,
			Version: state.Input.Source.Version,
		}
	}
	result, err := s.Executor.Execute(ctx, req)
	if err != nil {
		return err
	}
	state.Result = result
	return nil
}

type OutputStep struct {
	Processor *frameworkoutput.Processor
}

func (s *OutputStep) Name() string { return "output" }

func (s *OutputStep) Execute(ctx context.Context, state *PipelineContext) error {
	if s == nil || s.Processor == nil || state == nil || state.Result == nil {
		return nil
	}
	return s.Processor.Process(ctx, &frameworkoutput.Context{
		Input:  state.Input,
		Parsed: state.Parsed,
		Values: state.Values,
		Result: state.Result,
	})
}

func BuildInputValues(parsed *frameworkrouter.ParsedCommand) map[string]any {
	values := make(map[string]any)
	if parsed == nil || parsed.Node == nil {
		return values
	}
	builtin := builtinFlagSet()
	for key, value := range parsed.Flags {
		if _, skip := builtin[strings.ToLower(key)]; skip {
			continue
		}
		values[key] = value
	}
	for index, arg := range parsed.Node.Args {
		if arg.Variadic {
			if index < len(parsed.Args) {
				values[arg.Name] = append([]string(nil), parsed.Args[index:]...)
			}
			break
		}
		if index < len(parsed.Args) {
			values[arg.Name] = parsed.Args[index]
		}
	}
	return values
}

func builtinFlagSet() map[string]struct{} {
	out := make(map[string]struct{}, len(registry.BuiltinFlags))
	for _, flag := range registry.BuiltinFlags {
		out[strings.ToLower(flag.Name)] = struct{}{}
	}
	return out
}

func containsEnum(candidates []string, actual string) bool {
	for _, candidate := range candidates {
		if candidate == actual {
			return true
		}
	}
	return false
}

func isDryRun(parsed *frameworkrouter.ParsedCommand) bool {
	if parsed == nil {
		return false
	}
	value, ok := parsed.Flags["dry-run"]
	if !ok {
		return false
	}
	dryRun, ok := value.(bool)
	return ok && dryRun
}

func BuildNormalizedRequest(input *frameworkadapter.RawInput, parsed *frameworkrouter.ParsedCommand, values map[string]any) *executor.NormalizedRequest {
	request := &executor.NormalizedRequest{Input: map[string]any{}}
	if parsed != nil && parsed.Node != nil {
		request.Tool = parsed.Node.HandlerRef
	}
	for key, value := range values {
		request.Input[key] = value
	}
	if parsed != nil {
		request.Meta = executor.RequestMeta{
			DryRun:  isDryRun(parsed),
			Output:  stringValue(parsed.Flags["format"]),
			Select:  stringValue(parsed.Flags["select"]),
			Verbose: boolValue(parsed.Flags["verbose"]),
		}
	}
	if input != nil {
		request.Source = executor.RequestSource{
			Channel: input.Source.Channel,
			Command: input.Source.Command,
			Product: input.Source.Product,
			Version: input.Source.Version,
		}
	}
	return request
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boolValue(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}
