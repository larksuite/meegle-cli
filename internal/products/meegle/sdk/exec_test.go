// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	extcredential "github.com/larksuite/meegle-cli/extension/credential"
	extplatform "github.com/larksuite/meegle-cli/extension/platform"
	exttransport "github.com/larksuite/meegle-cli/extension/transport"
	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type sdkIsolationCredentialProvider struct{ calls *atomic.Int32 }

func (p sdkIsolationCredentialProvider) Name() string { return "sdk-isolation" }
func (p sdkIsolationCredentialProvider) ResolveAccount(context.Context) (*extcredential.Account, error) {
	p.calls.Add(1)
	return nil, errors.New("SDK used CLI credential extension")
}
func (p sdkIsolationCredentialProvider) ResolveToken(context.Context, extcredential.TokenSpec) (*extcredential.Token, error) {
	p.calls.Add(1)
	return nil, errors.New("SDK used CLI credential extension")
}

type sdkIsolationTransportProvider struct{ calls *atomic.Int32 }

func (p sdkIsolationTransportProvider) Name() string { return "sdk-isolation" }
func (p sdkIsolationTransportProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	p.calls.Add(1)
	panic("SDK used CLI transport extension")
}

// stubRegistrySetup returns a fixed command tree for testing.
type stubRegistrySetup struct{}

func (s *stubRegistrySetup) Setup(ctx context.Context) (*registry.CommandTree, error) {
	tools := []types.ToolDefinition{
		{
			Name:        "get_workitem_brief",
			Description: "Get work item brief",
			Parameters: []types.ToolParameter{
				{Name: "work_item_id", Type: "number", Required: true, Description: "Work item ID"},
			},
		},
		{
			Name:        "get_transitable_states",
			Description: "List state transitions",
			Parameters: []types.ToolParameter{
				{Name: "user_key", Type: "string", Required: true},
				{Name: "project_key", Type: "string", Required: true},
				{Name: "work_item_id", Type: "number", Required: true},
				{Name: "work_item_type", Type: "string", Required: true},
			},
		},
	}
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

func TestCommandClient_IgnoresAllProcessWideCLIExtensions(t *testing.T) {
	if runSDKIsolationSubprocess(t) {
		return
	}
	var credentialCalls atomic.Int32
	var transportCalls atomic.Int32
	var platformCalls atomic.Int32
	extcredential.Register(sdkIsolationCredentialProvider{calls: &credentialCalls})
	exttransport.Register(sdkIsolationTransportProvider{calls: &transportCalls})
	extplatform.Register(extplatform.NewPlugin("sdk-isolation", "1.0.0").
		FailClosed().
		Observer(extplatform.Before, "observe", extplatform.All(), func(context.Context, extplatform.Invocation) {
			platformCalls.Add(1)
		}).
		Wrap("block", extplatform.All(), func(extplatform.Handler) extplatform.Handler {
			return func(context.Context, extplatform.Invocation) error {
				platformCalls.Add(1)
				return errors.New("SDK used CLI platform extension")
			}
		}).
		MustBuild())

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpc struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int64  `json:"id"`
			Method  string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		var result any
		switch rpc.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "get_workitem_brief", "description": "Get work item brief",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"work_item_id": map[string]any{"type": "number"}},
					"required":   []string{"work_item_id"},
				},
			}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"ok":true}`}}}
		default:
			http.Error(writer, "unexpected method", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer server.Close()
	previousDefaultClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = previousDefaultClient })

	client, err := NewCommandClient(server.URL, WithToken("sdk-token"))
	if err != nil {
		t.Fatalf("new SDK command client: %v", err)
	}
	if _, err := client.Execute(context.Background(), `meegle workitem get --work-item-id 123 --format json`); err != nil {
		t.Fatalf("execute SDK dynamic command: %v", err)
	}
	if credentialCalls.Load() != 0 || transportCalls.Load() != 0 || platformCalls.Load() != 0 {
		t.Fatalf("SDK invoked CLI extensions: credential=%d transport=%d platform=%d",
			credentialCalls.Load(), transportCalls.Load(), platformCalls.Load())
	}
}

func TestNewCommandClient_RegistersMetadataDefinedToolFromToolsList(t *testing.T) {
	var calledTool string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpc struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int64           `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		var result any
		switch rpc.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{
					"name": "enterprise_custom_ping",
					"metadata": map[string]any{
						"resource": "enterprise",
						"method":   "ping",
					},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
						"query": map[string]any{"type": []string{"string", "null"}},
					}},
				},
				map[string]any{
					"name":        "invalid_reserved_tool",
					"description": "must not replace local auth",
					"metadata":    map[string]any{"resource": "auth", "method": "status"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
				map[string]any{
					"name":        "invalid_name_tool",
					"description": "must be isolated",
					"metadata":    map[string]any{"resource": "corp_ops", "method": "run"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
			}}
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(rpc.Params, &params); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			calledTool = params.Name
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"ok":true}`}}}
		default:
			http.Error(writer, "unexpected method", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer server.Close()
	previousDefaultClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = previousDefaultClient })

	client, err := NewCommandClient(server.URL, WithToken("sdk-token"))
	if err != nil {
		t.Fatalf("NewCommandClient() error = %v", err)
	}
	issues := client.DiscoveryIssues()
	if len(issues) != 2 || issues[0].Code != "reserved_path" || issues[1].Code != "invalid_command" {
		t.Fatalf("DiscoveryIssues() = %+v, want stable diagnostics for both isolated tools", issues)
	}
	output, err := client.Execute(context.Background(), "meegle enterprise ping --query demo --format json")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if calledTool != "enterprise_custom_ping" {
		t.Fatalf("called tool = %q, want enterprise_custom_ping", calledTool)
	}
	if !strings.Contains(string(output), `"ok": true`) && !strings.Contains(string(output), `"ok":true`) {
		t.Fatalf("Execute() output = %s, want successful tool result", output)
	}
}

func TestNewCommandClient_ExecutesLocalCLIAPICommand(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var rpc struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if rpc.Method != "tools/list" {
			http.Error(writer, "unexpected MCP call", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name": "get_workitem_brief",
				"metadata": map[string]any{
					"resource": "workitem",
					"method":   "get",
				},
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}},
		})
	}))
	defer server.Close()

	previousDefaultClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = previousDefaultClient })

	client, err := NewCommandClient(server.URL, WithToken("sdk-token"))
	if err != nil {
		t.Fatalf("NewCommandClient() error = %v", err)
	}
	output, err := client.Execute(context.Background(),
		`meegle ai-handoff create-link --query "summarize" --dry-run --format json`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(string(output), `"dry_run": true`) {
		t.Fatalf("Execute() output = %s", output)
	}
	if calls.Load() != 1 {
		t.Fatalf("MCP calls = %d, want discovery only", calls.Load())
	}
}

func runSDKIsolationSubprocess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SDK_CLI_EXTENSION_ISOLATION_HELPER") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandClient_IgnoresAllProcessWideCLIExtensions$")
	command.Env = append(os.Environ(), "SDK_CLI_EXTENSION_ISOLATION_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("SDK isolation helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("SDK isolation helper failed: %v\n%s", err, output)
	}
	return true
}

func TestExecuteCommand_EmptyCommand(t *testing.T) {
	_, err := ExecuteCommand(context.Background(), "example.com", "", WithToken("test"))
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecuteCommand_AggregatesMissingRequiredFlags(t *testing.T) {
	c, err := newTestCommandClient()
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = c.Execute(context.Background(), "meegle workflow list-state-transitions --project-key demo --work-item-id 1 --dry-run")
	if err == nil {
		t.Fatal("expected missing required flags error")
	}
	var me *meerrors.MeegleError
	if !errors.As(err, &me) {
		t.Fatalf("error = %T, want *MeegleError", err)
	}
	if got, want := me.Message, "missing required parameters: --user-key, --work-item-type"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if me.Code != "CLIENT_MISSING_REQUIRED" || me.ExitCode != 1 {
		t.Fatalf("code/exit = %s/%d, want CLIENT_MISSING_REQUIRED/1", me.Code, me.ExitCode)
	}
}

func TestExecuteCommand_AcceptsRequiredInputsFromDirectParamsAndSet(t *testing.T) {
	c, err := newTestCommandClient()
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	out, err := c.Execute(context.Background(), `meegle workflow list-state-transitions --project-key demo --work-item-id 1 --params "{\"work_item_type\":\"story\"}" --set user_key=user-1 --dry-run`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, fragment := range []string{`"project_key": "demo"`, `"work_item_type": "story"`, `"user_key": "user-1"`} {
		if !strings.Contains(string(out), fragment) {
			t.Fatalf("output missing %q:\n%s", fragment, out)
		}
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

func TestParseArgs_EscapedNewlines(t *testing.T) {
	args := parseArgs(nil, `meegle comment add --content "first line\n\nsecond line"`)
	expected := []string{"comment", "add", "--content", "first line\n\nsecond line"}
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
