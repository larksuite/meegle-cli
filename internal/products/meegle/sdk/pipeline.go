// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/integrations/formatting"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// sdkInjectStep pre-injects the MCP endpoint used by the Facade command-string
// SDK. This SDK is MCP-only and must not expose local CLI API commands.
type sdkInjectStep struct {
	cfg *ClientConfig
}

func (s *sdkInjectStep) Name() string { return "sdk_inject" }

func (s *sdkInjectStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state.OutputConfig == nil {
		state.OutputConfig = make(map[string]any)
	}
	token := s.cfg.resolveToken()
	state.OutputConfig["mcp.host"] = s.cfg.Host
	state.OutputConfig["mcp.headers"] = s.cfg.Headers
	state.OutputConfig["mcp.token"] = token
	state.OutputConfig["mcp.server_url"] = s.cfg.serverURL()
	state.OutputConfig["mcp.injected"] = true
	if s.cfg.UserAgent != "" {
		state.OutputConfig["mcp.user_agent"] = s.cfg.buildUserAgent()
	}
	return nil
}

// newSDKPipelineFactory builds an SDK-specific pipeline factory.
// Inserts sdkInjectStep before the standard pipeline steps.
func newSDKPipelineFactory(cfg *ClientConfig, setup *meegle.DynamicRegistrySetup) cliapp.PipelineFactory {
	var commandsFunc func() []types.MappedCommand
	if setup != nil {
		commandsFunc = setup.MappedCommands
	}
	// Keep the SDK runtime MCP-only even though the npm CLI configures both MCP
	// and CLI API runtimes. The registry above this pipeline contains only
	// dynamically discovered MCP commands, so a CLI API runtime is deliberately
	// absent here.
	resolver := meegle.NewCommandRuntimeResolver(
		&meegle.MCPRuntime{CommandsFunc: commandsFunc},
		nil,
	)
	return func(appCfg cliapp.Config) (*pipeline.Pipeline, error) {
		return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
			&sdkInjectStep{cfg: cfg},
			&pipeline.ParamMergeStep{},
			&meegle.StructuredFlagNameNormalizeStep{},
			&meegle.RuntimeValidateStep{Resolver: resolver},
			&meegle.RuntimeSessionStep{Resolver: resolver},
			&meegle.RuntimeExecutorStep{Resolver: resolver},
			&pipeline.OutputStep{Processor: formatting.DefaultProcessor()},
		}}, nil
	}
}
