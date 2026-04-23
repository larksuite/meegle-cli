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

// sdkInjectStep pre-injects all MCP connection config into OutputConfig.
// Executes before SessionStep; SessionStep skips local reads when values are already present.
type sdkInjectStep struct {
	cfg *ClientConfig
}

func (s *sdkInjectStep) Name() string { return "sdk_inject" }

func (s *sdkInjectStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state.OutputConfig == nil {
		state.OutputConfig = make(map[string]any)
	}
	state.OutputConfig["mcp.host"] = s.cfg.Host
	state.OutputConfig["mcp.headers"] = s.cfg.Headers
	state.OutputConfig["mcp.token"] = s.cfg.resolveToken()
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
	return func(appCfg cliapp.Config) (*pipeline.Pipeline, error) {
		return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
			&sdkInjectStep{cfg: cfg},
			&pipeline.ParamMergeStep{},
			&meegle.MeegleValidateStep{},
			&meegle.SessionStep{},
			&meegle.McpExecutorStep{CommandsFunc: commandsFunc},
			&pipeline.OutputStep{Processor: formatting.DefaultProcessor()},
		}}, nil
	}
}
