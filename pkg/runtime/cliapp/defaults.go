// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"fmt"

	"github.com/spf13/cobra"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/output/formatter"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

type PipelineFactory func(cfg Config) (*pipeline.Pipeline, error)

type RootCommandMetadata struct {
	AppName string
	Version string
}

type RootCommandCustomizer func(root *cobra.Command, meta RootCommandMetadata)

func DefaultOutputProcessor() *frameworkoutput.Processor {
	return formatter.DefaultProcessor()
}

func DefaultPipelineFactory(cfg Config) (*pipeline.Pipeline, error) {
	return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
		&pipeline.ParamMergeStep{},
		&pipeline.ParamValidateStep{},
		&pipeline.ExecutorStep{Executor: cfg.Executor},
		&pipeline.OutputStep{Processor: cfg.OutputProcessor},
	}}, nil
}

func applyDefaultRootCommand(root *cobra.Command, meta RootCommandMetadata) {
	if root == nil {
		return
	}
	root.Short = "CLI application."
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), meta.Version)
			return err
		},
	})
}
