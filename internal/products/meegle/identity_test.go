// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/larksuite/meegle-cli/extension/credential"
)

type snapshotRegisteringProvider struct{}

func (snapshotRegisteringProvider) Name() string { return "snapshot-registering" }
func (snapshotRegisteringProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	credential.Register(snapshotLateProvider{})
	return nil, nil
}
func (snapshotRegisteringProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type snapshotLateProvider struct{}

func (snapshotLateProvider) Name() string { return "snapshot-late" }
func (snapshotLateProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (snapshotLateProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return &credential.Token{Value: "late-token"}, nil
}

type contractCredentialProvider struct{ mode string }

var (
	errContractAccountUnavailable = errors.New("account unavailable")
	errContractTokenUnavailable   = errors.New("token unavailable")
	errContractAccountPanic       = errors.New("secret account panic")
	errContractTokenPanic         = errors.New("secret token panic")
)

func (contractCredentialProvider) Name() string { return "contract-provider" }
func (p contractCredentialProvider) ResolveAccount(ctx context.Context) (*credential.Account, error) {
	switch p.mode {
	case "invalid-config-takeover":
		return &credential.Account{Host: "enterprise.example.com"}, nil
	case "account-error":
		return nil, errContractAccountUnavailable
	case "account-panic":
		panic(errContractAccountPanic)
	case "account-block":
		select {}
	case "context-canceled":
		return nil, ctx.Err()
	default:
		return nil, nil
	}
}
func (p contractCredentialProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	switch p.mode {
	case "invalid-config-takeover":
		return &credential.Token{Value: "enterprise-token", Source: "enterprise"}, nil
	case "invalid-config-token-only":
		return &credential.Token{Value: "enterprise-token", Source: "enterprise"}, nil
	case "token-error":
		return nil, errContractTokenUnavailable
	case "token-panic":
		panic(errContractTokenPanic)
	case "token-block":
		select {}
	case "token-source-invalid":
		return &credential.Token{Value: "enterprise-token", Source: "secret\ncredential: forged"}, nil
	case "token-header-invalid":
		return &credential.Token{Value: "enterprise-token", Header: "X-Valid\r\nX-Forged"}, nil
	default:
		return nil, nil
	}
}

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

func TestCLIIdentityContext_RoundTrip(t *testing.T) {
	want := ResolvedIdentity{Host: "example.com", Token: "secret", Source: SourceExtension}
	ctx := WithCLIIdentity(context.Background(), want)
	got, ok := CLIIdentityFromContext(ctx)
	if !ok || got.Host != want.Host || got.Token != want.Token || got.Source != want.Source {
		t.Fatalf("CLIIdentityFromContext() = (%+v, %v), want %+v", got, ok, want)
	}
}

func TestResolveCLIIdentity_FreezesOneProviderSnapshotForAccountAndToken(t *testing.T) {
	if os.Getenv("CREDENTIAL_SNAPSHOT_HELPER") == "1" {
		setupTestDir(t)
		credential.Register(snapshotRegisteringProvider{})
		identity, err := ResolveCLIIdentity(context.Background(), MeegleConfig{
			Host:        "builtin.example.com",
			AccessToken: "builtin-token",
		}, "default")
		if err != nil {
			t.Fatalf("ResolveCLIIdentity(): %v", err)
		}
		if identity.Source == SourceExtension || identity.Token != "builtin-token" {
			t.Fatalf("identity = source %s token %q; provider registered during resolution must wait for the next execution", identity.Source, identity.Token)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
	command.Env = append(os.Environ(), "CREDENTIAL_SNAPSHOT_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("credential snapshot helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("credential snapshot helper failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

func TestResolveCLIIdentity_ProviderFailureContracts(t *testing.T) {
	if mode := os.Getenv("CREDENTIAL_CONTRACT_HELPER"); mode != "" {
		setupTestDir(t)
		credential.Register(contractCredentialProvider{mode: mode})
		ctx := context.Background()
		if mode == "context-canceled" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		} else if mode == "account-block" || mode == "token-block" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
			defer cancel()
		}
		cfg := MeegleConfig{Host: "builtin.example.com", AccessToken: "builtin-token"}
		if mode == "invalid-config-takeover" || mode == "invalid-config-token-only" || mode == "invalid-config-no-token" {
			cfg.AccessToken = "${CREDENTIAL_CONTRACT_MISSING_ENV}"
		}
		identity, err := ResolveCLIIdentity(ctx, cfg, "default")
		if mode == "skip" {
			if err != nil || identity.Source != SourceConfig || identity.Token != "builtin-token" {
				t.Fatalf("skip fallback identity=%+v err=%v", identity, err)
			}
			return
		}
		if mode == "invalid-config-takeover" {
			if err != nil || identity.Host != "enterprise.example.com" || identity.Token != "enterprise-token" || identity.Source != SourceExtension {
				t.Fatalf("enterprise takeover identity=%+v err=%v", identity, err)
			}
			return
		}
		if mode == "invalid-config-token-only" {
			if err == nil || !strings.Contains(err.Error(), "CREDENTIAL_CONTRACT_MISSING_ENV") {
				t.Fatalf("token-only takeover identity=%+v err=%v, want original config error", identity, err)
			}
			return
		}
		if mode == "invalid-config-no-token" {
			if err == nil || !strings.Contains(err.Error(), "CREDENTIAL_CONTRACT_MISSING_ENV") {
				t.Fatalf("missing extension token identity=%+v err=%v, want original config error", identity, err)
			}
			return
		}
		if mode == "token-source-invalid" {
			if err != nil || identity.CredentialTokenSource != "<invalid>" || identity.Token != "enterprise-token" {
				t.Fatalf("invalid token source identity=%+v err=%v", identity, err)
			}
			return
		}
		if err == nil {
			t.Fatalf("mode %s returned nil error", mode)
		}
		if mode == "context-canceled" && !errors.Is(err, context.Canceled) {
			t.Fatalf("context error = %v, want context.Canceled", err)
		}
		switch mode {
		case "account-error":
			if !errors.Is(err, errContractAccountUnavailable) ||
				!strings.Contains(err.Error(), `credential provider "contract-provider"`) ||
				!strings.Contains(err.Error(), "resolve account") {
				t.Fatalf("account error = %v, want provider/stage context with preserved cause", err)
			}
		case "token-error":
			if !errors.Is(err, errContractTokenUnavailable) ||
				!strings.Contains(err.Error(), `credential provider "contract-provider"`) ||
				!strings.Contains(err.Error(), "resolve token") {
				t.Fatalf("token error = %v, want provider/stage context with preserved cause", err)
			}
		case "account-panic":
			if !errors.Is(err, errContractAccountPanic) {
				t.Fatalf("account panic chain lost sentinel: %v", err)
			}
		case "token-panic":
			if !errors.Is(err, errContractTokenPanic) {
				t.Fatalf("token panic chain lost sentinel: %v", err)
			}
		case "account-block", "token-block":
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocking callback error = %v, want deadline exceeded", err)
			}
		case "token-header-invalid":
			if !strings.Contains(err.Error(), "invalid token header") || strings.Contains(err.Error(), "X-Forged") {
				t.Fatalf("invalid token header error = %v", err)
			}
		}
		if strings.Contains(err.Error(), "panic") && !strings.Contains(err.Error(), "contract-provider") {
			t.Fatalf("panic error lacks provider name: %v", err)
		}
		return
	}

	for _, mode := range []string{"skip", "invalid-config-takeover", "invalid-config-token-only", "invalid-config-no-token", "account-error", "account-panic", "account-block", "token-error", "token-panic", "token-block", "token-source-invalid", "token-header-invalid", "context-canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResolveCLIIdentity_ProviderFailureContracts$")
			command.Env = append(os.Environ(), "CREDENTIAL_CONTRACT_HELPER="+mode)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("mode %s timed out: %v\n%s", mode, ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("mode %s failed: %v\n%s", mode, err, fmt.Sprintf("%s", output))
			}
		})
	}
}
