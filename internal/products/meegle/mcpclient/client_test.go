// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeRPCResponse(id int64, result json.RawMessage) jsonRPCResponse {
	return jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func TestCallToolSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := makeRPCResponse(req.ID, json.RawMessage(`{"content":[{"type":"text","text":"{\"id\":123}"}]}`))
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	resp, err := client.CallTool(context.Background(), "test_tool", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resp.Data)
	}
	if m["id"] != float64(123) {
		t.Errorf("expected id=123, got %v", m["id"])
	}
}

func TestCallToolServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := makeRPCResponse(req.ID, json.RawMessage(`{"content":[{"type":"text","text":"something went wrong"}],"isError":true}`))
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "something went wrong" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestListTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		result := json.RawMessage(`{
			"tools": [{
				"name": "workitem_get",
				"description": "Get a workitem",
				"inputSchema": {
					"type": "object",
					"properties": {
						"project_key": {"type": "string", "description": "Project key"},
						"workitem_id": {"type": "number", "description": "Workitem ID"}
					},
					"required": ["project_key"]
				}
			}]
		}`)
		resp := makeRPCResponse(req.ID, result)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if tool.Name != "workitem_get" {
		t.Errorf("expected name=workitem_get, got %q", tool.Name)
	}
	if tool.Description != "Get a workitem" {
		t.Errorf("unexpected description: %q", tool.Description)
	}
	if len(tool.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(tool.Parameters))
	}

	// Build a map for easier lookup since map iteration order is non-deterministic
	paramMap := make(map[string]interface{})
	for _, p := range tool.Parameters {
		paramMap[p.Name] = p
	}

	pkParam, ok := paramMap["project_key"]
	if !ok {
		t.Fatal("expected project_key parameter")
	}
	if pk, ok := pkParam.(interface{ GetRequired() bool }); ok {
		_ = pk
	}
	// Check via direct loop
	for _, p := range tool.Parameters {
		switch p.Name {
		case "project_key":
			if !p.Required {
				t.Errorf("project_key should be required")
			}
			if p.Type != "string" {
				t.Errorf("project_key type: expected string, got %q", p.Type)
			}
		case "workitem_id":
			if p.Required {
				t.Errorf("workitem_id should not be required")
			}
			if p.Type != "number" {
				t.Errorf("workitem_id type: expected number, got %q", p.Type)
			}
		default:
			t.Errorf("unexpected parameter: %q", p.Name)
		}
	}
}

func TestHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestWithToken(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := makeRPCResponse(req.ID, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`))
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, WithToken(func() (string, error) {
		return "mytoken123", nil
	}))
	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "Bearer mytoken123" {
		t.Errorf("expected Authorization: Bearer mytoken123, got %q", capturedAuth)
	}
}

// Custom auth header mode: token lands in the named header as a raw value
// and Authorization must be entirely absent (some backends reject requests
// that carry both).
func TestWithAuthHeader_SendsCustomHeaderAndSuppressesAuthorization(t *testing.T) {
	var capturedAuth, capturedCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustom = r.Header.Get("x-meegle-auth")
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := makeRPCResponse(req.ID, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`))
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL,
		WithToken(func() (string, error) { return "u-custom", nil }),
		WithAuthHeader("x-meegle-auth"),
	)
	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCustom != "u-custom" {
		t.Errorf("expected x-meegle-auth: u-custom, got %q", capturedCustom)
	}
	if capturedAuth != "" {
		t.Errorf("expected Authorization to be absent, got %q", capturedAuth)
	}
}
