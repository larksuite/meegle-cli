// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"strings"
	"testing"
)

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
