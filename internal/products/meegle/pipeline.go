// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"net/http"

	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// newPipelineFactory assembles the stable Meegle command lifecycle. Backend
// differences live behind CommandRuntime; mutually exclusive executors are not
// modeled as serial pipeline steps.
func newPipelineFactory(setup *DynamicRegistrySetup, identity *ResolvedIdentity, httpClient *http.Client) cliapp.PipelineFactory {
	return func(cfg cliapp.Config) (*pipeline.Pipeline, error) {
		session := &SessionStep{Identity: identity, HTTPClient: httpClient}
		resolver := NewCommandRuntimeResolver(
			&MCPRuntime{Session: session, CommandsFunc: setup.MappedCommands},
			&CLIAPIRuntime{Session: session},
		)
		return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
			&pipeline.ParamMergeStep{},
			&StructuredFlagNameNormalizeStep{},
			&RuntimeValidateStep{Resolver: resolver},
			&RuntimeSessionStep{Resolver: resolver},
			&RuntimeExecutorStep{Resolver: resolver},
			&pipeline.OutputStep{Processor: meegleOutputProcessor()},
		}}, nil
	}
}
