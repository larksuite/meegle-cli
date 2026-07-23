// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

func TestSessionStep_SkipsAuthCommand(t *testing.T) {
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"auth", "login"},
			Flags:    map[string]any{},
		},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error for auth command, got: %v", err)
	}
}

func TestSessionStep_SkipsConfigCommand(t *testing.T) {
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"config", "init"},
			Flags:    map[string]any{},
		},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error for config command, got: %v", err)
	}
}

func TestSessionStep_SkipsInspectCommand(t *testing.T) {
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"inspect"},
			Flags:    map[string]any{},
		},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error for inspect command, got: %v", err)
	}
}

func TestSessionStep_NilState(t *testing.T) {
	step := &SessionStep{}
	err := step.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error for nil state, got: %v", err)
	}
}

func TestSessionStep_UsesPreInjectedValues(t *testing.T) {
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.injected": true,
			"mcp.host":     "injected.example.com",
			"mcp.headers":  map[string]string{"X-Custom-Auth": "some-token"},
		},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.OutputConfig["mcp.host"] != "injected.example.com" {
		t.Errorf("expected host to remain injected.example.com, got %v", state.OutputConfig["mcp.host"])
	}
}

func TestSessionStep_SkipsWithInjectedNoAuthHeader(t *testing.T) {
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.injected": true,
			"mcp.host":     "example.com",
			"mcp.headers":  map[string]string{"X-Tenant": "abc"},
		},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error for injected without Authorization, got: %v", err)
	}
}

func TestSessionStep_ConfigAccessTokenBypassesKeychain(t *testing.T) {
	setupTestDir(t)
	t.Setenv("CI_TOKEN_TEST", "tok_from_env")
	if err := SaveProfileConfig("default", MeegleConfig{
		Host:        "meegle.com",
		AccessToken: "${CI_TOKEN_TEST}",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.token"]; got != "tok_from_env" {
		t.Errorf("expected token from env, got %v", got)
	}
	if _, ok := state.OutputConfig["mcp.token_manager"]; ok {
		t.Error("config-token mode must not attach a token manager")
	}
	if _, ok := state.OutputConfig["mcp.store"]; ok {
		t.Error("config-token mode must not attach a token store")
	}
}

func TestSessionStep_EnvVarsOverrideProfile(t *testing.T) {
	setupTestDir(t)
	if err := SaveProfileConfig("default", MeegleConfig{
		Host: "profile.example.com",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	t.Setenv("MEEGLE_HOST", "env.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_from_env_var")

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.host"]; got != "env.example.com" {
		t.Errorf("expected env host to win, got %v", got)
	}
	if got := state.OutputConfig["mcp.token"]; got != "tok_from_env_var" {
		t.Errorf("expected env token to win, got %v", got)
	}
	if _, ok := state.OutputConfig["mcp.token_manager"]; ok {
		t.Error("env-var mode must not attach a token manager")
	}
}

func TestSessionStep_EnvVarsEnableZeroConfig(t *testing.T) {
	setupTestDir(t)
	// No profile saved at all.
	t.Setenv("MEEGLE_HOST", "zero.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_zero")

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.host"]; got != "zero.example.com" {
		t.Errorf("expected zero-config env host, got %v", got)
	}
	if got := state.OutputConfig["mcp.token"]; got != "tok_zero" {
		t.Errorf("expected zero-config env token, got %v", got)
	}
}

func TestSessionStep_EnvHostIsSanitized(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "https://env.example.com/path")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env")

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.host"]; got != "env.example.com" {
		t.Errorf("expected sanitized env host, got %v", got)
	}
}

// SessionStep must forward the resolved access_token_header into OutputConfig
// under `mcp.access_token_header`. ExecutorStep reads that key to decide
// which HTTP header the token lands in; losing it here silently falls back
// to Authorization: Bearer and the request fails server-side.
func TestSessionStep_AccessTokenHeader_PropagatesToOutputConfig(t *testing.T) {
	setupTestDir(t)
	if err := SaveProfileConfig("default", MeegleConfig{
		Host:              "meegle.com",
		AccessToken:       "tok",
		AccessTokenHeader: "x-meegle-auth",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.access_token_header"]; got != "x-meegle-auth" {
		t.Errorf("expected mcp.access_token_header = x-meegle-auth, got %v", got)
	}
}

// MEEGLE_ACCESS_TOKEN_HEADER env override must also reach OutputConfig, so
// that operators can flip the auth header at runtime without editing config.
func TestSessionStep_AccessTokenHeader_EnvOverride(t *testing.T) {
	setupTestDir(t)
	if err := SaveProfileConfig("default", MeegleConfig{
		Host:              "meegle.com",
		AccessToken:       "tok",
		AccessTokenHeader: "x-from-config",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	t.Setenv("MEEGLE_ACCESS_TOKEN_HEADER", "x-from-env")

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.access_token_header"]; got != "x-from-env" {
		t.Errorf("expected env override to win, got %v", got)
	}
}

func TestSessionStep_SanitizesHostWithScheme(t *testing.T) {
	setupTestDir(t)
	if err := SaveProfileConfig("default", MeegleConfig{
		Host:        "https://meegle.com/extra/path",
		AccessToken: "tok_literal",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	if got := state.OutputConfig["mcp.host"]; got != "meegle.com" {
		t.Errorf("expected sanitized host 'meegle.com', got %v", got)
	}
}

func TestSessionStep_ConfigAccessTokenMissingEnvFailsFast(t *testing.T) {
	setupTestDir(t)
	// Ensure the referenced env var is unset.
	t.Setenv("CI_TOKEN_UNSET", "") // Setenv then Unsetenv to guarantee cleanup
	if err := os.Unsetenv("CI_TOKEN_UNSET"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	if err := SaveProfileConfig("default", MeegleConfig{
		Host:        "meegle.com",
		AccessToken: "${CI_TOKEN_UNSET}",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
	}
	err := step.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected fail-fast error for unset env var")
	}
	if !strings.Contains(err.Error(), "CI_TOKEN_UNSET") {
		t.Errorf("expected error to mention var name, got: %v", err)
	}
}

func TestMeegleValidateStep_RejectsMissingRequired(t *testing.T) {
	step := &MeegleValidateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"work-item", "get-brief"},
			Flags:    map[string]any{},
			Node: &registry.CommandNode{
				Flags: []registry.FlagDef{
					{Name: "work_item_id", Required: true, Type: "string"},
				},
			},
		},
		Values: map[string]any{},
	}
	err := step.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
	want := "missing required parameter: --work_item_id"
	if err.Error() != want {
		t.Fatalf("expected error message %q, got %q", want, err.Error())
	}
}

func TestMeegleValidateStep_PassesWhenPresent(t *testing.T) {
	step := &MeegleValidateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath:      []string{"work-item", "get-brief"},
			Flags:         map[string]any{"work_item_id": "123"},
			ExplicitFlags: map[string]any{"work_item_id": "123"},
			Node: &registry.CommandNode{
				Flags: []registry.FlagDef{
					{Name: "work_item_id", Required: true, Type: "string"},
				},
			},
		},
		Values: map[string]any{"work_item_id": "123"},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error when required param present, got: %v", err)
	}
}

func TestMeegleValidateStep_NilState(t *testing.T) {
	step := &MeegleValidateStep{}
	err := step.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error for nil state, got: %v", err)
	}
}

func TestMeegleValidateStep_BuildsValuesWhenNil(t *testing.T) {
	step := &MeegleValidateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath:      []string{"work-item", "get-brief"},
			Flags:         map[string]any{"work_item_id": "456"},
			ExplicitFlags: map[string]any{"work_item_id": "456"},
			Node: &registry.CommandNode{
				Flags: []registry.FlagDef{
					{Name: "work_item_id", Required: true, Type: "string"},
				},
			},
		},
		// Values is nil — step should build them from Parsed
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if state.Values == nil {
		t.Fatal("expected Values to be populated")
	}
}

// End-to-end check that a custom access_token_header actually reaches the
// HTTP request. Previously startup and runtime had separate code paths; the
// guarantee here is that both converge on the same header semantics. Also
// asserts Authorization is entirely absent — the whole point of the custom
// header mode is to placate backends that reject carrying both.
func TestMcpExecutorStep_AppliesCustomAuthHeader(t *testing.T) {
	var capturedAuth, capturedCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustom = r.Header.Get("x-meegle-auth")
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Minimal tools/call response — just enough for McpExecutorStep to
		// parse without error.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":                "ignored.example.com",
			"mcp.server_url":          server.URL,
			"mcp.token":               "u-custom",
			"mcp.headers":             map[string]string{},
			"mcp.access_token_header": "x-meegle-auth",
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	if capturedCustom != "u-custom" {
		t.Errorf("expected x-meegle-auth: u-custom, got %q", capturedCustom)
	}
	if capturedAuth != "" {
		t.Errorf("expected Authorization to be absent, got %q", capturedAuth)
	}
}

// When a header value with the same name as access_token_header is present
// in mcp.headers, it must not leak into the request — tokenFunc is the only
// writer for that header (otherwise we'd double-set and risk a mismatch).
func TestMcpExecutorStep_CustomAuthHeader_OverridesStaticHeader(t *testing.T) {
	var capturedCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCustom = r.Header.Get("x-meegle-auth")
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":                "ignored.example.com",
			"mcp.server_url":          server.URL,
			"mcp.token":               "live-token",
			"mcp.headers":             map[string]string{"x-meegle-auth": "STALE-SHOULD-NOT-LEAK"},
			"mcp.access_token_header": "x-meegle-auth",
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	if capturedCustom != "live-token" {
		t.Errorf("expected live-token (from tokenFunc), got %q", capturedCustom)
	}
}

func TestMcpExecutorStep_RefreshRetryUsesFreshStoredToken(t *testing.T) {
	store := &memTokenStore{data: &auth.TokenData{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ClientID:     "client-id",
	}}

	var oauthServer *httptest.Server
	oauthServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 "https://issuer.example",
				"authorization_endpoint": "https://issuer.example/auth",
				"registration_endpoint":  "https://issuer.example/register",
				"token_endpoint":         oauthServer.URL + "/token",
			})
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"fresh-token","refresh_token":"refresh-token","expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer oauthServer.Close()

	previousClient := http.DefaultClient
	http.DefaultClient = oauthServer.Client()
	t.Cleanup(func() { http.DefaultClient = previousClient })

	var seenTokens []string
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		seenTokens = append(seenTokens, token)
		if token == "stale-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if token != "fresh-token" {
			t.Errorf("unexpected token %q", token)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		})
	}))
	defer mcpServer.Close()

	tm := auth.NewTokenManager(store, strings.TrimPrefix(oauthServer.URL, "https://"))
	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":          "ignored.example.com",
			"mcp.server_url":    mcpServer.URL,
			"mcp.token":         "stale-token",
			"mcp.headers":       map[string]string{},
			"mcp.store":         auth.TokenStore(store),
			"mcp.token_manager": tm,
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed after refresh: %v", err)
	}
	if len(seenTokens) != 2 || seenTokens[0] != "stale-token" || seenTokens[1] != "fresh-token" {
		t.Fatalf("expected retry with refreshed token, got %v", seenTokens)
	}
}

func TestMcpExecutorStep_MyWorkTodoActionInfoErrorAddsSuggestion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result": map[string]any{
				"isError": true,
				"content": []map[string]any{{
					"type": "text",
					"text": "id=1000050815, code=1000050815, message=Invalid parameter, chain=[bytedance.bits.search:get action info fail, please retry]",
				}},
			},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"mywork", "todo"},
			Node:     &registry.CommandNode{HandlerRef: "list_todo"},
			Flags: map[string]any{
				"action":   "this_week",
				"page-num": "1",
			},
			ExplicitFlags: map[string]any{
				"action":   "this_week",
				"page-num": "1",
			},
		},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}

	err := step.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected backend error")
	}
	var me *meerrors.MeegleError
	if !strings.Contains(err.Error(), "get action info fail") || !errors.As(err, &me) {
		t.Fatalf("expected MeegleError with backend message, got %T %v", err, err)
	}
	if !strings.Contains(me.Suggestion, "--asset-key Asset_xxx") {
		t.Fatalf("expected asset-key suggestion, got %q", me.Suggestion)
	}
	if !strings.Contains(me.Suggestion, "meegle --refresh mywork todo") {
		t.Fatalf("expected refresh suggestion, got %q", me.Suggestion)
	}
}

func TestMcpExecutorStep_SkipsGroupNode(t *testing.T) {
	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"work-item"},
			Node:     &registry.CommandNode{HandlerRef: ""},
		},
		OutputConfig: map[string]any{},
	}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("expected no error for group node, got: %v", err)
	}
}

// Regression: --dry-run must short-circuit McpExecutorStep so the backend is
// not called. The previous behavior silently ignored --dry-run, which made
// debugging --params/--set hard because the user had no way to preview the
// payload without a real round-trip.
func TestMcpExecutorStep_DryRunSkipsBackend(t *testing.T) {
	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "update"},
			Node: &registry.CommandNode{
				HandlerRef: "update_workitem_fields",
			},
			Flags: map[string]any{
				"dry-run":      true,
				"work-item-id": "7110160001",
				"project-key":  "p-1",
			},
			ExplicitFlags: map[string]any{
				"dry-run":      true,
				"work-item-id": "7110160001",
				"project-key":  "p-1",
			},
		},
		// Note: no OutputConfig with mcp.host/token — dry-run must work without auth.
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("dry-run execute: %v", err)
	}
	if state.Result == nil || state.Result.Data == nil {
		t.Fatal("expected dry-run to set Result.Data")
	}
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", state.Result.Data)
	}
	if data["tool"] != "update_workitem_fields" {
		t.Errorf("tool = %v, want update_workitem_fields", data["tool"])
	}
	params, ok := data["params"].(map[string]any)
	if !ok {
		t.Fatalf("params not a map: %#v", data["params"])
	}
	// Keys must already be snake_case (matches what the backend would receive).
	if params["work_item_id"] != "7110160001" {
		t.Errorf("params[work_item_id] = %v, want 7110160001", params["work_item_id"])
	}
	if params["project_key"] != "p-1" {
		t.Errorf("params[project_key] = %v, want p-1", params["project_key"])
	}
	if data["dry_run"] != true {
		t.Errorf("dry_run flag missing from result: %#v", data)
	}
}

// Dry-run must surface unknown top-level params as a `validation.unknown_params`
// list so users can spot ignored arguments without doing a live round-trip.
// Drives the user-facing case where `--params '{"name":"..."}'` looks correct
// in the dry-run output but is silently dropped by the backend.
func TestMcpExecutorStep_DryRunIncludesUnknownParamsValidation(t *testing.T) {
	step := &McpExecutorStep{}
	tagsJSON := `{"work-item-id":"string","project-key":"string"}`
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "update"},
			Node: &registry.CommandNode{
				HandlerRef: "update_workitem_fields",
				Meta:       registry.NodeMeta{Tags: map[string]string{"mcp_param_types": tagsJSON}},
			},
			Flags: map[string]any{
				"dry-run":      true,
				"work-item-id": "1",
				"project-key":  "p-1",
				"name":         "stray",
			},
			ExplicitFlags: map[string]any{
				"dry-run":      true,
				"work-item-id": "1",
				"project-key":  "p-1",
				"name":         "stray",
			},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("dry-run execute: %v", err)
	}
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", state.Result.Data)
	}
	validation, ok := data["validation"].(map[string]any)
	if !ok {
		t.Fatalf("validation not a map: %#v", data["validation"])
	}
	unknown, ok := validation["unknown_params"].([]string)
	if !ok {
		t.Fatalf("unknown_params not a []string: %#v", validation["unknown_params"])
	}
	if len(unknown) != 1 || unknown[0] != "name" {
		t.Errorf("unknown_params = %v, want [name]", unknown)
	}
}

// All-valid dry-run must NOT add a validation field — preserves backward
// compatibility for callers that parse dry-run JSON output.
func TestMcpExecutorStep_DryRunNoValidationWhenAllParamsValid(t *testing.T) {
	step := &McpExecutorStep{}
	tagsJSON := `{"work-item-id":"string","project-key":"string"}`
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "update"},
			Node: &registry.CommandNode{
				HandlerRef: "update_workitem_fields",
				Meta:       registry.NodeMeta{Tags: map[string]string{"mcp_param_types": tagsJSON}},
			},
			Flags: map[string]any{
				"dry-run":      true,
				"work-item-id": "1",
				"project-key":  "p-1",
			},
			ExplicitFlags: map[string]any{
				"dry-run":      true,
				"work-item-id": "1",
				"project-key":  "p-1",
			},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("dry-run execute: %v", err)
	}
	data := state.Result.Data.(map[string]any)
	if _, present := data["validation"]; present {
		t.Errorf("validation should be absent when no unknown params, got %#v", data["validation"])
	}
}

func TestMcpExecutorStep_DryRunSkipsMeegleRuntimeFlags(t *testing.T) {
	step := &McpExecutorStep{}
	tagsJSON := `{"action":"string","page-num":"number"}`
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"mywork", "todo"},
			Node: &registry.CommandNode{
				HandlerRef: "list_todo",
				Meta:       registry.NodeMeta{Tags: map[string]string{"mcp_param_types": tagsJSON}},
			},
			Flags: map[string]any{
				"dry-run":  true,
				"refresh":  true,
				"profile":  "default",
				"action":   "this_week",
				"page-num": "1",
			},
			ExplicitFlags: map[string]any{
				"dry-run":  true,
				"refresh":  true,
				"profile":  "default",
				"action":   "this_week",
				"page-num": "1",
			},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("dry-run execute: %v", err)
	}
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", state.Result.Data)
	}
	params, ok := data["params"].(map[string]any)
	if !ok {
		t.Fatalf("params not a map: %#v", data["params"])
	}
	if _, ok := params["refresh"]; ok {
		t.Fatalf("refresh should not be sent to backend: %#v", params)
	}
	if _, ok := params["profile"]; ok {
		t.Fatalf("profile should not be sent to backend: %#v", params)
	}
	if params["action"] != "this_week" || params["page_num"] != float64(1) {
		t.Fatalf("business params changed unexpectedly: %#v", params)
	}
	if _, present := data["validation"]; present {
		t.Fatalf("runtime flags should not appear as unknown params: %#v", data["validation"])
	}
}

// Regression: SessionStep must not require auth when --dry-run is set, so
// dry-run works offline (no token) and surfaces errors locally. setupTestDir
// isolates the config dir to a fresh tempdir so the user's real profile
// cannot mask the assertion.
func TestSessionStep_DryRunSkipsAuth(t *testing.T) {
	setupTestDir(t)
	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "update"},
			Flags:    map[string]any{"dry-run": true},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("dry-run session step should not require auth: %v", err)
	}
}

// Regression: pipeline factory must keep the dry-run support visible by
// including SessionStep before McpExecutorStep — both must honor --dry-run.
func TestNewPipelineFactory_DryRunSupportedEndToEnd(t *testing.T) {
	factory := newPipelineFactory(NewDynamicRegistrySetup(nil, nil))
	pipe, err := factory(cliapp.Config{})
	if err != nil {
		t.Fatalf("pipeline factory: %v", err)
	}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "update"},
			Node: &registry.CommandNode{
				HandlerRef: "update_workitem_fields",
				Flags: []registry.FlagDef{
					{Name: "work-item-id", Required: true, Type: "string"},
				},
			},
			Flags: map[string]any{
				"dry-run":      true,
				"work-item-id": "7110160001",
			},
			ExplicitFlags: map[string]any{
				"dry-run":      true,
				"work-item-id": "7110160001",
			},
		},
	}
	if err := pipe.Execute(context.Background(), state); err != nil {
		t.Fatalf("pipeline execute (dry-run): %v", err)
	}
	if state.Result == nil || state.Result.Data == nil {
		t.Fatal("expected dry-run pipeline to populate Result.Data")
	}
}

// MCP schemas and README examples use snake_case while generated CLI flags use
// kebab-case. Required values supplied through --params must normalize before
// MeegleValidateStep checks them.
func TestNewPipelineFactoryParamsSatisfyRequiredSnakeCaseFlags(t *testing.T) {
	factory := newPipelineFactory(NewDynamicRegistrySetup(nil, nil))
	pipe, err := factory(cliapp.Config{})
	if err != nil {
		t.Fatalf("pipeline factory: %v", err)
	}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "create"},
			Node: &registry.CommandNode{
				HandlerRef: "create_workitem",
				Flags: []registry.FlagDef{
					{Name: "work-item-type", Required: true, Type: registry.FlagTypeString},
					{Name: "project-key", Required: true, Type: registry.FlagTypeString},
				},
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"work-item-type":"string","project-key":"string"}`,
				}},
			},
			Flags: map[string]any{
				"params":  `{"work_item_type":"testcase","project_key":"DEMO"}`,
				"dry-run": true,
			},
			ExplicitFlags: map[string]any{
				"params":  `{"work_item_type":"testcase","project_key":"DEMO"}`,
				"dry-run": true,
			},
		},
	}
	if err := pipe.Execute(context.Background(), state); err != nil {
		t.Fatalf("pipeline execute: %v", err)
	}
	data := state.Result.Data.(map[string]any)
	params := data["params"].(map[string]any)
	if params["work_item_type"] != "testcase" || params["project_key"] != "DEMO" {
		t.Fatalf("params = %#v", params)
	}
}

// batchNDJSONHook reshapes batch payloads only when --format=ndjson AND the
// node carries TagMcpBatch. Non-batch payloads (even if they happen to carry
// {results, errors} keys) must pass through unchanged.
func TestBatchNDJSONHook_OnlyBatchNodesReshape(t *testing.T) {
	hook := batchNDJSONHook{}
	plainNode := &registry.CommandNode{Name: "list"}
	ctx := &frameworkoutput.Context{
		Options: &frameworkoutput.FormatOptions{Mode: "ndjson"},
		Parsed:  &router.ParsedCommand{Node: plainNode},
	}
	data := map[string]any{
		"results": []any{map[string]any{"id": 1}},
		"errors":  []any{},
	}
	out, err := hook.Process(ctx, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isMap := out.(map[string]any); !isMap {
		t.Errorf("expected non-batch payload to pass through as map, got %T", out)
	}
}

func TestBatchNDJSONHook_BatchNodeFlattens(t *testing.T) {
	hook := batchNDJSONHook{}
	batchNode := &registry.CommandNode{
		Name: "+batch-get",
		Meta: registry.NodeMeta{Tags: map[string]string{TagMcpBatch: "1"}},
	}
	ctx := &frameworkoutput.Context{
		Options: &frameworkoutput.FormatOptions{Mode: "ndjson"},
		Parsed:  &router.ParsedCommand{Node: batchNode},
	}
	data := map[string]any{
		"results": []map[string]any{{"work_item_id": int64(1)}},
		"errors":  []map[string]any{},
		"summary": map[string]any{"total": 1, "succeeded": 1, "failed": 0},
	}
	out, err := hook.Process(ctx, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok {
		t.Fatalf("expected flattened []any, got %T", out)
	}
	if len(arr) != 2 { // 1 result + summary
		t.Fatalf("expected 2 records (result + summary), got %d: %#v", len(arr), arr)
	}
}

func TestBatchNDJSONHook_NonNDJSONPassthrough(t *testing.T) {
	hook := batchNDJSONHook{}
	batchNode := &registry.CommandNode{
		Meta: registry.NodeMeta{Tags: map[string]string{TagMcpBatch: "1"}},
	}
	ctx := &frameworkoutput.Context{
		Options: &frameworkoutput.FormatOptions{Mode: "json"},
		Parsed:  &router.ParsedCommand{Node: batchNode},
	}
	data := map[string]any{"results": []any{}, "errors": []any{}}
	out, _ := hook.Process(ctx, data)
	if _, isMap := out.(map[string]any); !isMap {
		t.Errorf("expected json mode to pass through, got %T", out)
	}
}

// --- coerceArray behavior for --fields / object-shaped array flags -------

func TestCoerceArray_SingleJSONObjectWrapsInArray(t *testing.T) {
	got := coerceArray([]string{`{"field_key":"name","field_value":"T"}`}, "object")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 1 {
		t.Fatalf("len = %d, want 1 (single JSON object must be wrapped): %#v", len(arr), arr)
	}
	entry, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("entry type = %T, want map", arr[0])
	}
	if entry["field_key"] != "name" || entry["field_value"] != "T" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestCoerceArray_SingleJSONArrayUsedAsIs(t *testing.T) {
	got := coerceArray([]string{`[{"field_key":"name","field_value":"A"},{"field_key":"priority","field_value":"P1"}]`}, "object")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 2 {
		t.Fatalf("len = %d, want 2", len(arr))
	}
	first, _ := arr[0].(map[string]any)
	if first["field_key"] != "name" {
		t.Fatalf("first entry = %#v", first)
	}
}

func TestCoerceArray_MultipleJSONObjectsDecoded(t *testing.T) {
	got := coerceArray([]string{
		`{"field_key":"name","field_value":"A"}`,
		`{"field_key":"priority","field_value":"P1"}`,
	}, "object")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 2 {
		t.Fatalf("len = %d, want 2", len(arr))
	}
	for i, want := range []string{"name", "priority"} {
		m, ok := arr[i].(map[string]any)
		if !ok {
			t.Fatalf("element %d not decoded: %#v", i, arr[i])
		}
		if m["field_key"] != want {
			t.Fatalf("element %d field_key = %#v, want %q", i, m["field_key"], want)
		}
	}
}

func TestCoerceArray_MultiplePlainStringsKeptAsStrings(t *testing.T) {
	got := coerceArray([]string{"name", "status"}, "string")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 2 || arr[0] != "name" || arr[1] != "status" {
		t.Fatalf("arr = %#v, want [name status]", arr)
	}
}

func TestCoerceArray_MultipleNumericItemsParsed(t *testing.T) {
	got := coerceArray([]string{"1", "2", "3"}, "number")
	arr, ok := got.([]float64)
	if !ok {
		t.Fatalf("type = %T, want []float64", got)
	}
	if len(arr) != 3 || arr[0] != 1 || arr[2] != 3 {
		t.Fatalf("arr = %#v", arr)
	}
}

func TestCoerceArray_SingleStringFallsBackToCSVSplit(t *testing.T) {
	got := coerceArray([]string{"name,status"}, "string")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 2 || arr[0] != "name" || arr[1] != "status" {
		t.Fatalf("arr = %#v", arr)
	}
}

func TestCoerceArray_BrokenJSONKeptAsString(t *testing.T) {
	// An element starting with { or [ that doesn't parse stays verbatim so
	// the downstream backend can surface the error rather than us hiding it.
	got := coerceArray([]string{`{not json`, "ok"}, "object")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 2 || arr[0] != `{not json` || arr[1] != "ok" {
		t.Fatalf("arr = %#v", arr)
	}
}

// --- coerceValue object-type handling (issue#14) -------------------------

func TestCoerceValue_ObjectJSONStringDecoded(t *testing.T) {
	got := coerceValue(`{"start_date":"2026-05-19","end_date":"2026-05-20"}`, "object", "")
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("type = %T, want map[string]any (got %#v)", got, got)
	}
	if m["start_date"] != "2026-05-19" || m["end_date"] != "2026-05-20" {
		t.Fatalf("decoded map = %#v", m)
	}
}

func TestCoerceValue_ObjectJSONArrayDecoded(t *testing.T) {
	got := coerceValue(`[1,2,3]`, "object", "")
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("type = %T, want []any", got)
	}
	if len(arr) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(arr), arr)
	}
}

func TestCoerceValue_ObjectBrokenJSONKeptAsString(t *testing.T) {
	got := coerceValue(`{not json`, "object", "")
	if got != `{not json` {
		t.Fatalf("got = %#v, want raw string", got)
	}
}

func TestCoerceValue_ObjectEmptyStringPassesThrough(t *testing.T) {
	got := coerceValue("", "object", "")
	if got != "" {
		t.Fatalf("got = %#v, want empty string", got)
	}
}

func TestCoerceValue_ObjectNonStringPassesThrough(t *testing.T) {
	in := map[string]any{"already": "object"}
	got := coerceValue(in, "object", "")
	m, ok := got.(map[string]any)
	if !ok || m["already"] != "object" {
		t.Fatalf("got = %#v", got)
	}
}

func TestNewPipelineFactory(t *testing.T) {
	factory := newPipelineFactory(NewDynamicRegistrySetup(nil, nil))
	pipe, err := factory(cliapp.Config{})
	if err != nil {
		t.Fatalf("unexpected error from pipeline factory: %v", err)
	}
	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}
	expectedNames := []string{
		"param_merge",
		"structured_flag_name_normalize", "meegle_validate", "session", "mcp_executor", "auto_paginate", "batch_executor", "attachment_shortcut", "output",
	}
	if len(pipe.Steps) != len(expectedNames) {
		t.Fatalf("expected %d steps, got %d", len(expectedNames), len(pipe.Steps))
	}
	for i, name := range expectedNames {
		if pipe.Steps[i].Name() != name {
			t.Errorf("step %d: expected name %q, got %q", i, name, pipe.Steps[i].Name())
		}
	}
}

// End-to-end: when the CLI is configured with user_agent (via config or env),
// the real tool-call HTTP request must carry the assembled User-Agent header
// "meegle-cli[/version] <caller>". SessionStep is responsible for writing the
// assembled UA into OutputConfig; newMcpClientFromState reads it.
func TestMcpExecutorStep_AppliesUserAgent(t *testing.T) {
	var capturedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
			"mcp.user_agent": mcpclient.BuildUserAgent("my-svc/1.0"),
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	wantSuffix := " my-svc/1.0"
	if !strings.HasPrefix(capturedUA, "meegle-cli") || !strings.HasSuffix(capturedUA, wantSuffix) {
		t.Errorf("expected User-Agent like 'meegle-cli[/ver] my-svc/1.0', got %q", capturedUA)
	}
}

// When no user_agent is configured, the default "meegle-cli[/version]" is sent.
func TestMcpExecutorStep_DefaultUserAgentWhenUnset(t *testing.T) {
	var capturedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
			// no "mcp.user_agent" entry → default UA
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	if !strings.HasPrefix(capturedUA, "meegle-cli") {
		t.Errorf("expected default User-Agent starting with meegle-cli, got %q", capturedUA)
	}
	if strings.Contains(capturedUA, " my-svc") {
		t.Errorf("did not expect a caller suffix, got %q", capturedUA)
	}
}

// When the backend returns a "log_id: <id>" content entry, McpExecutorStep
// must surface it under Result.Metadata["logid"] so the EnvelopeHook can
// expose it via --envelope. Without this, the id is silently dropped and
// oncall has no way to trace a specific call back to argos. The production
// server uses "log_id:" (with underscore); see mcpclient.logIDPrefixes for
// the historical "logid:" alias.
func TestMcpExecutorStep_PropagatesLogIDToMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "log_id: 20260519145706D6A0F7B0A114DB405B66"},
					{"type": "text", "text": `{"ok":true}`},
				},
			},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	if state.Result == nil || state.Result.Metadata == nil {
		t.Fatalf("expected Result with Metadata, got %+v", state.Result)
	}
	got, _ := state.Result.Metadata["logid"].(string)
	if got != "20260519145706D6A0F7B0A114DB405B66" {
		t.Errorf("expected logid metadata stripped of prefix, got %q", got)
	}
}

// When the backend response carries no logid entry, Metadata must not
// contain an empty logid key — otherwise --envelope output would show
// {"logid":""} and mislead users into thinking they have a trace id.
func TestMcpExecutorStep_OmitsLogIDWhenAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body.ID,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": `{"ok":true}`},
				},
			},
		})
	}))
	defer server.Close()

	step := &McpExecutorStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Node:     &registry.CommandNode{HandlerRef: "get_work_item"},
			Flags:    map[string]any{},
		},
		Values: map[string]any{},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("executor step failed: %v", err)
	}
	if state.Result == nil {
		t.Fatalf("expected Result, got nil")
	}
	if _, ok := state.Result.Metadata["logid"]; ok {
		t.Errorf("expected no logid key in metadata when backend omitted it, got %+v", state.Result.Metadata)
	}
}

func TestExtractLogID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc123", "abc123"}, // bare id — what mcpclient now passes us
		{"log_id: abc123", "abc123"},
		{"log_id:abc123", "abc123"},
		{"  log_id:   abc123  ", "abc123"},
		{"logid: abc123", "abc123"}, // historical alias
		{"logid:abc123", "abc123"},
	}
	for _, c := range cases {
		if got := extractLogID(c.in); got != c.want {
			t.Errorf("extractLogID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SessionStep must translate ident.UserAgentCaller into the pre-built
// "mcp.user_agent" key that newMcpClientFromState consumes. This pins the
// startup → runtime handoff.
func TestSessionStep_WritesUserAgentToOutputConfig(t *testing.T) {
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "env.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok_env")
	t.Setenv("MEEGLE_USER_AGENT", "ci-runner")

	step := &SessionStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
		OutputConfig: map[string]any{},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step failed: %v", err)
	}
	ua, _ := state.OutputConfig["mcp.user_agent"].(string)
	wantSuffix := " ci-runner"
	if !strings.HasPrefix(ua, "meegle-cli") || !strings.HasSuffix(ua, wantSuffix) {
		t.Errorf("expected mcp.user_agent to end with ' ci-runner', got %q", ua)
	}
}

// --- AutoPaginateStep tests ---

// helper: build a minimal MCP tools/call response with a JSON payload
func mcpJSONResponse(t *testing.T, payload any) map[string]any {
	t.Helper()
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(jsonBytes)},
			},
		},
	}
}

// AutoPaginateStep must be a no-op when --auto-paginate is not set.
func TestAutoPaginateStep_NoOpWithoutFlag(t *testing.T) {
	step := &AutoPaginateStep{}
	originalData := map[string]any{"list": []any{"a"}}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{}, // no auto-paginate
		},
		Result: &executor.RawResult{Data: originalData},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", state.Result.Data)
	}
	list, _ := data["list"].([]any)
	if len(list) != 1 || list[0] != "a" {
		t.Errorf("data should be unchanged, got %#v", data)
	}
}

// AutoPaginateStep must be a no-op when the result has no pagination signals.
func TestAutoPaginateStep_NoOpWithoutPaginationSignals(t *testing.T) {
	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "get_workitem_brief"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data:     map[string]any{"name": "demo-item", "id": float64(1)},
			Metadata: map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := state.Result.Data.(map[string]any)
	if data["name"] != "demo-item" {
		t.Errorf("data should be unchanged, got %#v", data)
	}
	if _, hasMeta := state.Result.Metadata["auto_paginated"]; hasMeta {
		t.Errorf("should not set auto_paginated metadata when no pagination")
	}
}

// page_token-based pagination: first page has next_page_token, second page
// has none. The step should merge the list arrays and delete next_page_token.
func TestAutoPaginateStep_PageTokenMergesPages(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body struct {
			Params struct {
				Arguments struct {
					PageToken string `json:"page_token"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		var payload map[string]any
		if body.Params.Arguments.PageToken == "" {
			// First page
			payload = map[string]any{
				"list":            []any{"item1", "item2"},
				"next_page_token": "token-page-2",
			}
		} else {
			// Second page (final)
			payload = map[string]any{
				"list":            []any{"item3"},
				"next_page_token": "",
			}
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item1", "item2"},
				"next_page_token": "token-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 follow-up call, got %d", callCount)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 3 {
		t.Fatalf("expected 3 merged items, got %d: %#v", len(list), list)
	}
	if list[0] != "item1" || list[2] != "item3" {
		t.Errorf("merged list = %#v", list)
	}
	if _, exists := data["next_page_token"]; exists {
		t.Errorf("next_page_token should be deleted after merge")
	}

	meta := state.Result.Metadata
	if meta["auto_paginated"] != true {
		t.Errorf("expected auto_paginated=true, got %v", meta["auto_paginated"])
	}
	if meta["pages_merged"] != 2 {
		t.Errorf("expected pages_merged=2, got %v", meta["pages_merged"])
	}
	if meta["total_items"] != 3 {
		t.Errorf("expected total_items=3, got %v", meta["total_items"])
	}
	if _, truncated := meta["truncated"]; truncated {
		t.Errorf("should not be truncated for 2 pages")
	}
}

// page_num-based pagination: first page has pagination.has_more=true,
// second page has has_more=false. The step should merge and remove
// the pagination wrapper.
func TestAutoPaginateStep_PageNumMergesPages(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body struct {
			Params struct {
				Arguments struct {
					PageNum int `json:"page_num"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		var payload map[string]any
		if body.Params.Arguments.PageNum <= 1 {
			payload = map[string]any{
				"list": []any{"a", "b"},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(4),
				},
			}
		} else {
			payload = map[string]any{
				"list": []any{"c", "d"},
				"pagination": map[string]any{
					"has_more": false,
					"total":    float64(4),
				},
			}
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "list_todo",
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"page-num":"integer"}`,
				}},
			},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list": []any{"a", "b"},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(4),
				},
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 follow-up call, got %d", callCount)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 4 {
		t.Fatalf("expected 4 merged items, got %d: %#v", len(list), list)
	}
	if _, exists := data["pagination"]; exists {
		t.Errorf("pagination wrapper should be removed after merge")
	}
	gotTotal := toInt64(data["total"])
	if gotTotal != 4 {
		t.Errorf("expected total=4 at top level, got %v", data["total"])
	}

	meta := state.Result.Metadata
	if meta["auto_paginated"] != true {
		t.Errorf("expected auto_paginated=true, got %v", meta["auto_paginated"])
	}
	if meta["pages_merged"] != 2 {
		t.Errorf("expected pages_merged=2, got %v", meta["pages_merged"])
	}
}

// mergePayloads: arrays concatenate, maps shallow-merge, scalars overwrite.
func TestMergePayloads(t *testing.T) {
	base := map[string]any{
		"list":     []any{1, 2},
		"name":     "base",
		"metadata": map[string]any{"a": "1", "b": "2"},
	}
	next := map[string]any{
		"list":     []any{3, 4},
		"name":     "next",
		"metadata": map[string]any{"b": "3", "c": "4"},
	}
	merged := mergePayloads(base, next)

	list, _ := merged["list"].([]any)
	if len(list) != 4 || list[0] != 1 || list[3] != 4 {
		t.Errorf("list = %#v", list)
	}
	if merged["name"] != "next" {
		t.Errorf("name should be overwritten to 'next', got %v", merged["name"])
	}
	meta, _ := merged["metadata"].(map[string]any)
	if meta["a"] != "1" || meta["b"] != "3" || meta["c"] != "4" {
		t.Errorf("metadata not merged correctly: %#v", meta)
	}
}

// page_token-based pagination where the backend keeps returning a non-empty
// token but every page is empty. The step must stop after maxEmptyPageStreak
// consecutive empty pages instead of looping to the 200-page cap.
func TestAutoPaginateStep_PageTokenStopsOnEmptyStreak(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Every follow-up page returns an empty list but a non-empty token.
		payload := map[string]any{
			"list":            []any{},
			"next_page_token": "stuck-token",
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item1"},
				"next_page_token": "token-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	// Should stop after exactly maxEmptyPageStreak empty follow-up pages.
	if callCount != maxEmptyPageStreak {
		t.Errorf("expected %d follow-up calls (empty streak), got %d", maxEmptyPageStreak, callCount)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 1 {
		t.Errorf("expected original 1 item (no new items merged), got %d: %#v", len(list), list)
	}
	// The stuck token must be dropped so the caller isn't pointed at a dead continuation.
	if _, exists := data["next_page_token"]; exists {
		t.Errorf("next_page_token should be deleted after empty-streak stop")
	}

	meta := state.Result.Metadata
	if meta["auto_paginated"] != true {
		t.Errorf("expected auto_paginated=true, got %v", meta["auto_paginated"])
	}
	if _, truncated := meta["truncated"]; truncated {
		t.Errorf("should not be marked truncated on empty-streak stop (no valid continuation)")
	}
}

// page_num-based pagination where the backend keeps returning has_more=true
// but every follow-up page is empty. The step must stop after
// maxEmptyPageStreak consecutive empty pages.
func TestAutoPaginateStep_PageNumStopsOnEmptyStreak(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Every follow-up page returns an empty list but has_more=true.
		payload := map[string]any{
			"list": []any{},
			"pagination": map[string]any{
				"has_more": true,
				"total":    float64(100), // inflates total so the total-check doesn't break early
			},
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "list_todo",
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"page-num":"integer"}`,
				}},
			},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list": []any{"a"},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(100),
				},
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.host":       "ignored.example.com",
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	if callCount != maxEmptyPageStreak {
		t.Errorf("expected %d follow-up calls (empty streak), got %d", maxEmptyPageStreak, callCount)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 1 {
		t.Errorf("expected original 1 item (no new items merged), got %d: %#v", len(list), list)
	}
	meta := state.Result.Metadata
	if meta["stopped_reason"] != "consecutive_empty_pages" {
		t.Errorf("expected stopped_reason=consecutive_empty_pages, got %v", meta["stopped_reason"])
	}
}

// --- Additional AutoPaginateStep coverage ---

// First page already has no pagination signals (no next_page_token, no
// pagination.has_more). AutoPaginateStep should be a complete no-op — no
// follow-up calls, no metadata enrichment. This is the most common path
// (single-page responses) and must short-circuit cleanly.
func TestAutoPaginateStep_NoOpWhenFirstPageHasNoSignals(t *testing.T) {
	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "get_workitem_brief"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"only-item"},
				"next_page_token": "", // explicitly empty
			},
			Metadata: map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 1 || list[0] != "only-item" {
		t.Errorf("data should be unchanged, got %#v", data)
	}
	if _, hasMeta := state.Result.Metadata["auto_paginated"]; hasMeta {
		t.Errorf("should not set auto_paginated when no pagination signals")
	}
}

// --dry-run must prevent AutoPaginateStep from executing. In dry-run mode,
// McpExecutorStep leaves state.Result as nil, so AutoPaginateStep should
// short-circuit on the nil check and never make follow-up calls.
func TestAutoPaginateStep_DryRunDoesNotExecute(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, map[string]any{
			"list":            []any{"should-not-happen"},
			"next_page_token": "token-page-2",
		}))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true, "dry-run": true},
		},
		// In dry-run, McpExecutorStep returns early with state.Result == nil.
		Result: nil,
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("dry-run should not make any follow-up calls, got %d", callCount)
	}
}

// When the follow-up CallTool returns an error, AutoPaginateStep must
// propagate it to the caller rather than silently swallowing it.
func TestAutoPaginateStep_PageTokenPropagatesCallError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error": map[string]any{
				"code":    -32603,
				"message": "internal server error",
			},
		})
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item1"},
				"next_page_token": "token-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	err := step.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from failed follow-up call, got nil")
	}
}

// When a follow-up page returns a non-map response (e.g. a bare array or
// string), AutoPaginateStep must stop gracefully without panicking.
func TestAutoPaginateStep_PageTokenStopsOnNonMapResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a bare array instead of the expected map[string]any.
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, []any{"unexpected", "shape"}))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item1", "item2"},
				"next_page_token": "token-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should keep the original first-page data without merging the bad page.
	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 2 {
		t.Errorf("expected 2 items (original only), got %d: %#v", len(list), list)
	}
}

// When the result data itself is not a map (e.g. a bare array or scalar),
// AutoPaginateStep must be a no-op rather than panicking on a type assertion.
func TestAutoPaginateStep_NoOpOnNonMapResult(t *testing.T) {
	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:  &registry.CommandNode{HandlerRef: "search_by_mql"},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data:     []any{"bare", "array"},
			Metadata: map[string]any{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := state.Result.Data.([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("data should be unchanged bare array, got %#v", state.Result.Data)
	}
}

// When a sugar command has a ResultTransform, follow-up pages must also have
// the transform applied so merged data has a consistent shape. Without this,
// the first page is transformed but subsequent pages are raw, leading to
// mismatched field names in the merged list.
func TestAutoPaginateStep_TokenAppliesTransformToFollowUpPages(t *testing.T) {
	// Register a temporary transform for "testgroup/transformcmd" that
	// renames "raw_name" to "name" — simulating a sugar command transform.
	original := sugarResultTransforms["testgroup/transformcmd"]
	defer func() {
		if original != nil {
			sugarResultTransforms["testgroup/transformcmd"] = original
		} else {
			delete(sugarResultTransforms, "testgroup/transformcmd")
		}
	}()
	sugarResultTransforms["testgroup/transformcmd"] = func(data any) any {
		m, ok := data.(map[string]any)
		if !ok {
			return data
		}
		if list, ok := m["list"].([]any); ok {
			for i, item := range list {
				if itemMap, ok := item.(map[string]any); ok {
					if rawName, exists := itemMap["raw_name"]; exists {
						itemMap["name"] = rawName
						delete(itemMap, "raw_name")
					}
					list[i] = itemMap
				}
			}
			m["list"] = list
		}
		return m
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Follow-up page returns items with "raw_name" (pre-transform shape).
		payload := map[string]any{
			"list": []any{
				map[string]any{"raw_name": "item3-transformed"},
			},
			"next_page_token": "",
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	// First page data is already in post-transform shape (has "name", not "raw_name").
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:     &registry.CommandNode{HandlerRef: "search_by_mql"},
			FullPath: []string{"testgroup", "transformcmd"},
			Flags:    map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list": []any{
					map[string]any{"name": "item1-transformed"},
					map[string]any{"name": "item2-transformed"},
				},
				"next_page_token": "token-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 3 {
		t.Fatalf("expected 3 merged items, got %d: %#v", len(list), list)
	}
	// Every item must have "name" (post-transform), not "raw_name".
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item %d is not a map: %#v", i, item)
		}
		if _, hasRaw := m["raw_name"]; hasRaw {
			t.Errorf("item %d still has raw_name (transform not applied): %#v", i, m)
		}
		if m["name"] == nil {
			t.Errorf("item %d missing 'name' field: %#v", i, m)
		}
	}
}

// page_num-based pagination with a ResultTransform: the transform must not
// break pagination signal extraction. The transform runs on resp.Data before
// the code reads pageData["pagination"]["has_more"], so if a transform were to
// rename or delete the "pagination" field, the loop would terminate early.
// This test confirms that a transform touching only list items (not
// pagination) preserves the has_more signal across pages.
func TestAutoPaginateStep_PageNumAppliesTransformWithoutBreakingPagination(t *testing.T) {
	// Register a temporary transform for "testgroup/pagetransform" that
	// renames "raw_name" to "name" in list items but leaves pagination intact.
	original := sugarResultTransforms["testgroup/pagetransform"]
	defer func() {
		if original != nil {
			sugarResultTransforms["testgroup/pagetransform"] = original
		} else {
			delete(sugarResultTransforms, "testgroup/pagetransform")
		}
	}()
	sugarResultTransforms["testgroup/pagetransform"] = func(data any) any {
		m, ok := data.(map[string]any)
		if !ok {
			return data
		}
		if list, ok := m["list"].([]any); ok {
			for i, item := range list {
				if itemMap, ok := item.(map[string]any); ok {
					if rawName, exists := itemMap["raw_name"]; exists {
						itemMap["name"] = rawName
						delete(itemMap, "raw_name")
					}
					list[i] = itemMap
				}
			}
			m["list"] = list
		}
		return m
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body struct {
			Params struct {
				Arguments struct {
					PageNum int `json:"page_num"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		var payload map[string]any
		if body.Params.Arguments.PageNum <= 1 {
			// First page (called by McpExecutorStep, not by auto-paginate).
			payload = map[string]any{
				"list": []any{
					map[string]any{"raw_name": "a-raw"},
					map[string]any{"raw_name": "b-raw"},
				},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(4),
				},
			}
		} else {
			// Second page (follow-up by auto-paginate).
			payload = map[string]any{
				"list": []any{
					map[string]any{"raw_name": "c-raw"},
					map[string]any{"raw_name": "d-raw"},
				},
				"pagination": map[string]any{
					"has_more": false,
					"total":    float64(4),
				},
			}
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	// First page data is already in post-transform shape (has "name").
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "list_todo",
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"page-num":"integer"}`,
				}},
			},
			FullPath: []string{"testgroup", "pagetransform"},
			Flags:    map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list": []any{
					map[string]any{"name": "a-transformed"},
					map[string]any{"name": "b-transformed"},
				},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(4),
				},
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	// Should have made exactly 1 follow-up call (page 2).
	if callCount != 1 {
		t.Errorf("expected 1 follow-up call, got %d", callCount)
	}

	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 4 {
		t.Fatalf("expected 4 merged items, got %d: %#v", len(list), list)
	}
	// Every item must have "name" (post-transform), not "raw_name".
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item %d is not a map: %#v", i, item)
		}
		if _, hasRaw := m["raw_name"]; hasRaw {
			t.Errorf("item %d still has raw_name (transform not applied): %#v", i, m)
		}
		if m["name"] == nil {
			t.Errorf("item %d missing 'name' field: %#v", i, m)
		}
	}
}

// When the command declares "next_page_token" (not "page_token") in its
// mcp_param_types, the follow-up call must send the cursor under
// "next_page_token" — the parameter name the backend actually accepts.
// Sending "page_token" instead would be silently ignored, causing the backend
// to return page 1 repeatedly and producing 200 pages of duplicate data.
func TestAutoPaginateStep_TokenUsesNextPageTokenParamName(t *testing.T) {
	callCount := 0
	var receivedParamName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Capture which token parameter name the follow-up request used.
		if callCount == 1 {
			if _, ok := body.Params.Arguments["next_page_token"]; ok {
				receivedParamName = "next_page_token"
			} else if _, ok := body.Params.Arguments["page_token"]; ok {
				receivedParamName = "page_token"
			}
		}

		payload := map[string]any{
			"list":            []any{"item-next"},
			"next_page_token": "",
		}
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, payload))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "get_workitem_op_records",
				Meta: registry.NodeMeta{Tags: map[string]string{
					// Command declares next_page_token, NOT page_token.
					"mcp_param_types": `{"next-page-token":"string"}`,
				}},
			},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item-first"},
				"next_page_token": "cursor-page-2",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 follow-up call, got %d", callCount)
	}
	if receivedParamName != "next_page_token" {
		t.Errorf("expected follow-up to send next_page_token, got %q", receivedParamName)
	}
}

// When the command does NOT declare page_num in mcp_param_types, the
// page_num-based pagination path must not activate — even if the response
// contains pagination.has_more=true. This prevents blindly sending page_num
// to commands that use offset/cursor/group-pagination-list instead.
func TestAutoPaginateStep_PageNumSkippedWhenParamNotDeclared(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, map[string]any{
			"list": []any{"should-not-reach"},
			"pagination": map[string]any{
				"has_more": true,
				"total":    float64(100),
			},
		}))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "list_offset_based",
				// Command declares offset, NOT page_num.
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"offset":"integer","limit":"integer"}`,
				}},
			},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list": []any{"a", "b"},
				"pagination": map[string]any{
					"has_more": true,
					"total":    float64(100),
				},
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("auto-paginate step failed: %v", err)
	}

	// No follow-up calls — page_num not declared, token path not triggered.
	if callCount != 0 {
		t.Errorf("expected 0 follow-up calls (page_num not declared), got %d", callCount)
	}
	// Data should be unchanged.
	data := state.Result.Data.(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) != 2 {
		t.Errorf("expected original 2 items, got %d: %#v", len(list), list)
	}
	// Should NOT set auto_paginated metadata.
	if _, hasMeta := state.Result.Metadata["auto_paginated"]; hasMeta {
		t.Errorf("should not set auto_paginated when page_num not declared")
	}
}

// When the 200-page cap is hit and the command declares next_page_token
// (not page_token), the truncation stderr hint must tell the user to re-run
// with --next-page-token (the actual CLI flag), not --page-token. Otherwise
// the generated continuation command references a flag that does not exist
// for this command.
func TestAutoPaginateStep_TokenTruncationHintUsesCorrectFlagName(t *testing.T) {
	// Server that always returns a non-empty next_page_token so we hit the
	// 200-page cap. Each page returns one unique item so the empty-page guard
	// never triggers.
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, map[string]any{
			"list":            []any{fmt.Sprintf("item-%d", page)},
			"next_page_token": fmt.Sprintf("token-%d", page+1),
		}))
	}))
	defer server.Close()

	step := &AutoPaginateStep{}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node: &registry.CommandNode{
				HandlerRef: "get_workitem_op_records",
				Meta: registry.NodeMeta{Tags: map[string]string{
					"mcp_param_types": `{"next-page-token":"string"}`,
				}},
			},
			Flags: map[string]any{"auto-paginate": true},
		},
		Result: &executor.RawResult{
			Data: map[string]any{
				"list":            []any{"item-0"},
				"next_page_token": "token-1",
			},
			Metadata: map[string]any{},
		},
		OutputConfig: map[string]any{
			"mcp.server_url": server.URL,
			"mcp.token":      "tok",
			"mcp.headers":    map[string]string{},
		},
	}
	// Capture stderr to verify the hint uses the correct flag name.
	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	executeErr := step.Execute(context.Background(), state)

	_ = wPipe.Close()
	os.Stderr = oldStderr
	stderrBytes, _ := io.ReadAll(rPipe)
	_ = rPipe.Close()
	stderrOutput := string(stderrBytes)

	if executeErr != nil {
		t.Fatalf("auto-paginate step failed: %v", executeErr)
	}

	meta := state.Result.Metadata
	if meta["truncated"] != true {
		t.Fatalf("expected truncated=true, got %v", meta["truncated"])
	}
	if meta["pages_merged"].(int) != maxAutoPaginatePages {
		t.Errorf("expected pages_merged=%d, got %v", maxAutoPaginatePages, meta["pages_merged"])
	}
	// The continuation token must be present.
	if meta["next_page_token"] == nil || meta["next_page_token"] == "" {
		t.Errorf("expected non-empty next_page_token in meta, got %v", meta["next_page_token"])
	}

	// The stderr hint must reference --next-page-token (the actual CLI flag
	// for this command), not --page-token. Without this assertion the test
	// would pass even if the hint regressed to the hardcoded wrong flag.
	if !strings.Contains(stderrOutput, "--next-page-token") {
		t.Errorf("stderr hint should contain --next-page-token, got:\n%s", stderrOutput)
	}
	if strings.Contains(stderrOutput, "--page-token ") {
		t.Errorf("stderr hint should NOT contain --page-token (wrong flag for this command), got:\n%s", stderrOutput)
	}
}
