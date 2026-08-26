// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

// CommandRuntime owns the backend-specific phases of one routed command.
// The product pipeline keeps the lifecycle order stable while the resolver
// selects exactly one runtime for all three phases.
type CommandRuntime interface {
	Validate(*pipeline.PipelineContext) error
	PrepareSession(context.Context, *pipeline.PipelineContext) error
	Execute(context.Context, *pipeline.PipelineContext) error
}

// CommandRuntimeResolver maps command metadata to one backend runtime. MCP is
// the compatibility default because dynamically discovered nodes predate the
// executor_kind metadata; local CLI API nodes always opt in explicitly.
type CommandRuntimeResolver struct {
	runtimes map[string]CommandRuntime
}

func NewCommandRuntimeResolver(mcpRuntime, cliAPIRuntime CommandRuntime) *CommandRuntimeResolver {
	return &CommandRuntimeResolver{runtimes: map[string]CommandRuntime{
		ExecutorKindMCP:    mcpRuntime,
		ExecutorKindCLIAPI: cliAPIRuntime,
	}}
}

func (resolver *CommandRuntimeResolver) Resolve(state *pipeline.PipelineContext) (CommandRuntime, error) {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil, nil
	}
	kind := state.Parsed.Node.Meta.Tags[TagExecutorKind]
	if kind == "" {
		kind = ExecutorKindMCP
	}
	if resolver == nil {
		return nil, runtimeResolutionError(kind)
	}
	runtime := resolver.runtimes[kind]
	if runtime == nil {
		return nil, runtimeResolutionError(kind)
	}
	return runtime, nil
}

func runtimeResolutionError(kind string) error {
	return meerrors.NewClientError(
		"CLIENT_MISCONFIGURED",
		fmt.Sprintf("command runtime %q is not configured", kind),
	)
}

type RuntimeValidateStep struct {
	Resolver *CommandRuntimeResolver
}

func (step *RuntimeValidateStep) Name() string { return "runtime_validate" }

func (step *RuntimeValidateStep) Execute(_ context.Context, state *pipeline.PipelineContext) error {
	runtime, err := step.Resolver.Resolve(state)
	if err != nil || runtime == nil {
		return err
	}
	return runtime.Validate(state)
}

type RuntimeSessionStep struct {
	Resolver *CommandRuntimeResolver
}

func (step *RuntimeSessionStep) Name() string { return "runtime_session" }

func (step *RuntimeSessionStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	runtime, err := step.Resolver.Resolve(state)
	if err != nil || runtime == nil {
		return err
	}
	return runtime.PrepareSession(ctx, state)
}

type RuntimeExecutorStep struct {
	Resolver *CommandRuntimeResolver
}

func (step *RuntimeExecutorStep) Name() string { return "runtime_executor" }

func (step *RuntimeExecutorStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	runtime, err := step.Resolver.Resolve(state)
	if err != nil || runtime == nil {
		return err
	}
	return runtime.Execute(ctx, state)
}

// MCPRuntime owns ordinary MCP calls and MCP-backed composite modes. Attachment
// shortcuts remain here because their prepare phase calls MCP; their file
// transfer phase uses signed HTTP and may move behind a dedicated runtime.
type MCPRuntime struct {
	Session      *SessionStep
	CommandsFunc func() []types.MappedCommand
}

func (runtime *MCPRuntime) Validate(state *pipeline.PipelineContext) error {
	return validateMeegleCommand(state)
}

func (runtime *MCPRuntime) PrepareSession(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil {
		return nil
	}
	if injected, _ := state.OutputConfig["mcp.injected"].(bool); injected {
		return nil
	}
	if err := runtime.sessionStep().Execute(ctx, state); err != nil {
		return err
	}
	projectResolvedSessionToMCP(state)
	return nil
}

func (runtime *MCPRuntime) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	if state.Parsed.Node.Meta.Tags[TagMcpBatch] == "1" {
		return (&BatchExecutorStep{CommandsFunc: runtime.CommandsFunc}).Execute(ctx, state)
	}
	if state.Parsed.Node.Meta.Tags[TagAttachmentShortcut] != "" {
		return (&AttachmentShortcutStep{}).Execute(ctx, state)
	}
	if err := (&McpExecutorStep{CommandsFunc: runtime.CommandsFunc}).Execute(ctx, state); err != nil {
		return err
	}
	return (&AutoPaginateStep{CommandsFunc: runtime.CommandsFunc}).Execute(ctx, state)
}

func (runtime *MCPRuntime) sessionStep() *SessionStep {
	if runtime != nil && runtime.Session != nil {
		return runtime.Session
	}
	return &SessionStep{}
}

func projectResolvedSessionToMCP(state *pipeline.PipelineContext) {
	if state == nil || state.OutputConfig == nil {
		return
	}
	for _, name := range []string{
		"host",
		"token",
		"headers",
		"identity_source",
		"access_token_header",
		"http_client",
		"user_agent",
		"store",
		"token_manager",
	} {
		if value, ok := state.OutputConfig["session."+name]; ok {
			state.OutputConfig["mcp."+name] = value
		}
	}
}
