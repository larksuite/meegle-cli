// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"testing"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

type recordingCommandRuntime struct {
	validateCalls int
	sessionCalls  int
	executeCalls  int
}

func (runtime *recordingCommandRuntime) Validate(*pipeline.PipelineContext) error {
	runtime.validateCalls++
	return nil
}

func (runtime *recordingCommandRuntime) PrepareSession(context.Context, *pipeline.PipelineContext) error {
	runtime.sessionCalls++
	return nil
}

func (runtime *recordingCommandRuntime) Execute(context.Context, *pipeline.PipelineContext) error {
	runtime.executeCalls++
	return nil
}

func TestCommandRuntimeLifecycleSelectsExactlyOneRuntime(t *testing.T) {
	mcpRuntime := &recordingCommandRuntime{}
	cliRuntime := &recordingCommandRuntime{}
	resolver := NewCommandRuntimeResolver(mcpRuntime, cliRuntime)
	state := runtimeTestState(ExecutorKindCLIAPI)

	steps := []pipeline.PipelineStep{
		&RuntimeValidateStep{Resolver: resolver},
		&RuntimeSessionStep{Resolver: resolver},
		&RuntimeExecutorStep{Resolver: resolver},
	}
	for _, step := range steps {
		if err := step.Execute(context.Background(), state); err != nil {
			t.Fatalf("%s.Execute() error = %v", step.Name(), err)
		}
	}

	if cliRuntime.validateCalls != 1 || cliRuntime.sessionCalls != 1 || cliRuntime.executeCalls != 1 {
		t.Fatalf("CLI API runtime calls = validate:%d session:%d execute:%d",
			cliRuntime.validateCalls, cliRuntime.sessionCalls, cliRuntime.executeCalls)
	}
	if mcpRuntime.validateCalls != 0 || mcpRuntime.sessionCalls != 0 || mcpRuntime.executeCalls != 0 {
		t.Fatalf("MCP runtime must not run for CLI API command: %#v", mcpRuntime)
	}
}

func TestCommandRuntimeResolverDefaultsDynamicCommandsToMCP(t *testing.T) {
	mcpRuntime := &recordingCommandRuntime{}
	resolver := NewCommandRuntimeResolver(mcpRuntime, &recordingCommandRuntime{})
	runtime, err := resolver.Resolve(runtimeTestState(""))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if runtime != mcpRuntime {
		t.Fatalf("Resolve() runtime = %T, want MCP runtime", runtime)
	}
}

func TestCommandRuntimeResolverRejectsUnknownRuntime(t *testing.T) {
	resolver := NewCommandRuntimeResolver(&recordingCommandRuntime{}, &recordingCommandRuntime{})
	_, err := resolver.Resolve(runtimeTestState("unknown"))
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) || meegleErr.Code != "CLIENT_MISCONFIGURED" {
		t.Fatalf("Resolve() error = %#v", err)
	}
}

func runtimeTestState(kind string) *pipeline.PipelineContext {
	return &pipeline.PipelineContext{Parsed: &router.ParsedCommand{Node: &registry.CommandNode{
		Name:       "command",
		HandlerRef: "handler",
		Meta: registry.NodeMeta{Tags: map[string]string{
			TagExecutorKind: kind,
		}},
	}}}
}
