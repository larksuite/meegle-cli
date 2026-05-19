// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
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
			FullPath: []string{"work-item", "get-brief"},
			Flags:    map[string]any{"work_item_id": "123"},
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
			FullPath: []string{"work-item", "get-brief"},
			Flags:    map[string]any{"work_item_id": "456"},
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
		"meegle_validate", "session", "mcp_executor", "batch_executor", "attachment_shortcut", "output",
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
