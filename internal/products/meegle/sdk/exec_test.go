// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"
	"strings"
	"testing"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// stubRegistrySetup returns a fixed command tree for testing.
type stubRegistrySetup struct{}

func (s *stubRegistrySetup) Setup(ctx context.Context) (*registry.CommandTree, error) {
	tools := []types.ToolDefinition{{
		Name:        "get_workitem_brief",
		Description: "Get work item brief",
		Parameters: []types.ToolParameter{
			{Name: "work_item_id", Type: "number", Required: true, Description: "Work item ID"},
		},
	}}
	commands := meegle.ExportMapToolsForTest(tools)
	tree := meegle.ExportBuildCommandTreeForTest(commands)
	return tree, nil
}

// newTestCommandClient creates a CommandClient with a stub registry for testing.
func newTestCommandClient() (*CommandClient, error) {
	cfg := &ClientConfig{Host: "example.com", Token: "test"}
	placeholderExec := executor.Func(func(_ context.Context, _ *executor.Request) (*executor.RawResult, error) {
		return nil, nil
	})
	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithSetup(&stubRegistrySetup{}),
		cliapp.WithExecutor(placeholderExec),
		cliapp.WithPipelineFactory(newSDKPipelineFactory(cfg, nil)),
	)
	if err != nil {
		return nil, err
	}
	return &CommandClient{app: app}, nil
}

func TestNewCommandClient_DiscoveryFailure(t *testing.T) {
	_, err := NewCommandClient("example.com", WithToken("test"))
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
	if !strings.Contains(err.Error(), "tool discovery failed") {
		t.Fatalf("expected 'tool discovery failed' in error, got: %v", err)
	}
}

func TestNewCommandClient_EmptyHost(t *testing.T) {
	_, err := NewCommandClient("")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestNewCommandClient_TokenAndAuthHeaderConflict(t *testing.T) {
	_, err := NewCommandClient("example.com",
		WithToken("my-token"),
		WithHeaders(map[string]string{"Authorization": "Bearer other"}),
	)
	if err == nil {
		t.Fatal("expected error for conflicting token and Authorization header")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

func TestExecuteCommand_HelpCommand(t *testing.T) {
	c, err := newTestCommandClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, err := c.Execute(context.Background(), "meegle --help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty help output")
	}
}

func TestExecuteCommand_EmptyCommand(t *testing.T) {
	_, err := ExecuteCommand(context.Background(), "example.com", "", WithToken("test"))
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestClientConfig_ServerURL(t *testing.T) {
	cfg := &ClientConfig{Host: "meegle.com"}
	if cfg.serverURL() != "https://meegle.com/mcp_server/v1" {
		t.Errorf("unexpected URL: %s", cfg.serverURL())
	}
}

func TestClientConfig_ServerURLWithQuery(t *testing.T) {
	cfg := &ClientConfig{
		Host:  "staging.example.com",
		Query: map[string]string{"env": "staging"},
	}
	u := cfg.serverURL()
	if !strings.Contains(u, "env=staging") {
		t.Errorf("expected query param in URL, got: %s", u)
	}
	if !strings.HasPrefix(u, "https://staging.example.com/mcp_server/v1?") {
		t.Errorf("unexpected URL prefix: %s", u)
	}
}

func TestClientConfig_ResolveToken(t *testing.T) {
	// From Token field
	cfg := &ClientConfig{Token: "direct-token"}
	if cfg.resolveToken() != "direct-token" {
		t.Errorf("expected direct-token, got %s", cfg.resolveToken())
	}

	// Fallback from Headers
	cfg = &ClientConfig{Headers: map[string]string{"Authorization": "Bearer header-token"}}
	if cfg.resolveToken() != "header-token" {
		t.Errorf("expected header-token, got %s", cfg.resolveToken())
	}

	// Token field takes priority (validation prevents both, but test resolveToken logic)
	cfg = &ClientConfig{Token: "direct", Headers: map[string]string{"Authorization": "Bearer fallback"}}
	if cfg.resolveToken() != "direct" {
		t.Errorf("expected direct, got %s", cfg.resolveToken())
	}
}

func TestClientConfig_HttpHeaders_ExcludesAuth(t *testing.T) {
	cfg := &ClientConfig{Headers: map[string]string{
		"Authorization": "Bearer xxx",
		"X-Env":         "staging",
	}}
	h := cfg.httpHeaders()
	if h.Get("Authorization") != "" {
		t.Error("httpHeaders should exclude Authorization")
	}
	if h.Get("X-Env") != "staging" {
		t.Error("httpHeaders should include X-Env")
	}
}

func TestParseArgs_WithPrefix(t *testing.T) {
	args := parseArgs(nil, "meegle workitem get-brief --work-item-id 123")
	expected := []string{"workitem", "get-brief", "--work-item-id", "123"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg %d: expected %q, got %q", i, want, args[i])
		}
	}
}

func TestParseArgs_WithoutPrefix(t *testing.T) {
	args := parseArgs(nil, "workitem get-brief --work-item-id 123")
	expected := []string{"workitem", "get-brief", "--work-item-id", "123"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
}

func TestParseArgs_QuotedArgs(t *testing.T) {
	args := parseArgs(nil, `workitem create --name "my work item"`)
	expected := []string{"workitem", "create", "--name", "my work item"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg %d: expected %q, got %q", i, want, args[i])
		}
	}
}

func TestRejectShellOperators(t *testing.T) {
	tests := []struct {
		name        string
		cmdStr      string
		errContains string
	}{
		{"pipe", "cd a/b | meegle workitem", "pipes"},
		{"redirect", "meegle workitem get-brief --work-item-id 123 > ./output.json", "redirection"},
		{"append redirect", "meegle workitem get-brief >> ./output.json", "redirection"},
		{"semicolon", "meegle workitem list; meegle workitem get-brief", "chaining"},
		{"and", "meegle workitem list && meegle workitem get-brief", "conditional"},
		{"or", "meegle workitem list || echo fail", "conditional"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectShellOperators(tt.cmdStr)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.cmdStr)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestRejectShellOperators_ValidCommands(t *testing.T) {
	valid := []string{
		"meegle workitem get-brief --work-item-id 123",
		"meegle mywork todo --action done --page-num 1",
		`meegle workitem create --name "my work item"`,
		// Operators inside double-quoted values must not trigger the check.
		`meegle workitem query --mql "SELECT id FROM story WHERE created_at >= '2026-02-13'"`,
		`meegle workitem query --mql "a >= 1 AND b <= 2"`,
		`meegle workitem update --name "A && B || C"`,
		`meegle workitem create --desc "pipe | inside"`,
		`meegle workitem create --desc "semi ; inside"`,
	}
	for _, cmd := range valid {
		if err := rejectShellOperators(cmd); err != nil {
			t.Errorf("unexpected error for %q: %v", cmd, err)
		}
	}
}

func TestRejectShellOperators_OutsideQuotesStillRejected(t *testing.T) {
	// Operators that sit outside "..." must still be rejected even if the
	// command also contains a legitimate quoted value.
	tests := []struct {
		name        string
		cmdStr      string
		errContains string
	}{
		{
			"redirect after quoted flag",
			`meegle workitem query --mql "a >= 1" > out.json`,
			"redirection",
		},
		{
			"pipe between two quoted commands",
			`meegle workitem get --id "1" | meegle workitem get --id "2"`,
			"pipes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectShellOperators(tt.cmdStr)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.cmdStr)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
			}
		})
	}
}
