// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"context"
	"io"
	"os"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

type Config struct {
	AppName          string
	Version          string
	Setup            registry.RegistrySetup
	Manager          *registry.RegistryManager
	Executor         executor.Executor
	Pipeline         *pipeline.Pipeline
	PipelineFactory  PipelineFactory
	OutputProcessor  *frameworkoutput.Processor
	Materializer     frameworkadapter.InputMaterializer
	Stdout           io.Writer
	Stderr           io.Writer
	RootCustomizer   RootCommandCustomizer
	BeforeExecute    BeforeExecuteHook
	AfterExecute     AfterExecuteHook
	ContextDecorator ContextDecorator
}

// WithContextDecorator decorates only terminal CLI executions. Programmatic
// Invoke/ExecuteRaw calls intentionally retain their caller-owned context.
func WithContextDecorator(decorator func(context.Context) context.Context) Option {
	return func(cfg *Config) { cfg.ContextDecorator = decorator }
}

type Option func(*Config)

func defaultConfig() Config {
	return Config{
		AppName:      "meego-cli",
		Version:      "dev",
		Materializer: frameworkadapter.DefaultMaterializer{},
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}
}

func WithAppName(name string) Option {
	return func(cfg *Config) { cfg.AppName = name }
}

func WithVersion(version string) Option {
	return func(cfg *Config) { cfg.Version = version }
}

func WithSetup(setup registry.RegistrySetup) Option {
	return func(cfg *Config) { cfg.Setup = setup }
}

func WithManager(manager *registry.RegistryManager) Option {
	return func(cfg *Config) { cfg.Manager = manager }
}

func WithExecutor(exec executor.Executor) Option {
	return func(cfg *Config) { cfg.Executor = exec }
}

func WithPipeline(pipe *pipeline.Pipeline) Option {
	return func(cfg *Config) { cfg.Pipeline = pipe }
}

func WithPipelineFactory(factory PipelineFactory) Option {
	return func(cfg *Config) { cfg.PipelineFactory = factory }
}

func WithOutputProcessor(processor *frameworkoutput.Processor) Option {
	return func(cfg *Config) { cfg.OutputProcessor = processor }
}

func WithMaterializer(materializer frameworkadapter.InputMaterializer) Option {
	return func(cfg *Config) { cfg.Materializer = materializer }
}

func WithStdout(stdout io.Writer) Option {
	return func(cfg *Config) { cfg.Stdout = stdout }
}

func WithStderr(stderr io.Writer) Option {
	return func(cfg *Config) { cfg.Stderr = stderr }
}

func WithRootCommandCustomizer(customizer RootCommandCustomizer) Option {
	return func(cfg *Config) { cfg.RootCustomizer = customizer }
}

func WithExecutionHooks(before BeforeExecuteHook, after AfterExecuteHook) Option {
	return func(cfg *Config) {
		cfg.BeforeExecute = before
		cfg.AfterExecute = after
	}
}
