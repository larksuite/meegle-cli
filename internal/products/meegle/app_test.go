// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	extcredential "github.com/larksuite/meegle-cli/extension/credential"
	extplatform "github.com/larksuite/meegle-cli/extension/platform"
	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

var errActualStartup = errors.New("actual startup sentinel")

type actualStartupCredential struct{}

func (actualStartupCredential) Name() string { return "startup-sentinel" }
func (actualStartupCredential) ResolveAccount(context.Context) (*extcredential.Account, error) {
	return nil, errActualStartup
}
func (actualStartupCredential) ResolveToken(context.Context, extcredential.TokenSpec) (*extcredential.Token, error) {
	return nil, nil
}

type actualStartupPlugin struct{}

func (actualStartupPlugin) Name() string    { return "startup-sentinel" }
func (actualStartupPlugin) Version() string { return "1.0.0" }
func (actualStartupPlugin) Capabilities() extplatform.Capabilities {
	return extplatform.Capabilities{FailurePolicy: extplatform.FailClosed}
}
func (actualStartupPlugin) Install(extplatform.Registrar) error { return errActualStartup }

func TestProfileNameFromArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		fallback string
		want     string
	}{
		{name: "separate value", args: []string{"workitem", "list", "--profile", "enterprise"}, fallback: "default", want: "enterprise"},
		{name: "equals value", args: []string{"--profile=enterprise", "workitem", "list"}, fallback: "default", want: "enterprise"},
		{name: "last value wins", args: []string{"--profile", "first", "--profile=second"}, fallback: "default", want: "second"},
		{name: "end of flags", args: []string{"--", "--profile", "ignored"}, fallback: "default", want: "default"},
		{name: "missing value", args: []string{"--profile"}, fallback: "default", want: "default"},
		{name: "next flag is not a value", args: []string{"--profile", "--refresh"}, fallback: "default", want: "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := profileNameFromArgs(tc.args, tc.fallback); got != tc.want {
				t.Fatalf("profileNameFromArgs(%v, %q) = %q, want %q", tc.args, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestFinalizePlugins_PreservesBusinessError(t *testing.T) {
	businessErr := errors.New("business failed")
	shutdownErr := errors.New("shutdown failed")
	if got := finalizePlugins(businessErr, shutdownErr); !errors.Is(got, businessErr) {
		t.Fatalf("finalizePlugins() = %v, want original business error", got)
	}
}

func TestFinalizePlugins_ReturnsShutdownErrorAfterSuccessfulCommand(t *testing.T) {
	shutdownErr := errors.New("shutdown failed")
	if got := finalizePlugins(nil, shutdownErr); !errors.Is(got, shutdownErr) {
		t.Fatalf("finalizePlugins() = %v, want shutdown error", got)
	}
}

func TestFinalizePlugins_SuccessfulFirstRunYieldsToShutdownError(t *testing.T) {
	shutdownErr := errors.New("shutdown failed")
	if got := finalizePlugins(ErrFirstRunSetupComplete, shutdownErr); !errors.Is(got, shutdownErr) {
		t.Fatalf("finalizePlugins() = %v, want shutdown error after successful first-run setup", got)
	}
}

func TestLifecycleCommandError_TreatsSuccessfulFirstRunAsSuccess(t *testing.T) {
	if got := lifecycleCommandError(ErrFirstRunSetupComplete); got != nil {
		t.Fatalf("lifecycleCommandError(ErrFirstRunSetupComplete) = %v, want nil", got)
	}
	businessErr := errors.New("business failed")
	if got := lifecycleCommandError(businessErr); !errors.Is(got, businessErr) {
		t.Fatalf("lifecycleCommandError(business error) = %v, want original error", got)
	}
}

func TestExtensionStartupErrorHasStableCodeAndPreservesCause(t *testing.T) {
	cause := errors.New("plugin install failed with token secret-startup-token")
	err := extensionStartupError("CLIENT_EXTENSION_INSTALL_FAILED", "failed to install CLI platform extensions", cause)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) {
		t.Fatalf("error type = %T, want *MeegleError", err)
	}
	if me.Code != "CLIENT_EXTENSION_INSTALL_FAILED" || me.ExitCode != 1 {
		t.Fatalf("mapped error = %+v", me)
	}
	if !errors.Is(err, cause) {
		t.Fatal("startup error does not preserve original cause")
	}
	if strings.Contains(me.Message, "secret-startup-token") || me.Message != "failed to install CLI platform extensions" {
		t.Fatalf("public startup message exposed internal cause: %q", me.Message)
	}
}

func TestNewCLIApp_MapsActualExtensionStartupFailures(t *testing.T) {
	mode := os.Getenv("ACTUAL_STARTUP_FAILURE_HELPER")
	if mode != "" {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("MEEGLE_HOST", "")
		t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")
		originalArgs := os.Args
		defer func() { os.Args = originalArgs }()
		os.Args = []string{"meegle", "workitem", "get"}
		wantCode := ""
		switch mode {
		case "credential":
			extcredential.Register(actualStartupCredential{})
			wantCode = "CLIENT_CREDENTIAL_RESOLUTION_FAILED"
		case "platform":
			extplatform.Register(actualStartupPlugin{})
			wantCode = "CLIENT_EXTENSION_INSTALL_FAILED"
		default:
			t.Fatalf("unknown helper mode %q", mode)
		}
		_, err := NewCLIApp("1.2.3", nil)
		var me *meerrors.MeegleError
		if !errors.As(err, &me) || me.Code != wantCode || !errors.Is(err, errActualStartup) {
			t.Fatalf("NewCLIApp() error = %#v, want code=%s cause=%v", err, wantCode, errActualStartup)
		}
		return
	}

	for _, mode := range []string{"credential", "platform"} {
		command := exec.Command(os.Args[0], "-test.run=^TestNewCLIApp_MapsActualExtensionStartupFailures$")
		command.Env = append(os.Environ(), "ACTUAL_STARTUP_FAILURE_HELPER="+mode)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s startup helper: %v\n%s", mode, err, output)
		}
	}
}

func TestBuildRegistrySetup_NilHeaders(t *testing.T) {
	setup := BuildRegistrySetup("example.com", nil)
	if setup == nil {
		t.Fatal("expected non-nil setup even with nil headers")
	}
}

func TestBuildRegistrySetup_WithHeaders(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer test"}
	setup := BuildRegistrySetup("example.com", headers)
	if setup == nil {
		t.Fatal("expected non-nil setup")
	}
}

func TestBuildRegistrySetup_EmptyHost(t *testing.T) {
	setup := BuildRegistrySetup("", nil)
	if setup == nil {
		t.Fatal("expected non-nil setup even with empty host")
	}
}

func TestRootCustomizerVersionFlagAlias(t *testing.T) {
	root := &cobra.Command{Use: "meegle"}
	rootCustomizer(nil, nil, nil, platformruntime.Diagnostics{}, ResolvedIdentity{})(root, cliapp.RootCommandMetadata{
		AppName: "meegle",
		Version: "1.2.3",
	})

	stdout := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stdout)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "1.2.3" {
		t.Fatalf("--version output = %q, want %q", got, "1.2.3")
	}
}

func TestRootCustomizer_PreservesLegacyStaticCommandSurface(t *testing.T) {
	root := &cobra.Command{Use: "meegle"}
	static := &StaticCommands{
		Auth:       &cobra.Command{Use: "auth"},
		Config:     &cobra.Command{Use: "config"},
		Inspect:    &cobra.Command{Use: "inspect"},
		Completion: &cobra.Command{Use: "completion"},
		URL:        &cobra.Command{Use: "url"},
	}

	rootCustomizer(static, nil, nil, platformruntime.Diagnostics{}, ResolvedIdentity{})(root, cliapp.RootCommandMetadata{
		AppName: "meegle",
		Version: "1.2.3",
	})

	var got []string
	for _, command := range root.Commands() {
		got = append(got, command.Name())
	}
	sort.Strings(got)
	want := []string{"auth", "completion", "config", "extension", "inspect", "url"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("root commands = %v, want legacy commands plus documented extension command %v", got, want)
	}
}

func TestAnnotateStaticCommands_AddsStableSourceAndRisk(t *testing.T) {
	root := &cobra.Command{Use: "meegle"}
	status := &cobra.Command{Use: "status", RunE: func(*cobra.Command, []string) error { return nil }}
	auth := &cobra.Command{Use: "auth"}
	auth.AddCommand(status)
	root.AddCommand(auth)

	annotateStaticCommands(root)
	if got := status.Annotations["command_id"]; got != "static:auth/status" {
		t.Fatalf("command_id = %q", got)
	}
	if got := status.Annotations["command_source"]; got != "static" {
		t.Fatalf("command_source = %q", got)
	}
	if got := status.Annotations["risk_level"]; got != "read" {
		t.Fatalf("risk_level = %q", got)
	}
}

// Startup-time env-var bootstrapping: without this, dynamic commands never
// get registered because the MCP client is nil and tool discovery is skipped.
func TestBuildMcpClient_EnvVarsBootstrapZeroConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "env.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env_bootstrap")

	client, ident := buildMcpClient(MeegleConfig{}, "default")
	if client == nil {
		t.Fatal("expected non-nil client when env vars provide host+token")
	}
	if ident.TokenManager != nil {
		t.Error("env-var mode must not attach a TokenManager")
	}
	if ident.Source != SourceEnv {
		t.Errorf("expected SourceEnv, got %v", ident.Source)
	}
}

func TestBuildMcpClient_EnvHostSanitizedAtStartup(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "https://env.example.com/path")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env")

	client, _ := buildMcpClient(MeegleConfig{}, "default")
	if client == nil {
		t.Fatal("expected non-nil client when env vars provide host+token")
	}
}

// When MEEGLE_USER_AGENT is set, buildMcpClient must resolve it into
// ident.UserAgentCaller so that the startup client is configured with the
// caller suffix. We can't read mcpclient.Client's unexported userAgent field,
// so this pins the handoff from ResolveIdentity into buildMcpClient's opts.
func TestBuildMcpClient_AppendsUserAgentCaller(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "env.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env")
	t.Setenv("MEEGLE_USER_AGENT", "ci-runner")

	client, ident := buildMcpClient(MeegleConfig{}, "default")
	if client == nil {
		t.Fatal("expected non-nil client when env vars provide host+token")
	}
	if ident.UserAgentCaller != "ci-runner" {
		t.Errorf("expected ident.UserAgentCaller=ci-runner, got %q", ident.UserAgentCaller)
	}
}

// A config-sourced user_agent flows through ResolveIdentity too (env unset).
func TestBuildMcpClient_UserAgentFromConfig(t *testing.T) {
	setupTestDir(t)
	// setupTestDir already clears MEEGLE_HOST / MEEGLE_USER_ACCESS_TOKEN /
	// MEEGLE_USER_AGENT; we intentionally rely on the config struct below.

	cfg := MeegleConfig{
		Host:        "cfg.example.com",
		AccessToken: "tok_cfg",
		UserAgent:   "svc-from-cfg/1.0",
	}
	client, ident := buildMcpClient(cfg, "default")
	if client == nil {
		t.Fatal("expected non-nil client when config provides host+token")
	}
	if ident.UserAgentCaller != "svc-from-cfg/1.0" {
		t.Errorf("expected ident.UserAgentCaller=svc-from-cfg/1.0, got %q", ident.UserAgentCaller)
	}
}

// The "Not logged in" note must name the exact rotation knob the user can
// reach based on which source the active token came from, AND surface
// `auth login` as a fallback so a user without a fresh token still has a
// recovery path. For env/config sources the auth login line must include
// the unset/clear step — otherwise ResolveIdentity's env > config > store
// priority leaves the new keychain token shadowed by the stale one.
func TestNotLoggedInNote_SourceSpecificRotationHint(t *testing.T) {
	cases := []struct {
		src     IdentitySource
		mustInc []string
	}{
		{SourceEnv, []string{"MEEGLE_USER_ACCESS_TOKEN", "unset MEEGLE_USER_ACCESS_TOKEN", "meegle auth login"}},
		{SourceConfig, []string{"meegle config set user_access_token", "meegle auth login"}},
		{SourceStore, []string{"meegle auth login"}},
		{SourceUnset, []string{"meegle auth login"}},
	}
	for _, tc := range cases {
		got := notLoggedInNote(tc.src)
		for _, frag := range tc.mustInc {
			if !strings.Contains(got, frag) {
				t.Errorf("source=%v: expected note to contain %q, got:\n%s", tc.src, frag, got)
			}
		}
	}
}
