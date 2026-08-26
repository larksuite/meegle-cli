// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/larksuite/meegle-cli/extension/credential"
	"github.com/larksuite/meegle-cli/extension/transport"
	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"
)

type changingPriorityProvider struct {
	calls int
}

type diagnosticInvalidCredentialProvider struct{}

func (diagnosticInvalidCredentialProvider) Name() string { return "secret\ncredential-active: forged" }
func (diagnosticInvalidCredentialProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (diagnosticInvalidCredentialProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type diagnosticPanicTransportProvider struct{}

func (diagnosticPanicTransportProvider) Name() string { panic("secret provider failure") }
func (diagnosticPanicTransportProvider) ResolveInterceptor(context.Context) transport.Interceptor {
	return nil
}

type diagnosticInvalidNameTransportProvider struct{}

func (diagnosticInvalidNameTransportProvider) Name() string { return "secret\ntransport: forged" }
func (diagnosticInvalidNameTransportProvider) ResolveInterceptor(context.Context) transport.Interceptor {
	return nil
}

type diagnosticBlockingNameTransportProvider struct{ release <-chan struct{} }

func (p diagnosticBlockingNameTransportProvider) Name() string {
	<-p.release
	return "blocking-name"
}
func (diagnosticBlockingNameTransportProvider) ResolveInterceptor(context.Context) transport.Interceptor {
	return nil
}

func (*changingPriorityProvider) Name() string { return "changing-priority" }
func (p *changingPriorityProvider) Priority() int {
	p.calls++
	if p.calls == 1 {
		return 2
	}
	return 99
}
func (*changingPriorityProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (*changingPriorityProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

func TestExtensionDoctor_ReportsEffectiveAndSkippedComponentsWithoutSecrets(t *testing.T) {
	diagnostics := platformruntime.Diagnostics{
		Restrictor: "active-policy",
		Plugins: []platformruntime.PluginDiagnostic{
			{
				Name: "active-policy", Version: "1.2.3", Status: "active",
				FailurePolicy: "fail-closed", Restricts: true,
				Hooks: []platformruntime.HookDiagnostic{{Name: "active-policy.audit", Kind: "observer", Timing: "after", Selector: "custom"}},
				Rules: []string{"read-only"},
				RuleDetails: []platformruntime.RuleDiagnostic{{
					Name: "read-only", Allow: []string{"workitem/**"}, MaxRisk: "read", Identities: []string{"user"},
				}},
			},
			{Name: "optional-audit", Version: "2.0.0", Status: "skipped", FailurePolicy: "fail-open", FailureStage: "install"},
		},
	}
	identity := ResolvedIdentity{
		Token: "super-secret-token", Source: SourceExtension,
		CredentialProvider: "corp-sso", CredentialTokenSource: "oidc",
	}
	command := newExtensionCommand(diagnostics, identity, []ToolMappingIssue{{
		Code: "reserved_path", ToolName: "remote_auth_status", Path: "auth/status",
	}})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"doctor"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute doctor: %v", err)
	}

	got := output.String()
	for _, fragment := range []string{
		"credential-active: corp-sso token-source=oidc",
		"plugin: active-policy version=1.2.3 status=active policy=fail-closed restricts=true",
		"hook: active-policy.audit kind=observer timing=after selector=custom",
		"rule: read-only",
		"plugin: optional-audit version=2.0.0 status=skipped policy=fail-open restricts=false failure-stage=install",
		"policy: active-policy",
		"rule: read-only allow=workitem/** deny=none max-risk=read identities=user allow-unannotated=false",
		`dynamic-tool: "remote_auth_status" path="auth/status" status=skipped code=reserved_path`,
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("doctor output missing %q:\n%s", fragment, got)
		}
	}
	if strings.Contains(got, identity.Token) {
		t.Fatalf("doctor leaked token: %s", got)
	}
}

func TestDynamicToolDiagnosticsNeverPrintsSecretMaterial(t *testing.T) {
	command := newDynamicToolDiagnosticsCommand([]ToolMappingIssue{{
		Code: "invalid_command", ToolName: "corp_probe\nplugin: forged", Path: "corp_ops/run\x1b[31m",
	}})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute discovery diagnostics: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, `dynamic-tool: "corp_probe\nplugin: forged" path="corp_ops/run\x1b[31m" status=skipped code=invalid_command`) {
		t.Fatalf("missing deterministic diagnostic: %s", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("diagnostic injection created extra lines: %q", got)
	}
}

func TestTransportDiagnostics_IsolatesProviderNamePanicAndShowsSecurityBaseline(t *testing.T) {
	if os.Getenv("TRANSPORT_DIAGNOSTIC_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
		command.Env = append(os.Environ(), "TRANSPORT_DIAGNOSTIC_HELPER=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("transport diagnostic helper timed out: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("transport diagnostic helper failed: %v\n%s", err, output)
		}
		return
	}
	transport.Register(diagnosticPanicTransportProvider{})
	command := newTransportDiagnosticsCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute transport diagnostics: %v", err)
	}
	got := output.String()
	for _, fragment := range []string{"transport: <unavailable>", "hook-timeout=30s", "tls-downgrade=blocked", "redirects=10"} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("transport diagnostics missing %q: %s", fragment, got)
		}
	}
	if strings.Contains(got, "secret provider failure") {
		t.Fatalf("transport diagnostics leaked panic text: %s", got)
	}
}

func TestTransportDiagnostics_RejectsUntrustedProviderName(t *testing.T) {
	if runExtensionCommandSubprocess(t, "TRANSPORT_INVALID_NAME_HELPER") {
		return
	}
	transport.Register(diagnosticInvalidNameTransportProvider{})
	command := newTransportDiagnosticsCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute transport diagnostics: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "transport: <invalid>") || strings.Contains(got, "secret") || strings.Count(got, "\n") != 1 {
		t.Fatalf("unsafe transport diagnostics: %q", got)
	}
}

func TestTransportDiagnostics_ProviderNameHonorsCommandDeadline(t *testing.T) {
	if runExtensionCommandSubprocess(t, "TRANSPORT_BLOCKING_NAME_HELPER") {
		return
	}
	release := make(chan struct{})
	defer close(release)
	transport.Register(diagnosticBlockingNameTransportProvider{release: release})
	command := newTransportDiagnosticsCommand()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	command.SetContext(ctx)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	started := time.Now()
	if err := command.Execute(); err != nil {
		t.Fatalf("execute transport diagnostics: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("transport diagnostics ignored command deadline: %s", elapsed)
	}
	if got := output.String(); !strings.Contains(got, "transport: <unavailable>") {
		t.Fatalf("transport diagnostics = %q", got)
	}
}

func runExtensionCommandSubprocess(t *testing.T, marker string) bool {
	t.Helper()
	if os.Getenv(marker) == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
	command.Env = append(os.Environ(), marker+"=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("extension command helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("extension command helper failed: %v\n%s", err, output)
	}
	return true
}

func TestExtensionDoctor_ReportsCredentialPriorityFrozenAtRegistration(t *testing.T) {
	if os.Getenv("CREDENTIAL_DOCTOR_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExtensionDoctor_ReportsCredentialPriorityFrozenAtRegistration$")
		command.Env = append(os.Environ(), "CREDENTIAL_DOCTOR_HELPER=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("credential doctor helper timed out: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("credential doctor helper failed: %v\n%s", err, output)
		}
		return
	}
	credential.Register(&changingPriorityProvider{})
	command := newExtensionCommand(platformruntime.Diagnostics{}, ResolvedIdentity{}, nil)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"credentials"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute credentials: %v", err)
	}
	if got := output.String(); !strings.Contains(got, "credential: changing-priority priority=2") {
		t.Fatalf("credential diagnostics did not report frozen priority:\n%s", got)
	}
}

func TestCredentialDiagnostics_UsesValidatedFrozenProviderName(t *testing.T) {
	if runExtensionCommandSubprocess(t, "CREDENTIAL_INVALID_NAME_HELPER") {
		return
	}
	credential.Register(diagnosticInvalidCredentialProvider{})
	command := newCredentialDiagnosticsCommand(ResolvedIdentity{
		CredentialProvider:    "corp-sso",
		CredentialTokenSource: "secret\ncredential: forged",
	})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute credential diagnostics: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "credential: <invalid>") || !strings.Contains(got, "token-source=<invalid>") || strings.Contains(got, "secret") || strings.Contains(got, "forged") {
		t.Fatalf("unsafe credential diagnostics: %q", got)
	}
}
