// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

func TestAppExecuteWithIORunsTerminalChain(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--priority", "3", "--format", "json"}, stdout, stdout); err != nil {
		t.Fatalf("execute with io: %v", err)
	}
	output := stdout.String()
	for _, fragment := range []string{"\"key\": \"PROJ\"", "\"priority\": 3"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got %s", fragment, output)
		}
	}
}

func TestAppExecuteWithIOSelectProjection(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--priority", "3", "--select", "key", "--format", "json"}, stdout, stdout); err != nil {
		t.Fatalf("execute with io: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "\"key\": \"PROJ\"") {
		t.Fatalf("expected projected key in %s", output)
	}
	if strings.Contains(output, "priority") {
		t.Fatalf("expected priority to be projected out, got %s", output)
	}
}

func TestAppExecuteWithIOJSONOutput(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--priority", "3", "--format", "json"}, stdout, stdout); err != nil {
		t.Fatalf("execute with io: %v", err)
	}
	output := strings.TrimSpace(stdout.String())
	if !json.Valid([]byte(output)) {
		t.Fatalf("expected json output to be valid json, got %q", output)
	}
}

// Required flags may be supplied through --params. Router-level Cobra required
// validation runs before the pipeline and would otherwise reject this form
// before ParamMergeStep can materialize the JSON keys as flags.
func TestAppExecuteWithIOParamsSatisfyRequiredFlags(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{
		"workitem", "create",
		"--params", `{"key":"PROJ","priority":3}`,
		"--format", "json",
	}, stdout, stdout); err != nil {
		t.Fatalf("execute with params: %v", err)
	}
	output := stdout.String()
	for _, fragment := range []string{`"key": "PROJ"`, `"priority": 3`} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got %s", fragment, output)
		}
	}
}

func TestAppInvokeStillRejectsMissingRequiredFlag(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	_, err = app.Invoke(context.Background(), []string{"workitem", "create"})
	if err == nil {
		t.Fatal("expected missing required flag error")
	}
	cliErr := frameworkerrors.As(err)
	if cliErr == nil || cliErr.Code != frameworkerrors.CodeParamRequired {
		t.Fatalf("error = %v, want %s", err, frameworkerrors.CodeParamRequired)
	}
}

func TestAppInvokeAggregatesMissingRequiredFlags(t *testing.T) {
	called := false
	app, err := newRequiredValidationTestApp(&called)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	_, err = app.Invoke(context.Background(), []string{
		"workflow", "list-state-transitions",
		"--project-key", "demo",
		"--work-item-id", "1",
		"--dry-run",
	})
	if err == nil {
		t.Fatal("expected missing required flags error")
	}
	if got, want := err.Error(), "missing required parameters: --user-key, --work-item-type"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	cliErr := frameworkerrors.As(err)
	if cliErr == nil || cliErr.Code != frameworkerrors.CodeParamRequired {
		t.Fatalf("error = %v, want %s", err, frameworkerrors.CodeParamRequired)
	}
	if called {
		t.Fatal("executor must not run when required flags are missing")
	}
}

func TestAppParseArgsToRequestAggregatesMissingRequiredFlags(t *testing.T) {
	app, err := newRequiredValidationTestApp(nil)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	_, err = app.ParseArgsToRequest(context.Background(), []string{
		"workflow", "list-state-transitions",
		"--project-key", "demo",
		"--work-item-id", "1",
	})
	if err == nil {
		t.Fatal("expected missing required flags error")
	}
	if got, want := err.Error(), "missing required parameters: --user-key, --work-item-type"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestAppParseArgsToRequestAcceptsRequiredFlagsFromDirectParamsAndSet(t *testing.T) {
	app, err := newRequiredValidationTestApp(nil)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	req, err := app.ParseArgsToRequest(context.Background(), []string{
		"workflow", "list-state-transitions",
		"--project-key", "demo",
		"--work-item-id", "1",
		"--params", `{"work-item-type":"story"}`,
		"--set", "user-key=user-1",
	})
	if err != nil {
		t.Fatalf("parse args to request: %v", err)
	}
	for key, want := range map[string]any{
		"project-key":    "demo",
		"work-item-id":   "1",
		"work-item-type": "story",
		"user-key":       "user-1",
	} {
		if got := req.Input[key]; got != want {
			t.Fatalf("input[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestAppExecuteWithIOVersion(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"version"}, stdout, stdout); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "test-version" {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestAppParseArgsToRequestRejectsVersionFlag(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	_, err = app.ParseArgsToRequest(context.Background(), []string{"--version"})
	if err == nil {
		t.Fatal("expected --version to be rejected as a meta command")
	}
	if !strings.Contains(err.Error(), "help/version command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppInvokeRunsProgrammaticChain(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	result, err := app.Invoke(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--priority", "3"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", result.Data)
	}
	if data["key"] != "PROJ" {
		t.Fatalf("key = %#v", data["key"])
	}
	if data["priority"] != 3 {
		t.Fatalf("priority = %#v", data["priority"])
	}
}

func TestAppExecuteWithIODryRunSkipsBackend(t *testing.T) {
	called := false
	app, err := newTestAppWithHandler(func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		called = true
		return &executor.RawResult{Data: req.Values}, nil
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--dry-run"}, stdout, stdout); err != nil {
		t.Fatalf("execute dry-run: %v", err)
	}
	if called {
		t.Fatal("expected executor not to be called during dry-run")
	}
	output := stdout.String()
	for _, fragment := range []string{"\"tool\": \"test.workitem.create\"", "\"dry_run\": true", "\"key\": \"PROJ\""} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got %s", fragment, output)
		}
	}
}

func TestAppParseArgsToRequestBuildsNormalizedRequest(t *testing.T) {
	app, err := newTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	req, err := app.ParseArgsToRequest(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--dry-run"})
	if err != nil {
		t.Fatalf("parse args to request: %v", err)
	}
	if req.Tool != "test.workitem.create" {
		t.Fatalf("tool = %q", req.Tool)
	}
	if !req.Meta.DryRun {
		t.Fatal("expected dry-run meta to be true")
	}
	if req.Input["key"] != "PROJ" {
		t.Fatalf("key = %#v", req.Input["key"])
	}
}

// When a pipeline step fails, App.Execute must render the error envelope to
// stderr via the OutputStep processor and wrap the returned err in
// AlreadyRenderedError so CLI entry points know not to re-render.
func TestAppExecuteWithIORendersErrorEnvelope(t *testing.T) {
	backend := frameworkerrors.New(frameworkerrors.CategoryServer, "BACKEND_5XX", "upstream 503")
	app, err := newTestAppWithHandler(func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		_, _ = ctx, req
		return nil, backend
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runErr := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--format", "json"}, stdout, stderr)
	if runErr == nil {
		t.Fatalf("expected error, got nil")
	}
	var rendered *AlreadyRenderedError
	if !stderrors.As(runErr, &rendered) {
		t.Fatalf("error must be wrapped as AlreadyRenderedError, got %T: %v", runErr, runErr)
	}
	// Exit code must still come through from the inner CLIError.
	if code := frameworkerrors.ExitCode(runErr); code != 2 {
		t.Fatalf("exit code = %d, want 2 (CategoryServer)", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty on error, got %q", stdout.String())
	}
	// Envelope must be valid JSON and carry the documented fields.
	trimmed := strings.TrimSpace(stderr.String())
	if !json.Valid([]byte(trimmed)) {
		t.Fatalf("stderr is not valid JSON:\n%s", trimmed)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["data"] != nil {
		t.Fatalf("data must be null, got %v", env["data"])
	}
	record, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field missing: %v", env["error"])
	}
	if record["code"] != "BACKEND_5XX" || record["retryable"] != true {
		t.Fatalf("error record: %v", record)
	}
}

// NDJSON flag must flow through the RenderError path so a one-line envelope
// is emitted on stderr — proving the executeParsed plumbing honors --format.
func TestAppExecuteWithIORendersErrorEnvelope_NDJSON(t *testing.T) {
	app, err := newTestAppWithHandler(func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		_, _ = ctx, req
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, "BAD", "nope")
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	_ = app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "PROJ", "--format", "ndjson"}, stdout, stderr)
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want single ndjson envelope line, got %d:\n%s", len(lines), stderr.String())
	}
	if !strings.Contains(lines[0], `"code":"BAD"`) {
		t.Fatalf("line missing code: %s", lines[0])
	}
}

// Programmatic/SDK callers must not see stderr pollution — error rendering is
// a TTY concern. Invoke() uses IOModeProgrammatic so RenderError skips.
func TestAppInvokeDoesNotRenderEnvelope(t *testing.T) {
	app, err := newTestAppWithHandler(func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		_, _ = ctx, req
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, "BAD", "nope")
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	_, runErr := app.Invoke(context.Background(), []string{"workitem", "create", "--key", "PROJ"})
	if runErr == nil {
		t.Fatalf("expected error")
	}
	// Must NOT be wrapped — programmatic callers consume err directly.
	var rendered *AlreadyRenderedError
	if stderrors.As(runErr, &rendered) {
		t.Fatalf("programmatic error must not be wrapped as AlreadyRenderedError")
	}
}

func TestAppRootCustomizerOverridesDefaultShellText(t *testing.T) {
	app, err := New(
		WithAppName("custom-cli"),
		WithVersion("test-version"),
		WithSetup(registry.NewStaticSetup(&registry.CommandTree{})),
		WithRootCommandCustomizer(func(root *cobra.Command, meta RootCommandMetadata) {
			root.Short = "Custom runtime shell"
		}),
	)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if app.RootCommand().Short != "Custom runtime shell" {
		t.Fatalf("root short = %q", app.RootCommand().Short)
	}
	stdout := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"version"}, stdout, stdout); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "test-version" {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func newTestApp() (*App, error) {
	return newTestAppWithHandler(func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		_ = ctx
		return &executor.RawResult{Data: req.Values, Metadata: map[string]any{"tool": req.Command.Node.HandlerRef}}, nil
	})
}

func newTestAppWithHandler(handler executor.Handler) (*App, error) {
	tree := &registry.CommandTree{Nodes: []*registry.CommandNode{{
		Name: "workitem",
		Help: registry.HelpText{Brief: "Manage work items"},
		Children: []*registry.CommandNode{{
			Name:       "create",
			Help:       registry.HelpText{Brief: "Create a work item"},
			HandlerRef: "test.workitem.create",
			Flags:      []registry.FlagDef{{Name: "key", Type: registry.FlagTypeString, Required: true}, {Name: "priority", Type: registry.FlagTypeInt}},
		}},
	}}}
	return New(
		WithAppName("meego-cli"),
		WithVersion("test-version"),
		WithSetup(registry.NewStaticSetup(tree)),
		WithExecutor(executor.NewDirectExecutor(map[string]executor.Handler{
			"test.workitem.create": handler,
		})),
	)
}

func newRequiredValidationTestApp(called *bool) (*App, error) {
	tree := &registry.CommandTree{Nodes: []*registry.CommandNode{{
		Name: "workflow",
		Help: registry.HelpText{Brief: "Manage workflows"},
		Children: []*registry.CommandNode{{
			Name:       "list-state-transitions",
			Help:       registry.HelpText{Brief: "List state transitions"},
			HandlerRef: "test.workflow.list-state-transitions",
			Flags: []registry.FlagDef{
				{Name: "user-key", Type: registry.FlagTypeString, Required: true},
				{Name: "project-key", Type: registry.FlagTypeString, Required: true},
				{Name: "work-item-id", Type: registry.FlagTypeString, Required: true},
				{Name: "work-item-type", Type: registry.FlagTypeString, Required: true},
			},
		}},
	}}}
	return New(
		WithAppName("meegle"),
		WithVersion("test-version"),
		WithSetup(registry.NewStaticSetup(tree)),
		WithExecutor(executor.NewDirectExecutor(map[string]executor.Handler{
			"test.workflow.list-state-transitions": func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
				_, _ = ctx, req
				if called != nil {
					*called = true
				}
				return &executor.RawResult{Data: req.Values}, nil
			},
		})),
	)
}
