// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type rebuildRegistrySetup struct {
	tree *registry.CommandTree
}

func requireSafeRuntimeFailure(t *testing.T, err error, hook, stage, secret string) {
	t.Helper()
	var failure *RuntimeFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T %v, want *RuntimeFailure", err, err)
	}
	if failure.HookName != hook || failure.Stage != stage {
		t.Fatalf("runtime failure = %+v, want hook=%q stage=%q", failure, hook, stage)
	}
	if secret != "" && strings.Contains(failure.Error(), secret) {
		t.Fatalf("public runtime error leaked callback detail: %q", failure.Error())
	}
	if failure.Cause == nil || (secret != "" && !strings.Contains(failure.Cause.Error(), secret)) {
		t.Fatalf("runtime failure did not preserve internal cause: %+v", failure)
	}
	if code := failure.ErrorPayload()["code"]; code != "CLIENT_EXTENSION_RUNTIME_FAILED" {
		t.Fatalf("runtime failure code = %#v", code)
	}
}

func (s *rebuildRegistrySetup) Setup(context.Context) (*registry.CommandTree, error) {
	return s.tree, nil
}

type countPipelineStep struct {
	calls *int
}

type stealingPayloadError struct {
	secret string
	cause  error
}

func (e *stealingPayloadError) Error() string { return e.secret }
func (e *stealingPayloadError) Unwrap() error { return e.cause }
func (e *stealingPayloadError) ErrorPayload() map[string]any {
	return map[string]any{"code": "STOLEN", "message": e.secret, "retryable": false}
}

type panickingPayloadError struct{ cause error }

func (e *panickingPayloadError) Error() string { return "secret-panicking-payload" }
func (e *panickingPayloadError) Unwrap() error { return e.cause }
func (*panickingPayloadError) ErrorPayload() map[string]any {
	panic("secret-payload-builder-panic")
}

type panickingTraversalError struct{}

func (*panickingTraversalError) Error() string { return "secret-error-traversal" }
func (*panickingTraversalError) Unwrap() error { panic("secret-unwrap-panic") }
func (*panickingTraversalError) Is(error) bool { panic("secret-is-panic") }
func (*panickingTraversalError) As(any) bool   { panic("secret-as-panic") }

func (countPipelineStep) Name() string { return "count" }
func (s countPipelineStep) Execute(context.Context, *pipeline.PipelineContext) error {
	*s.calls++
	return nil
}

type testPlugin struct {
	name     string
	restrict bool
}

func (p testPlugin) Name() string  { return p.name }
func (testPlugin) Version() string { return "1.0.0" }
func (p testPlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{Restricts: p.restrict, FailurePolicy: platformapi.FailClosed}
}

type metadataPanicPlugin struct {
	panicAt string
	policy  platformapi.FailurePolicy
}

func (p metadataPanicPlugin) Name() string {
	if p.panicAt == "name" {
		panic("name unavailable")
	}
	return "metadata-panic"
}
func (p metadataPanicPlugin) Version() string {
	if p.panicAt == "version" {
		panic("version unavailable")
	}
	return "1.0.0"
}
func (p metadataPanicPlugin) Capabilities() platformapi.Capabilities {
	if p.panicAt == "capabilities" {
		panic("capabilities unavailable")
	}
	return platformapi.Capabilities{FailurePolicy: p.policy}
}
func (metadataPanicPlugin) Install(platformapi.Registrar) error { return nil }

type optionalInstallFailurePlugin struct{}

func (optionalInstallFailurePlugin) Name() string    { return "optional-audit" }
func (optionalInstallFailurePlugin) Version() string { return "2.0.0" }
func (optionalInstallFailurePlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{FailurePolicy: platformapi.FailOpen}
}
func (optionalInstallFailurePlugin) Install(platformapi.Registrar) error {
	return errors.New("audit sink unavailable")
}

type partialInstallFailurePlugin struct{ policy platformapi.FailurePolicy }

func (partialInstallFailurePlugin) Name() string    { return "partial-install" }
func (partialInstallFailurePlugin) Version() string { return "1.0.0" }
func (p partialInstallFailurePlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{FailurePolicy: p.policy}
}
func (partialInstallFailurePlugin) Install(registrar platformapi.Registrar) error {
	registrar.Observe(platformapi.Before, "must-rollback", platformapi.All(), func(context.Context, platformapi.Invocation) {})
	return errors.New("install failed after registration")
}

type panickingTraversalInstallPlugin struct{}

func (panickingTraversalInstallPlugin) Name() string    { return "install-error-chain" }
func (panickingTraversalInstallPlugin) Version() string { return "1.0.0" }
func (panickingTraversalInstallPlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{FailurePolicy: platformapi.FailClosed}
}
func (panickingTraversalInstallPlugin) Install(platformapi.Registrar) error {
	return &panickingTraversalError{}
}

type failOpenRestrictPlugin struct{ installCalls *atomic.Int32 }

func (failOpenRestrictPlugin) Name() string    { return "optional-restrict" }
func (failOpenRestrictPlugin) Version() string { return "1.0.0" }
func (failOpenRestrictPlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{Restricts: true, FailurePolicy: platformapi.FailOpen}
}
func (p failOpenRestrictPlugin) Install(platformapi.Registrar) error {
	p.installCalls.Add(1)
	return nil
}

type failOpenRestrictNamePanicPlugin struct{}

func (failOpenRestrictNamePanicPlugin) Name() string { panic("name unavailable") }
func (failOpenRestrictNamePanicPlugin) Version() string {
	return "1.0.0"
}
func (failOpenRestrictNamePanicPlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{Restricts: true, FailurePolicy: platformapi.FailOpen}
}
func (failOpenRestrictNamePanicPlugin) Install(platformapi.Registrar) error { return nil }

type blockingInstallPlugin struct {
	policy     platformapi.FailurePolicy
	started    chan<- struct{}
	release    <-chan struct{}
	registered chan<- struct{}
}

type blockingMetadataPlugin struct{}

func (blockingMetadataPlugin) Name() string    { return "blocking-metadata" }
func (blockingMetadataPlugin) Version() string { return "1.0.0" }
func (blockingMetadataPlugin) Capabilities() platformapi.Capabilities {
	select {}
}
func (blockingMetadataPlugin) Install(platformapi.Registrar) error { return nil }

func (blockingInstallPlugin) Name() string    { return "blocking-install" }
func (blockingInstallPlugin) Version() string { return "1.0.0" }
func (p blockingInstallPlugin) Capabilities() platformapi.Capabilities {
	return platformapi.Capabilities{FailurePolicy: p.policy}
}
func (p blockingInstallPlugin) Install(registrar platformapi.Registrar) error {
	close(p.started)
	<-p.release
	registrar.Observe(platformapi.Before, "late", platformapi.All(), func(context.Context, platformapi.Invocation) {})
	close(p.registered)
	return nil
}

func TestInstallPlugins_DiagnosticsDistinguishSkippedPlugin(t *testing.T) {
	runtime, err := installPlugins("1.0.0", []platformapi.Plugin{optionalInstallFailurePlugin{}})
	if err != nil {
		t.Fatalf("installPlugins: %v", err)
	}
	diagnostics := runtime.Diagnostics()
	if len(diagnostics.Plugins) != 1 {
		t.Fatalf("diagnostic plugins = %+v", diagnostics.Plugins)
	}
	got := diagnostics.Plugins[0]
	if got.Name != "optional-audit" || got.Status != "skipped" || got.FailurePolicy != "fail-open" || got.FailureStage != "install" {
		t.Fatalf("plugin diagnostic = %+v", got)
	}
}

func TestInstallPlugins_TimeoutFreezesStagingRegistrar(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		policy  platformapi.FailurePolicy
		wantErr bool
	}{
		{name: "fail-open", policy: platformapi.FailOpen},
		{name: "fail-closed", policy: platformapi.FailClosed, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			registered := make(chan struct{})
			plugin := blockingInstallPlugin{
				policy: testCase.policy, started: started, release: release, registered: registered,
			}
			plugins := []platformapi.Plugin{plugin}
			if testCase.policy == platformapi.FailOpen {
				plugins = append(plugins, testPlugin{name: "healthy-after-timeout"})
			}
			begin := time.Now()
			runtime, err := installPluginsWithTimeout("1.0.0", plugins, 20*time.Millisecond)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("install error = %v, wantErr=%t", err, testCase.wantErr)
			}
			if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
				t.Fatalf("install timeout returned after %s", elapsed)
			}
			select {
			case <-started:
			default:
				t.Fatal("Install callback did not start")
			}
			close(release)
			select {
			case <-registered:
			case <-time.After(time.Second):
				t.Fatal("late Install callback did not finish")
			}
			if runtime != nil {
				diagnostics := runtime.Diagnostics()
				wantPlugins := 1
				if testCase.policy == platformapi.FailOpen {
					wantPlugins = 2
				}
				if len(diagnostics.Plugins) != wantPlugins || diagnostics.Plugins[0].Status != "skipped" || len(diagnostics.Plugins[0].Hooks) != 0 {
					t.Fatalf("runtime accepted late registration: %+v", diagnostics.Plugins)
				}
				if wantPlugins == 2 && diagnostics.Plugins[1].Status != "active" {
					t.Fatalf("healthy plugin after fail-open timeout was not active: %+v", diagnostics.Plugins)
				}
			}
		})
	}
}

func TestInstallPlugins_MetadataTimeoutFailsClosed(t *testing.T) {
	begin := time.Now()
	runtime, err := installPluginsWithTimeout("1.0.0", []platformapi.Plugin{blockingMetadataPlugin{}}, 20*time.Millisecond)
	if runtime != nil || err == nil {
		t.Fatalf("metadata timeout runtime=%v error=%v, want fail-closed error", runtime, err)
	}
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("metadata timeout returned after %s", elapsed)
	}
	requireSafeRuntimeFailure(t, err, "plugin-metadata", "metadata-capabilities", "deadline")
}

func TestInstallPlugins_PartialRegistrationIsRolledBackAtomically(t *testing.T) {
	runtime, err := installPlugins("1.0.0", []platformapi.Plugin{partialInstallFailurePlugin{policy: platformapi.FailOpen}})
	if err != nil {
		t.Fatalf("fail-open install: %v", err)
	}
	if len(runtime.observers) != 0 || len(runtime.wrappers) != 0 || len(runtime.rules) != 0 {
		t.Fatalf("failed plugin leaked staged registrations: observers=%d wrappers=%d rules=%d", len(runtime.observers), len(runtime.wrappers), len(runtime.rules))
	}
	diagnostics := runtime.Diagnostics()
	if len(diagnostics.Plugins) != 1 || diagnostics.Plugins[0].Status != "skipped" || diagnostics.Plugins[0].FailureStage != "install" {
		t.Fatalf("failed plugin diagnostics = %+v", diagnostics.Plugins)
	}

	if closedRuntime, closedErr := installPlugins("1.0.0", []platformapi.Plugin{partialInstallFailurePlugin{policy: platformapi.FailClosed}}); closedErr == nil || closedRuntime != nil {
		t.Fatalf("fail-closed partial install runtime=%v error=%v, want no committed runtime", closedRuntime, closedErr)
	}
}

func TestInstallPlugins_ContainsPanickingErrorTraversal(t *testing.T) {
	_, err := installPlugins("1.0.0", []platformapi.Plugin{panickingTraversalInstallPlugin{}})
	if err == nil || strings.Contains(err.Error(), "secret-error-traversal") {
		t.Fatalf("install error = %v, want controlled non-secret failure", err)
	}
	var unrelated interface{ Marker() }
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("plugin install error traversal escaped boundary: %v", recovered)
		}
	}()
	if errors.Is(err, errors.New("unrelated")) || errors.As(err, &unrelated) {
		t.Fatal("malicious install error unexpectedly matched unrelated target")
	}
}

func TestInstallPlugins_FailOpenRestrictFailsBeforeInstall(t *testing.T) {
	var installCalls atomic.Int32
	runtime, err := installPlugins("1.0.0", []platformapi.Plugin{failOpenRestrictPlugin{installCalls: &installCalls}})
	if runtime != nil {
		t.Fatalf("installPlugins() runtime = %v, want nil", runtime)
	}
	if err == nil || !strings.Contains(err.Error(), "fail-closed") {
		t.Fatalf("installPlugins() error = %v, want fail-closed declaration error", err)
	}
	if installCalls.Load() != 0 {
		t.Fatalf("Install calls = %d, want 0 for unsafe fail-open Restrict", installCalls.Load())
	}
}

func TestInstallPlugins_FailOpenRestrictCannotHideBehindMetadataFailure(t *testing.T) {
	runtime, err := installPlugins("1.0.0", []platformapi.Plugin{failOpenRestrictNamePanicPlugin{}})
	if runtime != nil {
		t.Fatalf("installPlugins() runtime = %v, want nil", runtime)
	}
	if err == nil || !strings.Contains(err.Error(), "fail-closed") {
		t.Fatalf("installPlugins() error = %v, want fail-closed declaration error", err)
	}
}

func TestActivePluginDiagnostic_DescribesBuiltInSelectorsAndPolicyRules(t *testing.T) {
	stage := &stagingRegistrar{
		observers: []observerEntry{{name: "audit.all", selector: platformapi.All()}},
		wrappers:  []wrapperEntry{{name: "guard.write", selector: platformapi.ByWrite()}},
		rules: []ruleEntry{{rule: &platformapi.Rule{
			Name: "read-only", Allow: []string{"workitem/**"}, Deny: []string{"workitem/delete"},
			MaxRisk: platformapi.RiskRead, Identities: []platformapi.Identity{platformapi.IdentityUser},
		}}},
	}
	diagnostic := activePluginDiagnostic("governance", "1.0.0", platformapi.Capabilities{Restricts: true}, stage)
	if diagnostic.Hooks[0].Selector != "all" || diagnostic.Hooks[1].Selector != "write" {
		t.Fatalf("hook selectors = %+v, want all and write", diagnostic.Hooks)
	}
	if len(diagnostic.RuleDetails) != 1 {
		t.Fatalf("rule details = %+v", diagnostic.RuleDetails)
	}
	rule := diagnostic.RuleDetails[0]
	if rule.Name != "read-only" || strings.Join(rule.Allow, ",") != "workitem/**" ||
		strings.Join(rule.Deny, ",") != "workitem/delete" || rule.MaxRisk != "read" || strings.Join(rule.Identities, ",") != "user" {
		t.Fatalf("rule diagnostic = %+v", rule)
	}
}

func TestInstallPlugins_MetadataPanicBecomesErrorOrFailOpenSkip(t *testing.T) {
	tests := []struct {
		name    string
		plugin  platformapi.Plugin
		wantErr bool
	}{
		{name: "capabilities cannot determine policy", plugin: metadataPanicPlugin{panicAt: "capabilities", policy: platformapi.FailOpen}, wantErr: true},
		{name: "fail closed name", plugin: metadataPanicPlugin{panicAt: "name", policy: platformapi.FailClosed}, wantErr: true},
		{name: "fail open name", plugin: metadataPanicPlugin{panicAt: "name", policy: platformapi.FailOpen}, wantErr: false},
		{name: "fail closed version", plugin: metadataPanicPlugin{panicAt: "version", policy: platformapi.FailClosed}, wantErr: true},
		{name: "fail open version", plugin: metadataPanicPlugin{panicAt: "version", policy: platformapi.FailOpen}, wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := installPlugins("1.0.0", []platformapi.Plugin{tc.plugin})
			if (err != nil) != tc.wantErr {
				t.Fatalf("installPlugins() error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestInstallPlugins_RejectsInvalidFailurePolicy(t *testing.T) {
	_, err := installPlugins("1.0.0", []platformapi.Plugin{
		metadataPanicPlugin{policy: platformapi.FailurePolicy(99)},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid failure policy") {
		t.Fatalf("installPlugins() error = %v, want invalid failure policy", err)
	}
}

func TestCallPluginMetadata_PanicIsSecretFreeAndPreservesCause(t *testing.T) {
	sentinel := errors.New("secret-plugin-metadata")
	_, err := callPluginMetadata("name", func() string { panic(sentinel) })
	requireSafeRuntimeFailure(t, err, "plugin-metadata", "metadata-name", "")
	if strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("metadata panic leaked sentinel: %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("metadata panic error chain lost sentinel: %v", err)
	}
}
func (p testPlugin) Install(registrar platformapi.Registrar) error {
	if p.restrict {
		registrar.Restrict(&platformapi.Rule{Name: "guard", Allow: []string{"**"}})
	}
	return nil
}

func TestInstallPlugins_RejectsDuplicatePluginNames(t *testing.T) {
	_, err := installPlugins("1.0.0", []platformapi.Plugin{
		testPlugin{name: "duplicate"},
		testPlugin{name: "duplicate"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("installPlugins() error = %v, want duplicate-name error", err)
	}
}

func TestRuntime_FailOpenObserverPanicDoesNotBreakCommand(t *testing.T) {
	called := false
	runtime := &Runtime{observers: []observerEntry{{
		name: "audit.after", when: platformapi.After, selector: platformapi.All(),
		observer: func(context.Context, platformapi.Invocation) { panic("audit unavailable") },
		policy:   platformapi.FailOpen,
	}}}
	command := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		called = true
		return nil
	}}
	runtime.Apply(command)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatal("business command was not executed")
	}
}

func TestRuntime_SelectorPanicHonorsFailurePolicy(t *testing.T) {
	panicSelector := func(platformapi.CommandView) bool { panic("selector unavailable") }
	tests := []struct {
		name       string
		policy     platformapi.FailurePolicy
		wantCalled bool
		wantErr    bool
	}{
		{name: "fail open", policy: platformapi.FailOpen, wantCalled: true},
		{name: "fail closed", policy: platformapi.FailClosed, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			runtime := &Runtime{observers: []observerEntry{{
				name: "audit.selector", when: platformapi.Before, selector: panicSelector,
				observer: func(context.Context, platformapi.Invocation) {}, policy: tc.policy,
			}}}
			command := &cobra.Command{
				Use: "probe", SilenceErrors: true, SilenceUsage: true,
				RunE: func(*cobra.Command, []string) error { called = true; return nil },
			}
			runtime.Apply(command)
			err := command.Execute()
			if (err != nil) != tc.wantErr {
				t.Fatalf("execute error = %v, wantErr=%v", err, tc.wantErr)
			}
			if called != tc.wantCalled {
				t.Fatalf("command called = %v, want %v", called, tc.wantCalled)
			}
			if tc.policy == platformapi.FailClosed {
				requireSafeRuntimeFailure(t, err, "audit.selector", "selector", "selector unavailable")
			}
		})
	}
}

func TestRuntime_BeforeObserverFailureIsVisibleToAfterObserver(t *testing.T) {
	var observedErr error
	commandCalled := false
	runtime := &Runtime{observers: []observerEntry{
		{
			name: "guard.before", when: platformapi.Before, selector: platformapi.All(), policy: platformapi.FailClosed,
			observer: func(context.Context, platformapi.Invocation) { panic("guard unavailable") },
		},
		{
			name: "audit.after", when: platformapi.After, selector: platformapi.All(), policy: platformapi.FailOpen,
			observer: func(_ context.Context, invocation platformapi.Invocation) { observedErr = invocation.Err() },
		},
	}}
	command := &cobra.Command{
		Use: "probe", SilenceErrors: true, SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error { commandCalled = true; return nil },
	}
	runtime.Apply(command)
	err := command.Execute()
	if err == nil {
		t.Fatal("execute error = nil, want before observer failure")
	}
	if commandCalled {
		t.Fatal("business command ran after fail-closed before observer failure")
	}
	if observedErr == nil || observedErr.Error() != err.Error() {
		t.Fatalf("after observer error = %v, want %v", observedErr, err)
	}
}

func TestRuntime_WrapperPanicBecomesCommandError(t *testing.T) {
	runtime := &Runtime{wrappers: []wrapperEntry{{
		name: "guard.panic", selector: platformapi.All(), policy: platformapi.FailClosed,
		wrapper: func(platformapi.Handler) platformapi.Handler {
			return func(context.Context, platformapi.Invocation) error { panic("broken guard") }
		},
	}}}
	command := &cobra.Command{Use: "probe", SilenceErrors: true, SilenceUsage: true, RunE: func(*cobra.Command, []string) error { return nil }}
	runtime.Apply(command)
	err := command.Execute()
	requireSafeRuntimeFailure(t, err, "guard.panic", "wrapper", "broken guard")
}

func TestRuntime_WrapperReturnedErrorIsSafeRuntimeFailure(t *testing.T) {
	runtime := &Runtime{wrappers: []wrapperEntry{{
		name: "guard.error", selector: platformapi.All(), policy: platformapi.FailClosed,
		wrapper: func(platformapi.Handler) platformapi.Handler {
			return func(context.Context, platformapi.Invocation) error {
				return errors.New("wrapper rejected secret-wrapper-token")
			}
		},
	}}}
	command := &cobra.Command{Use: "probe", SilenceErrors: true, SilenceUsage: true, RunE: func(*cobra.Command, []string) error { return nil }}
	runtime.Apply(command)
	requireSafeRuntimeFailure(t, command.Execute(), "guard.error", "wrapper", "secret-wrapper-token")
}

func TestBuildWrapper_DoesNotReuseDownstreamEvidenceAcrossInvocations(t *testing.T) {
	sentinel := errors.New("shared secret-wrapper-sentinel")
	calls := 0
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.reentry",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				calls++
				if calls == 1 {
					return next(ctx, invocation)
				}
				return sentinel
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return sentinel })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	if first := handler(context.Background(), nil); !errors.Is(first, sentinel) {
		t.Fatalf("first downstream error = %v", first)
	}
	requireSafeRuntimeFailure(t, handler(context.Background(), nil), "guard.reentry", "wrapper", "secret-wrapper-sentinel")
}

func TestBuildWrapper_AsyncNextDoesNotRaceInvocationTracking(t *testing.T) {
	finished := make(chan struct{})
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.async",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				go func() {
					_ = next(ctx, invocation)
					close(finished)
				}()
				return errors.New("async secret-wrapper-error")
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return nil })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	requireSafeRuntimeFailure(t, handler(context.Background(), nil), "guard.async", "wrapper", "secret-wrapper-error")
	<-finished
}

func TestBuildWrapper_LateAsyncNextCannotReportSuccessWithoutRunningDownstream(t *testing.T) {
	release := make(chan struct{})
	lateResult := make(chan error, 1)
	downstreamCalls := atomic.Int32{}
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.late-async",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				go func() {
					<-release
					lateResult <- next(ctx, invocation)
				}()
				return nil
			}
		},
	}, func(context.Context, platformapi.Invocation) error {
		downstreamCalls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}

	result := handler(context.Background(), nil)
	requireSafeRuntimeFailure(t, result, "guard.late-async", "wrapper", "synchronously")
	close(release)
	if lateErr := <-lateResult; lateErr == nil {
		t.Fatal("late asynchronous next call returned nil after wrapper completion")
	}
	if downstreamCalls.Load() != 0 {
		t.Fatalf("downstream calls = %d, want 0 for a late next call", downstreamCalls.Load())
	}
}

func TestBuildWrapper_IgnoredDuplicateNextStillFails(t *testing.T) {
	downstreamCalls := atomic.Int32{}
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.duplicate-next",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				if err := next(ctx, invocation); err != nil {
					return err
				}
				_ = next(ctx, invocation)
				return nil
			}
		},
	}, func(context.Context, platformapi.Invocation) error {
		downstreamCalls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}

	requireSafeRuntimeFailure(t, handler(context.Background(), nil), "guard.duplicate-next", "wrapper", "at most once")
	if got := downstreamCalls.Load(); got != 1 {
		t.Fatalf("downstream calls = %d, want exactly 1", got)
	}
}

func TestBuildWrapper_IgnoredDownstreamErrorStillFails(t *testing.T) {
	downstream := &platformapi.CommandDeniedError{Path: "workitem/create", Reason: "denied"}
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.ignore-downstream-error",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				_ = next(ctx, invocation)
				return nil
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return downstream })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	if result := handler(context.Background(), nil); !errors.Is(result, downstream) {
		t.Fatalf("ignored downstream result = %v, want original command failure", result)
	}
}

func TestBuildWrapper_ExecutesDownstreamAtMostOnce(t *testing.T) {
	calls := 0
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.double-next",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				if err := next(ctx, invocation); err != nil {
					return err
				}
				return next(ctx, invocation)
			}
		},
	}, func(context.Context, platformapi.Invocation) error { calls++; return nil })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	requireSafeRuntimeFailure(t, handler(context.Background(), nil), "guard.double-next", "wrapper", "at most once")
	if calls != 1 {
		t.Fatalf("downstream calls = %d, want exactly one", calls)
	}
}

func TestBuildWrapper_ContextReplacementPreservesDownstreamPayload(t *testing.T) {
	downstream := &platformapi.CommandDeniedError{Path: "workitem/create", Reason: "denied"}
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.context",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(context.Context, platformapi.Invocation) error {
				return next(context.Background(), nil)
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return downstream })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	result := handler(context.Background(), nil)
	if !errors.Is(result, downstream) {
		t.Fatalf("downstream error chain lost: %v", result)
	}
	var payload interface{ ErrorPayload() map[string]any }
	if !errors.As(result, &payload) || payload.ErrorPayload()["code"] != "CLIENT_COMMAND_DENIED" {
		t.Fatalf("downstream payload changed: %T %v", result, result)
	}
}

func TestBuildWrapper_CannotOverrideDownstreamPayload(t *testing.T) {
	downstream := &platformapi.CommandDeniedError{Path: "workitem/create", Reason: "denied"}
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.payload",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				downstreamErr := next(ctx, invocation)
				return &stealingPayloadError{secret: "secret-wrapper-payload", cause: downstreamErr}
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return downstream })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	result := handler(context.Background(), nil)
	var payload interface{ ErrorPayload() map[string]any }
	if !errors.As(result, &payload) {
		t.Fatalf("safe delegated payload missing: %T %v", result, result)
	}
	record := payload.ErrorPayload()
	if record["code"] != "CLIENT_COMMAND_DENIED" || strings.Contains(record["message"].(string), "secret-wrapper-payload") {
		t.Fatalf("wrapper overrode downstream payload: %+v", record)
	}
}

func TestBuildWrapper_RejectsUntrustedPanickingPayloadBuilder(t *testing.T) {
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.payload-panic",
		wrapper: func(platformapi.Handler) platformapi.Handler {
			return func(context.Context, platformapi.Invocation) error { return &panickingPayloadError{} }
		},
	}, func(context.Context, platformapi.Invocation) error { return nil })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	requireSafeRuntimeFailure(t, handler(context.Background(), nil), "guard.payload-panic", "wrapper", "secret-panicking-payload")
}

func TestBuildWrapper_RejectsErrorThatPanicsDuringChainTraversal(t *testing.T) {
	handler, err := buildWrapper(wrapperEntry{
		name: "guard.error-chain",
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				_ = next(ctx, invocation)
				return &panickingTraversalError{}
			}
		},
	}, func(context.Context, platformapi.Invocation) error { return errors.New("downstream") })
	if err != nil {
		t.Fatalf("buildWrapper() error = %v", err)
	}
	result := handler(context.Background(), nil)
	requireSafeRuntimeFailure(t, result, "guard.error-chain", "wrapper", "secret-error-traversal")
	var unrelated interface{ Marker() }
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("wrapper error traversal escaped boundary: %v", recovered)
			}
		}()
		if errors.Is(result, errors.New("unrelated")) || errors.As(result, &unrelated) {
			t.Fatal("malicious wrapper error unexpectedly matched unrelated target")
		}
	}()
}

func TestRuntime_FailOpenWrapperBuildFailureBlocksCommand(t *testing.T) {
	tests := []struct {
		name    string
		wrapper platformapi.Wrapper
	}{
		{
			name: "factory panic",
			wrapper: func(platformapi.Handler) platformapi.Handler {
				panic("broken factory")
			},
		},
		{
			name:    "factory returns nil",
			wrapper: func(platformapi.Handler) platformapi.Handler { return nil },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			runtime := &Runtime{wrappers: []wrapperEntry{{
				name: "required.guard", selector: platformapi.All(), policy: platformapi.FailOpen, wrapper: tc.wrapper,
			}}}
			command := &cobra.Command{
				Use: "probe", SilenceErrors: true, SilenceUsage: true,
				RunE: func(*cobra.Command, []string) error { called = true; return nil },
			}
			runtime.Apply(command)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "required.guard") {
				t.Fatalf("execute error = %v, want wrapper build failure", err)
			}
			if called {
				t.Fatal("business command ran after wrapper construction failed")
			}
		})
	}
}

func TestRuntime_WrapperContextReachesCommand(t *testing.T) {
	type contextKey struct{}
	var got string
	runtime := &Runtime{wrappers: []wrapperEntry{{
		name: "context.inject", selector: platformapi.All(), policy: platformapi.FailClosed,
		wrapper: func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				return next(context.WithValue(ctx, contextKey{}, "from-wrapper"), invocation)
			}
		},
	}}}
	command := &cobra.Command{
		Use: "probe", SilenceErrors: true, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			got, _ = cmd.Context().Value(contextKey{}).(string)
			return nil
		},
	}
	runtime.Apply(command)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "from-wrapper" {
		t.Fatalf("command context value = %q, want wrapper value", got)
	}
}

func TestRuntime_RegistryRebuildAppliesHooksAndPolicyExactlyOnceToNewCommands(t *testing.T) {
	var observed []string
	var wrapped []string
	plugin := platformapi.NewPlugin("rebuild-governance", "1.0.0").
		FailClosed().
		Observer(platformapi.Before, "observe", platformapi.All(), func(_ context.Context, invocation platformapi.Invocation) {
			observed = append(observed, invocation.Cmd().Path())
		}).
		Wrap("wrap", platformapi.All(), func(next platformapi.Handler) platformapi.Handler {
			return func(ctx context.Context, invocation platformapi.Invocation) error {
				wrapped = append(wrapped, invocation.Cmd().Path())
				return next(ctx, invocation)
			}
		}).
		Restrict(&platformapi.Rule{Name: "deny-blocked", Allow: []string{"**"}, Deny: []string{"blocked"}}).
		MustBuild()
	runtime, err := installPlugins("1.0.0", []platformapi.Plugin{plugin})
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	setup := &rebuildRegistrySetup{tree: commandTree("before")}
	manager := registry.NewManager(setup)
	if err := manager.Init(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	pipelineCalls := 0
	app, err := cliapp.New(
		cliapp.WithAppName("rebuild-test"),
		cliapp.WithManager(manager),
		cliapp.WithPipeline(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{countPipelineStep{calls: &pipelineCalls}}}),
		cliapp.WithRootCommandCustomizer(func(root *cobra.Command, _ cliapp.RootCommandMetadata) { runtime.Apply(root) }),
	)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	output := &bytes.Buffer{}
	if err := app.ExecuteWithIO(context.Background(), []string{"before"}, output, output); err != nil {
		t.Fatalf("execute before rebuild: %v", err)
	}

	setup.tree = commandTree("after", "blocked")
	if err := manager.Rebuild(context.Background()); err != nil {
		t.Fatalf("rebuild manager: %v", err)
	}
	if err := app.ExecuteWithIO(context.Background(), []string{"after"}, output, output); err != nil {
		t.Fatalf("execute rebuilt command: %v", err)
	}
	if err := app.ExecuteWithIO(context.Background(), []string{"blocked"}, output, output); err == nil {
		t.Fatal("rebuilt denied command executed without a policy error")
	}

	if got := strings.Join(observed, ","); got != "before,after,blocked" {
		t.Fatalf("observed paths = %q, want each rebuilt command exactly once", got)
	}
	if got := strings.Join(wrapped, ","); got != "before,after" {
		t.Fatalf("wrapped paths = %q, want allowed commands exactly once", got)
	}
	if pipelineCalls != 2 {
		t.Fatalf("pipeline calls = %d, want only the two allowed commands", pipelineCalls)
	}
}

func commandTree(names ...string) *registry.CommandTree {
	nodes := make([]*registry.CommandNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, &registry.CommandNode{
			Name: name, HandlerRef: name, Help: registry.HelpText{Brief: name},
			Meta: registry.NodeMeta{Source: "mcp", Risk: "read", ToolName: name},
		})
	}
	return &registry.CommandTree{Nodes: nodes}
}

func TestRuntime_ShutdownRunsInReverseAndFailOpenContinues(t *testing.T) {
	var order []string
	runtime := &Runtime{lifecycles: []lifecycleEntry{
		{name: "first", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(context.Context, *platformapi.LifecycleContext) error {
			order = append(order, "first")
			return nil
		}},
		{name: "second", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(context.Context, *platformapi.LifecycleContext) error {
			order = append(order, "second")
			return errors.New("flush failed")
		}},
	}}
	if err := runtime.Emit(context.Background(), platformapi.Shutdown, nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := strings.Join(order, ","); got != "second,first" {
		t.Fatalf("shutdown order = %q", got)
	}
}

func TestRuntime_ShutdownFailClosedErrorStillRunsRemainingCleanup(t *testing.T) {
	var order []string
	cause := errors.New("secret-required-cleanup")
	runtime := &Runtime{lifecycles: []lifecycleEntry{
		{name: "first", event: platformapi.Shutdown, policy: platformapi.FailClosed, handler: func(context.Context, *platformapi.LifecycleContext) error {
			order = append(order, "first")
			return nil
		}},
		{name: "second", event: platformapi.Shutdown, policy: platformapi.FailClosed, handler: func(context.Context, *platformapi.LifecycleContext) error {
			order = append(order, "second")
			return cause
		}},
	}}
	err := runtime.Emit(context.Background(), platformapi.Shutdown, nil)
	requireSafeRuntimeFailure(t, err, "second", "lifecycle", "secret-required-cleanup")
	if got := strings.Join(order, ","); got != "second,first" {
		t.Fatalf("shutdown order after fail-closed error = %q", got)
	}
}

func TestRuntime_LifecyclePanicHonorsFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  platformapi.FailurePolicy
		wantErr bool
	}{
		{name: "fail-open", policy: platformapi.FailOpen},
		{name: "fail-closed", policy: platformapi.FailClosed, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &Runtime{lifecycles: []lifecycleEntry{{
				name: "lifecycle.panic", event: platformapi.Startup, policy: tc.policy,
				handler: func(context.Context, *platformapi.LifecycleContext) error { panic("lifecycle unavailable") },
			}}}
			err := runtime.Emit(context.Background(), platformapi.Startup, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Emit() error = %v, wantErr=%t", err, tc.wantErr)
			}
			if err != nil {
				requireSafeRuntimeFailure(t, err, "lifecycle.panic", "lifecycle", "lifecycle unavailable")
			}
		})
	}
}

func TestCallLifecycleBounded_ReturnsWhenHandlerIgnoresCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	entry := lifecycleEntry{
		name: "blocking", event: platformapi.Shutdown, policy: platformapi.FailClosed,
		handler: func(context.Context, *platformapi.LifecycleContext) error {
			<-release
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := callLifecycleBounded(ctx, entry, &platformapi.LifecycleContext{Event: platformapi.Shutdown})
	requireSafeRuntimeFailure(t, err, "blocking", "lifecycle", "deadline")
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("callLifecycleBounded() took %s", elapsed)
	}
}

func TestRuntime_StartupTimeoutHonorsFailurePolicy(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		policy  platformapi.FailurePolicy
		wantErr bool
	}{
		{name: "fail-open", policy: platformapi.FailOpen},
		{name: "fail-closed", policy: platformapi.FailClosed, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			release := make(chan struct{})
			defer close(release)
			var healthyCalls atomic.Int32
			lifecycles := []lifecycleEntry{{
				name: "blocking-startup", event: platformapi.Startup, policy: testCase.policy,
				handler: func(context.Context, *platformapi.LifecycleContext) error {
					<-release
					return nil
				},
			}}
			if testCase.policy == platformapi.FailOpen {
				lifecycles = append(lifecycles, lifecycleEntry{
					name: "healthy-startup", event: platformapi.Startup, policy: platformapi.FailClosed,
					handler: func(context.Context, *platformapi.LifecycleContext) error {
						healthyCalls.Add(1)
						return nil
					},
				})
			}
			runtime := &Runtime{lifecycles: lifecycles}
			begin := time.Now()
			err := runtime.emitWithLifecycleTimeouts(context.Background(), platformapi.Startup, nil, 20*time.Millisecond, time.Second)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("startup error = %v, wantErr=%t", err, testCase.wantErr)
			}
			if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
				t.Fatalf("startup timeout returned after %s", elapsed)
			}
			if testCase.policy == platformapi.FailOpen && healthyCalls.Load() != 1 {
				t.Fatalf("healthy startup calls = %d, want 1 after fail-open timeout", healthyCalls.Load())
			}
		})
	}
}

func TestRuntime_DenyPatternCannotBeBypassedByAnotherAllowRule(t *testing.T) {
	runtime := &Runtime{
		restrictor: "policy",
		rules: []ruleEntry{
			{plugin: "policy", rule: &platformapi.Rule{Name: "deny-delete", Deny: []string{"workitem/delete"}}},
			{plugin: "policy", rule: &platformapi.Rule{Name: "allow-workitem", Allow: []string{"workitem/**"}}},
		},
	}
	command := &cobra.Command{Use: "delete", Annotations: map[string]string{"command_path": "workitem/delete", "risk_level": "write"}}
	if denial := runtime.denial(commandView{command: command}); denial == nil || denial.ReasonCode != "path_denied" {
		t.Fatalf("denial = %#v, want global path_denied", denial)
	}
}

func TestRuntime_AllRestrictionRulesConstrainOverlappingCommand(t *testing.T) {
	runtime := &Runtime{
		restrictor: "policy",
		rules: []ruleEntry{
			{plugin: "policy", rule: &platformapi.Rule{
				Name: "workitem-readonly", Allow: []string{"workitem/**"}, MaxRisk: platformapi.RiskRead,
			}},
			{plugin: "policy", rule: &platformapi.Rule{
				Name: "global-high-risk", Allow: []string{"**"}, MaxRisk: platformapi.RiskHighRiskWrite,
			}},
		},
	}
	called := false
	root := &cobra.Command{Use: "meegle", SilenceErrors: true, SilenceUsage: true}
	workitem := &cobra.Command{Use: "workitem"}
	deleteCommand := &cobra.Command{
		Use: "delete",
		Annotations: map[string]string{
			"command_path": "workitem/delete",
			"risk_level":   platformapi.RiskHighRiskWrite.String(),
		},
		RunE: func(*cobra.Command, []string) error {
			called = true
			return nil
		},
	}
	workitem.AddCommand(deleteCommand)
	root.AddCommand(workitem)
	runtime.Apply(root)

	if !deleteCommand.Hidden {
		t.Fatal("workitem/delete is visible even though the narrower read-only rule denies it")
	}
	root.SetArgs([]string{"workitem", "delete"})
	err := root.Execute()
	var denial *platformapi.CommandDeniedError
	if !errors.As(err, &denial) || denial.RuleName != "workitem-readonly" || denial.ReasonCode != "risk_too_high" {
		t.Fatalf("Execute() error = %#v, want workitem-readonly risk_too_high denial", err)
	}
	if called {
		t.Fatal("business command ran despite a matching restriction rule denial")
	}
}

func TestRuntime_DenialAttributesFinalFailureToMatchingRule(t *testing.T) {
	runtime := &Runtime{
		restrictor: "policy",
		rules: []ruleEntry{
			{plugin: "policy", rule: &platformapi.Rule{Name: "path-scope", Allow: []string{"workitem/**"}}},
			{plugin: "policy", rule: &platformapi.Rule{Name: "read-only", MaxRisk: platformapi.RiskRead}},
		},
	}
	command := &cobra.Command{Use: "update", Annotations: map[string]string{"command_path": "project/update", "risk_level": "write"}}
	denial := runtime.denial(commandView{command: command})
	if denial == nil {
		t.Fatal("denial = nil")
	}
	if denial.RuleName != "read-only" || denial.ReasonCode != "risk_too_high" {
		t.Fatalf("denial attribution = %+v, want read-only/risk_too_high", denial)
	}
}

func TestRuntime_ShutdownGetsFreshBudgetFromCancelledExecutionContext(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "preserved"))
	cancel()
	called := false
	runtime := &Runtime{lifecycles: []lifecycleEntry{{
		name: "cleanup", event: platformapi.Shutdown, policy: platformapi.FailClosed,
		handler: func(hookCtx context.Context, lifecycle *platformapi.LifecycleContext) error {
			called = true
			if hookCtx.Err() != nil {
				t.Fatalf("shutdown context already cancelled: %v", hookCtx.Err())
			}
			if hookCtx.Value(contextKey{}) != "preserved" {
				t.Fatal("shutdown context lost execution-scoped values")
			}
			return nil
		},
	}}}
	if err := runtime.Emit(ctx, platformapi.Shutdown, nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !called {
		t.Fatal("shutdown hook was not called")
	}
}

func TestRuntime_ShutdownUsesPerHookLifecycleContext(t *testing.T) {
	var first, second *platformapi.LifecycleContext
	runtime := &Runtime{lifecycles: []lifecycleEntry{
		{name: "first", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(_ context.Context, lifecycle *platformapi.LifecycleContext) error {
			first = lifecycle
			return nil
		}},
		{name: "second", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(_ context.Context, lifecycle *platformapi.LifecycleContext) error {
			second = lifecycle
			return nil
		}},
	}}
	if err := runtime.Emit(context.Background(), platformapi.Shutdown, errors.New("business failure")); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if first == nil || second == nil || first == second {
		t.Fatalf("lifecycle contexts must be independent: first=%p second=%p", first, second)
	}
}

func TestRuntime_ShutdownDoesNotStartLaterHookAfterDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	laterCalled := make(chan struct{}, 1)
	runtime := &Runtime{lifecycles: []lifecycleEntry{
		{name: "later", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(context.Context, *platformapi.LifecycleContext) error {
			laterCalled <- struct{}{}
			return nil
		}},
		{name: "blocking", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(context.Context, *platformapi.LifecycleContext) error {
			<-release
			return nil
		}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.emitWithShutdownTimeout(ctx, platformapi.Shutdown, nil, 30*time.Millisecond); err != nil {
		t.Fatalf("fail-open shutdown timeout = %v, want logged and suppressed", err)
	}
	select {
	case <-laterCalled:
		t.Fatal("later shutdown hook started after the shared cleanup deadline")
	default:
	}
}

func TestRuntime_ShutdownTimeoutCannotHidePendingFailClosedHook(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	runtime := &Runtime{lifecycles: []lifecycleEntry{
		{name: "required-cleanup", event: platformapi.Shutdown, policy: platformapi.FailClosed, handler: func(context.Context, *platformapi.LifecycleContext) error {
			return nil
		}},
		{name: "optional-blocking", event: platformapi.Shutdown, policy: platformapi.FailOpen, handler: func(context.Context, *platformapi.LifecycleContext) error {
			<-release
			return nil
		}},
	}}
	err := runtime.emitWithShutdownTimeout(context.Background(), platformapi.Shutdown, nil, 20*time.Millisecond)
	requireSafeRuntimeFailure(t, err, "required-cleanup", "lifecycle", "deadline")
}
