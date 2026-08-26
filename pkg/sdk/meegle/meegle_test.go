// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_DiscoveryFailure(t *testing.T) {
	_, err := NewClient("example.com", WithToken("test"))
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
	if !strings.Contains(err.Error(), "tool discovery failed") {
		t.Fatalf("expected 'tool discovery failed' in error, got: %v", err)
	}
}

func TestNewClient_EmptyHost(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestNewClient_TokenAndAuthHeaderConflict(t *testing.T) {
	_, err := NewClient("example.com",
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

func TestExecute_DiscoveryFailure(t *testing.T) {
	_, err := Execute(context.Background(), "example.com", "meegle --help", WithToken("test"))
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
}

func TestClient_DiscoveryIssuesIsNilSafe(t *testing.T) {
	var client *Client
	if issues := client.DiscoveryIssues(); issues != nil {
		t.Fatalf("nil client DiscoveryIssues() = %+v, want nil", issues)
	}
}

func TestClient_DiscoveryIssuesAtPublicSDKBoundary(t *testing.T) {
	calledTool := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpc struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
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
					"name": "enterprise_ping", "metadata": map[string]any{"resource": "enterprise", "method": "ping"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
				map[string]any{
					"name": "remote_auth", "metadata": map[string]any{"resource": "auth", "method": "status"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
				map[string]any{
					"name": "malformed_description", "description": 42,
					"metadata":    map[string]any{"resource": "enterprise", "method": "bad"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
				map[string]any{
					"name": "oversized_tool", "description": strings.Repeat("x", 300<<10),
					"metadata":    map[string]any{"resource": "enterprise", "method": "oversized"},
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
				map[string]any{
					"name": "unknown_without_metadata", "description": "cannot be mapped",
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

	client, err := NewClient(server.URL, WithToken("sdk-token"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	issues := client.DiscoveryIssues()
	if len(issues) != 4 || issues[0].Code != "invalid_tool_definition" || issues[0].ToolName != "malformed_description" ||
		issues[1].Code != "tool_definition_too_large" || issues[1].ToolName != "oversized_tool" ||
		issues[2].Code != "missing_mapping" || issues[2].ToolName != "unknown_without_metadata" ||
		issues[3].Code != "reserved_path" || issues[3].ToolName != "remote_auth" || issues[3].Path != "auth/status" {
		t.Fatalf("DiscoveryIssues() = %+v", issues)
	}
	issues[0].Code = "mutated"
	if got := client.DiscoveryIssues(); got[0].Code != "invalid_tool_definition" {
		t.Fatalf("DiscoveryIssues() leaked mutable state: %+v", got)
	}
	output, err := client.Execute(context.Background(), "meegle enterprise ping --format json")
	if err != nil {
		t.Fatalf("Execute() valid metadata tool: %v", err)
	}
	if calledTool != "enterprise_ping" || !strings.Contains(string(output), `"ok": true`) {
		t.Fatalf("public SDK execution: tool=%q output=%s", calledTool, output)
	}
}
