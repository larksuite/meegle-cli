// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"testing"
)

// Priority: MEEGLE_USER_ACCESS_TOKEN overrides the config's user_access_token
// and the keychain. The env source never attaches a TokenManager — there is
// no local refresh path.
func TestResolveIdentity_EnvBeatsConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "env.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:        "cfg.example.com",
		AccessToken: "tok_cfg",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.Host != "env.example.com" {
		t.Errorf("expected env host to win, got %q", ident.Host)
	}
	if ident.Token != "tok_env" {
		t.Errorf("expected env token to win, got %q", ident.Token)
	}
	if ident.Source != SourceEnv {
		t.Errorf("expected SourceEnv, got %v", ident.Source)
	}
	if ident.TokenManager != nil || ident.Store != nil {
		t.Error("env source must not attach TokenManager or Store")
	}
}

// Priority: when env is unset the config's user_access_token is used. The
// source is SourceConfig and the token is not refreshable (no TokenManager).
func TestResolveIdentity_ConfigWhenEnvAbsent(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:        "cfg.example.com",
		AccessToken: "tok_cfg",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.Host != "cfg.example.com" {
		t.Errorf("expected config host, got %q", ident.Host)
	}
	if ident.Token != "tok_cfg" {
		t.Errorf("expected config token, got %q", ident.Token)
	}
	if ident.Source != SourceConfig {
		t.Errorf("expected SourceConfig, got %v", ident.Source)
	}
	if ident.TokenManager != nil {
		t.Error("config source must not attach TokenManager")
	}
}

// When neither env nor config provide a token and no login has been performed,
// Source is SourceUnset and callers can decide whether to degrade or prompt.
func TestResolveIdentity_UnsetWhenNothingConfigured(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")

	ident, err := ResolveIdentity(MeegleConfig{Host: "cfg.example.com"}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.Source != SourceUnset {
		t.Errorf("expected SourceUnset, got %v", ident.Source)
	}
	if ident.Token != "" {
		t.Errorf("expected empty token, got %q", ident.Token)
	}
	if ident.Host != "cfg.example.com" {
		t.Errorf("expected host to still be set from config, got %q", ident.Host)
	}
}

// A full URL in MEEGLE_HOST should be reduced to the bare host before any
// downstream URL templating. Symmetric with what SessionStep used to do.
func TestResolveIdentity_SanitizesHost(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "https://env.example.com/some/path")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok")

	ident, err := ResolveIdentity(MeegleConfig{}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.Host != "env.example.com" {
		t.Errorf("expected sanitized host, got %q", ident.Host)
	}
}

// Custom access_token_header propagates from config so downstream can send
// the token in the named header (raw, no Bearer) instead of Authorization.
func TestResolveIdentity_AccessTokenHeader_FromConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")
	t.Setenv("MEEGLE_ACCESS_TOKEN_HEADER", "")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:              "custom.example.com",
		AccessToken:       "u-custom",
		AccessTokenHeader: "x-meegle-auth",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.AccessTokenHeader != "x-meegle-auth" {
		t.Errorf("expected x-meegle-auth, got %q", ident.AccessTokenHeader)
	}
	if ident.Token != "u-custom" {
		t.Errorf("expected token u-custom, got %q", ident.Token)
	}
}

// MEEGLE_ACCESS_TOKEN_HEADER env var overrides config, mirroring the
// priority for Host / Token. This lets operators flip backends without
// touching committed config.
func TestResolveIdentity_AccessTokenHeader_EnvOverridesConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok")
	t.Setenv("MEEGLE_ACCESS_TOKEN_HEADER", "x-meegle-auth")

	ident, err := ResolveIdentity(MeegleConfig{
		AccessTokenHeader: "x-other-header",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.AccessTokenHeader != "x-meegle-auth" {
		t.Errorf("expected env to win, got %q", ident.AccessTokenHeader)
	}
}

// An unresolved ${VAR} placeholder in the config should fail fast with a
// descriptive error — this is the only error path for ResolveIdentity.
func TestResolveIdentity_UnresolvedPlaceholderFailsFast(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")

	_, err := ResolveIdentity(MeegleConfig{
		Host:        "cfg.example.com",
		AccessToken: "${DEFINITELY_NOT_SET_XYZ}",
	}, "default")
	if err == nil {
		t.Fatal("expected error for unresolved placeholder")
	}
}

// MEEGLE_USER_AGENT must override config.user_agent, mirroring the priority
// rule used by the other identity fields.
func TestResolveIdentity_UserAgent_EnvBeatsConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_AGENT", "ci-runner")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:      "h.example.com",
		UserAgent: "should-lose",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.UserAgentCaller != "ci-runner" {
		t.Errorf("expected env caller to win, got %q", ident.UserAgentCaller)
	}
}

// When the env var is unset, the expanded config.user_agent becomes the caller.
func TestResolveIdentity_UserAgent_ConfigWhenEnvAbsent(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_AGENT", "")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:      "h.example.com",
		UserAgent: "my-svc/1.0",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.UserAgentCaller != "my-svc/1.0" {
		t.Errorf("expected config caller, got %q", ident.UserAgentCaller)
	}
}

// No env, no config → empty caller (means "do not append anything").
func TestResolveIdentity_UserAgent_EmptyWhenNothingSet(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_AGENT", "")

	ident, err := ResolveIdentity(MeegleConfig{Host: "h.example.com"}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.UserAgentCaller != "" {
		t.Errorf("expected empty caller, got %q", ident.UserAgentCaller)
	}
}

// ${VAR} expansion in user_agent flows through ResolveIdentity's ExpandEnv
// call and returns the resolved value as the caller.
func TestResolveIdentity_UserAgent_ConfigTemplateExpanded(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_USER_AGENT", "")
	t.Setenv("MEEGLE_TEST_UA_SVC", "svc-from-env/2.0")

	ident, err := ResolveIdentity(MeegleConfig{
		Host:      "h.example.com",
		UserAgent: "${MEEGLE_TEST_UA_SVC}",
	}, "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ident.UserAgentCaller != "svc-from-env/2.0" {
		t.Errorf("expected expanded caller, got %q", ident.UserAgentCaller)
	}
}
