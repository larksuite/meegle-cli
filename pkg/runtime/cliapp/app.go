// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

// AlreadyRenderedError wraps a pipeline error whose envelope has already
// been written to stderr by App.executeParsed via the OutputStep processor.
// CLI entry points should detect this wrapper (via errors.As) and exit with
// frameworkerrors.ExitCode(err) WITHOUT re-rendering the error text, so the
// user doesn't see both the formatted envelope and a plain-text duplicate.
type AlreadyRenderedError struct{ Err error }

type successfulExitError struct{ err error }

func (e *successfulExitError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *successfulExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.err)
}

// SuccessfulExit marks successful control flow that should reach the process
// entry without being rendered as an error envelope.
func SuccessfulExit(err error) error {
	if err == nil {
		return nil
	}
	return &successfulExitError{err: err}
}

// IsSuccessfulExit reports whether err carries a SuccessfulExit marker.
// Process entry points should map this control flow to exit code zero.
func IsSuccessfulExit(err error) bool {
	if err == nil {
		return false
	}
	var marker *successfulExitError
	return frameworkerrors.SafeAs(err, &marker)
}

func (e *AlreadyRenderedError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *AlreadyRenderedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.Err)
}

type App struct {
	manager *registry.RegistryManager
	router  *frameworkrouter.CommandRouter
	pipe    *pipeline.Pipeline
	config  Config
}

func New(opts ...Option) (*App, error) {
	config := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	manager := config.Manager
	if manager == nil {
		manager = registry.NewManager(config.Setup)
		if err := manager.Init(context.Background()); err != nil {
			return nil, err
		}
	}
	router, err := frameworkrouter.NewCommandRouter(manager, config.AppName)
	if err != nil {
		return nil, err
	}
	if config.OutputProcessor == nil {
		config.OutputProcessor = DefaultOutputProcessor()
	}
	pipe := config.Pipeline
	app := &App{manager: manager, router: router, config: config}
	if pipe == nil {
		factory := config.PipelineFactory
		if factory == nil {
			factory = DefaultPipelineFactory
		}
		pipe, err = factory(config)
		if err != nil {
			return nil, err
		}
	}
	app.pipe = pipe
	router.SetBuildFinalizer(func(root *cobra.Command) error {
		app.customizeRoot(root)
		return nil
	})
	if err := router.Build(app.executeParsed); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) RootCommand() *cobra.Command {
	if a == nil || a.router == nil {
		return nil
	}
	return a.router.RootCommand()
}

func (a *App) Execute(ctx context.Context, args []string) error {
	return a.ExecuteWithIO(ctx, args, a.config.Stdout, a.config.Stderr)
}

func (a *App) ExecuteWithIO(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	args = normalizeExecutableAliases(a.RootCommand(), args)
	if a.config.ContextDecorator != nil {
		if decorated := a.config.ContextDecorator(ctx); decorated != nil {
			ctx = decorated
		}
	}
	input := &frameworkadapter.RawInput{Args: append([]string(nil), args...), Context: ctx, IOMode: frameworkadapter.IOModeTTY, Stdout: stdout, Stderr: stderr, Source: a.sourceMeta("cli", args)}
	var runErr error
	if a.config.BeforeExecute != nil {
		runErr = a.config.BeforeExecute(ctx)
	}
	if runErr == nil {
		runErr = a.router.Execute(input, nil)
	}
	if a.config.AfterExecute != nil {
		if err := a.config.AfterExecute(ctx, runErr); err != nil && (runErr == nil || IsSuccessfulExit(runErr)) {
			runErr = err
		}
	}
	if IsSuccessfulExit(runErr) {
		return runErr
	}
	var rendered *AlreadyRenderedError
	if runErr != nil && !frameworkerrors.SafeAs(runErr, &rendered) {
		var payloadBuilder frameworkoutput.ErrorPayloadBuilder
		if format, ok := explicitErrorFormat(args); ok && frameworkerrors.SafeAs(runErr, &payloadBuilder) {
			state := &pipeline.PipelineContext{
				Input:  input,
				Parsed: &frameworkrouter.ParsedCommand{Flags: map[string]any{"format": format}},
			}
			if a.renderErrorEnvelope(state, runErr) {
				runErr = &AlreadyRenderedError{Err: runErr}
			}
		}
	}
	return runErr
}

// normalizeExecutableAliases keeps legacy top-level flags while routing them
// through their executable command. Cobra handles --version before RunE,
// which would otherwise bypass command observers, wrappers, and restrictions.
func normalizeExecutableAliases(root *cobra.Command, args []string) []string {
	command := root
	if root != nil {
		if found, _, err := root.Find(args); err == nil && found != nil {
			command = found
		}
	}
	versionFlags := make(map[int]struct{})
	afterTerminator := false
	expectsValue := false
	for index, arg := range args {
		if arg == "--" {
			afterTerminator = true
			expectsValue = false
			continue
		}
		if afterTerminator {
			continue
		}
		if expectsValue {
			expectsValue = false
			continue
		}
		if isEnabledVersionFlag(arg) {
			versionFlags[index] = struct{}{}
			continue
		}
		if flagRequiresSeparateValue(command, arg) {
			expectsValue = true
		}
	}
	if len(versionFlags) == 0 {
		return args
	}
	normalized := make([]string, 0, len(args))
	normalized = append(normalized, "version")
	for index, arg := range args {
		if _, isVersionFlag := versionFlags[index]; isVersionFlag {
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func flagRequiresSeparateValue(command *cobra.Command, arg string) bool {
	if command == nil || strings.Contains(arg, "=") {
		return false
	}
	if name, ok := strings.CutPrefix(arg, "--"); ok && name != "" {
		flag := command.Flag(name)
		return flag != nil && flag.NoOptDefVal == ""
	}
	if len(arg) != 2 || arg[0] != '-' || arg[1] == '-' {
		return false
	}
	shorthand := string(arg[1])
	for _, flags := range []*pflag.FlagSet{command.Flags(), command.PersistentFlags(), command.InheritedFlags()} {
		if flag := flags.ShorthandLookup(shorthand); flag != nil {
			return flag.NoOptDefVal == ""
		}
	}
	return false
}

func isEnabledVersionFlag(arg string) bool {
	if arg == "--version" {
		return true
	}
	value, ok := strings.CutPrefix(arg, "--version=")
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

func explicitErrorFormat(args []string) (string, bool) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		switch {
		case arg == "--format" || arg == "-o":
			if index+1 < len(args) {
				return strings.TrimSpace(args[index+1]), true
			}
		case strings.HasPrefix(arg, "--format="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "--format=")), true
		case strings.HasPrefix(arg, "-o="):
			return strings.TrimSpace(strings.TrimPrefix(arg, "-o=")), true
		}
	}
	return "", false
}

// RenderExplicitError writes a unified error envelope for failures that occur
// before an App is available, such as product startup or extension bootstrap
// failures. It returns false when no explicit output format was requested, the
// error has no stable public payload, or formatting/writing fails; callers can
// then preserve their existing plain-text fallback.
func RenderExplicitError(args []string, stderr io.Writer, processor *frameworkoutput.Processor, err error) bool {
	if err == nil || processor == nil {
		return false
	}
	var payloadBuilder frameworkoutput.ErrorPayloadBuilder
	if !frameworkerrors.SafeAs(err, &payloadBuilder) {
		return false
	}
	format, ok := explicitErrorFormat(args)
	if !ok {
		return false
	}
	payload, renderErr := processor.RenderError(&frameworkoutput.Context{
		Parsed: &frameworkrouter.ParsedCommand{Flags: map[string]any{"format": format}},
	}, err)
	if renderErr != nil || len(payload) == 0 || stderr == nil {
		return false
	}
	if _, writeErr := stderr.Write(payload); writeErr != nil {
		return false
	}
	if payload[len(payload)-1] != '\n' {
		_, _ = stderr.Write([]byte{'\n'})
	}
	return true
}

func (a *App) Invoke(ctx context.Context, args []string) (*executor.RawResult, error) {
	input := &frameworkadapter.RawInput{Args: append([]string(nil), args...), Context: ctx, IOMode: frameworkadapter.IOModeProgrammatic, Source: a.sourceMeta("cliapp", args)}
	return a.ExecuteRaw(ctx, input)
}

func (a *App) ParseArgsToRequest(ctx context.Context, args []string) (*executor.NormalizedRequest, error) {
	if len(args) == 0 {
		return nil, frameworkrouter.ErrInvalidArgs("args is required")
	}
	if isMetaCommand(args) {
		return nil, frameworkrouter.ErrInvalidArgs("help/version command does not produce a normalized request")
	}
	input := &frameworkadapter.RawInput{Args: append([]string(nil), args...), Context: ctx, IOMode: frameworkadapter.IOModeProgrammatic, Source: a.sourceMeta("cli", args)}
	parsed, err := a.router.Route(input)
	if err != nil {
		return nil, err
	}
	state := &pipeline.PipelineContext{Input: input, Parsed: parsed}
	for _, step := range []pipeline.PipelineStep{&pipeline.ParamMergeStep{}, &pipeline.ParamValidateStep{}} {
		if err := step.Execute(ctx, state); err != nil {
			return nil, err
		}
	}
	return pipeline.BuildNormalizedRequest(input, parsed, pipeline.BuildInputValues(parsed)), nil
}

func (a *App) ExecuteRaw(ctx context.Context, input *frameworkadapter.RawInput) (*executor.RawResult, error) {
	parsed, err := a.router.Route(input)
	if err != nil {
		return nil, err
	}
	state := &pipeline.PipelineContext{Input: input, Parsed: parsed}
	if err := a.pipe.Execute(ctx, state); err != nil {
		return nil, err
	}
	return state.Result, nil
}

func (a *App) Registry() *registry.Registry {
	if a == nil || a.manager == nil {
		return nil
	}
	return a.manager.Registry()
}

func (a *App) executeParsed(parsed *frameworkrouter.ParsedCommand) error {
	input := a.router.CurrentInput()
	state := &pipeline.PipelineContext{Input: input, Parsed: parsed}
	ctx := context.Background()
	if input != nil && input.Context != nil {
		ctx = input.Context
	}
	err := a.pipe.Execute(ctx, state)
	if err == nil {
		return nil
	}
	if IsSuccessfulExit(err) {
		return err
	}
	if a.renderErrorEnvelope(state, err) {
		return &AlreadyRenderedError{Err: err}
	}
	return err
}

// renderErrorEnvelope formats err via the pipeline's OutputStep processor and
// writes the resulting envelope to state.Input.Stderr. Returns true when the
// envelope was successfully written (the caller then wraps err with
// AlreadyRenderedError so main.fail doesn't re-render). Returns false in
// programmatic/SDK mode, when no OutputStep processor is reachable, or when
// formatting itself fails — in those cases the caller falls back to plain
// text error rendering.
func (a *App) renderErrorEnvelope(state *pipeline.PipelineContext, err error) bool {
	if state == nil || state.Input == nil {
		return false
	}
	if state.Input.IOMode == frameworkadapter.IOModeProgrammatic {
		return false
	}
	processor := a.pipelineOutputProcessor()
	if processor == nil {
		return false
	}
	fmtCtx := &frameworkoutput.Context{
		Input:  state.Input,
		Parsed: state.Parsed,
		Values: state.Values,
		Result: state.Result,
	}
	payload, renderErr := processor.RenderError(fmtCtx, err)
	if renderErr != nil || len(payload) == 0 {
		return false
	}
	stderr := state.Input.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if _, writeErr := stderr.Write(payload); writeErr != nil {
		return false
	}
	if payload[len(payload)-1] != '\n' {
		_, _ = stderr.Write([]byte{'\n'})
	}
	return true
}

// pipelineOutputProcessor walks the assembled pipeline for an OutputStep and
// returns its Processor, falling back to config.OutputProcessor so error
// envelope rendering is available even for pipelines that deliberately omit
// an OutputStep. Returns nil when neither is reachable — the caller then
// skips envelope rendering entirely.
func (a *App) pipelineOutputProcessor() *frameworkoutput.Processor {
	if a == nil {
		return nil
	}
	if a.pipe != nil {
		for _, step := range a.pipe.Steps {
			if out, ok := step.(*pipeline.OutputStep); ok && out != nil && out.Processor != nil {
				return out.Processor
			}
		}
	}
	return a.config.OutputProcessor
}

func (a *App) customizeRoot(root *cobra.Command) {
	meta := RootCommandMetadata{AppName: a.config.AppName, Version: a.config.Version}
	applyDefaultRootCommand(root, meta)
	if a.config.RootCustomizer != nil {
		a.config.RootCustomizer(root, meta)
	}
}

func (a *App) sourceMeta(channel string, args []string) frameworkadapter.InputSourceMeta {
	return frameworkadapter.InputSourceMeta{
		Channel: channel,
		Command: strings.Join(args, " "),
		Product: a.config.AppName,
		Version: a.config.Version,
	}
}

func isMetaCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "--version" {
			return true
		}
	}
	switch args[0] {
	case "help", "version":
		return true
	default:
		return false
	}
}
