// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
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

func TestListToolsAcceptsNullableUnionParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		result := json.RawMessage(`{
			"tools": [{
				"name": "nullable_search",
				"metadata": {"resource": "enterprise", "method": "search"},
				"inputSchema": {
					"type": "object",
					"properties": {
						"query": {"type": ["string", "null"], "description": "Optional query"},
						"tags": {"type": "array", "items": {"type": ["string", "null"]}}
					}
				}
			}]
		}`)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(request.ID, result))
	}))
	defer server.Close()

	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Issue != nil || tools[0].Name != "nullable_search" {
		t.Fatalf("tools = %+v, want one usable nullable_search tool", tools)
	}
	if len(tools[0].Parameters) != 2 {
		t.Fatalf("parameters = %+v", tools[0].Parameters)
	}
	parameters := map[string]types.ToolParameter{}
	for _, parameter := range tools[0].Parameters {
		parameters[parameter.Name] = parameter
	}
	if got := parameters["query"].Type; got != "string" {
		t.Fatalf("nullable query type = %q, want string", got)
	}
	if tags := parameters["tags"]; tags.Type != "array" || tags.Items == nil || tags.Items.Type != "string" {
		t.Fatalf("nullable array items = %+v, want array<string>", tags)
	}
}

func TestListToolsDiagnosesUnsupportedMultiTypeUnion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		result := json.RawMessage(`{
			"tools": [{
				"name": "ambiguous_search",
				"metadata": {"resource": "enterprise", "method": "search"},
				"inputSchema": {"type": "object", "properties": {
					"query": {"type": ["string", "number", "null"]}
				}}
			}]
		}`)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(request.ID, result))
	}))
	defer server.Close()

	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Issue == nil || tools[0].Issue.Code != "unsupported_schema_union" {
		t.Fatalf("tools = %+v, want unsupported_schema_union diagnostic", tools)
	}
}

func TestListToolsIsolatesMalformedEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := json.RawMessage(`{
			"tools": [
				{"name":"valid_tool","metadata":{"resource":"enterprise","method":"ping"},"inputSchema":{"type":"object","properties":{}}},
				{"name":"bad_description","description":42,"metadata":{"resource":"enterprise","method":"bad-description"},"inputSchema":{"type":"object","properties":{}}},
				{"name":"bad_property","metadata":{"resource":"enterprise","method":"bad-property"},"inputSchema":{"type":"object","properties":{"value":[]}}}
			]
		}`)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, result))
	}))
	defer server.Close()

	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v, want malformed entries isolated", err)
	}
	if len(tools) != 3 || tools[0].Name != "valid_tool" {
		t.Fatalf("tools = %+v", tools)
	}
	for index, name := range []string{"bad_description", "bad_property"} {
		issue := tools[index+1].Issue
		if issue == nil || issue.Code != "invalid_tool_definition" || tools[index+1].Name != name {
			t.Fatalf("tools[%d] = %+v, want parse issue for %s", index+1, tools[index+1], name)
		}
	}
}

func TestListToolsIsolatesOversizedToolAndParameterSchema(t *testing.T) {
	properties := make(map[string]any, maxToolParameters+1)
	for index := 0; index <= maxToolParameters; index++ {
		properties[fmt.Sprintf("field_%d", index)] = map[string]any{"type": "string"}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, mustJSONRaw(t, map[string]any{"tools": []any{
			map[string]any{"name": "valid_tool", "metadata": map[string]any{"resource": "enterprise", "method": "ping"}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			map[string]any{"name": "oversized_tool", "description": strings.Repeat("x", maxToolDefinitionBytes), "metadata": map[string]any{"resource": "enterprise", "method": "large"}},
			map[string]any{"name": "too_many_params", "metadata": map[string]any{"resource": "enterprise", "method": "wide"}, "inputSchema": map[string]any{"type": "object", "properties": properties}},
		}})))
	}))
	defer server.Close()

	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 3 || tools[0].Name != "valid_tool" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[1].Issue == nil || tools[1].Issue.Code != "tool_definition_too_large" {
		t.Fatalf("oversized tool = %+v", tools[1])
	}
	if tools[2].Issue == nil || tools[2].Issue.Code != "invalid_tool_definition" {
		t.Fatalf("wide tool = %+v", tools[2])
	}
}

func TestListToolsRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", maxToolsListResponseBytes+1)))
	}))
	defer server.Close()
	_, err := New(server.URL).ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("ListTools() error = %v, want response size limit", err)
	}
}

func TestCallToolRejectsOversizedResponse(t *testing.T) {
	const expectedMaxRPCResponseBytes = 32 << 20
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat(" ", expectedMaxRPCResponseBytes+1)))
	}))
	defer server.Close()

	_, err := New(server.URL).CallTool(context.Background(), "oversized_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "response exceeds 33554432-byte limit") {
		t.Fatalf("CallTool() error = %v, want 32 MiB response size limit", err)
	}
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return payload
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

func TestWithToken_RejectsHTTPSDowngradeBeforeLeakingBearerToken(t *testing.T) {
	var targetCalls atomic.Int32
	var leakedToken atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		leakedToken.Store(request.Header.Get("Authorization"))
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	roots := x509.NewCertPool()
	roots.AddCert(redirector.Certificate())
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	client := New(redirector.URL,
		WithHTTPClient(httpClient),
		WithToken(func() (string, error) { return "bearer-secret", nil }),
	)

	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("CallTool() error = %v, want credential redirect rejection", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
	if leaked := leakedToken.Load(); leaked != nil && leaked != "" {
		t.Fatalf("Bearer token leaked to downgrade target: %q", leaked)
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

func TestWithAuthHeader_OverridesStaticCredentialHeaders(t *testing.T) {
	var capturedAuth string
	var capturedCustom []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		capturedAuth = request.Header.Get("Authorization")
		capturedCustom = append([]string(nil), request.Header.Values("X-Meegle-Auth")...)
		var rpcRequest jsonRPCRequest
		_ = json.NewDecoder(request.Body).Decode(&rpcRequest)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(rpcRequest.ID, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`)))
	}))
	defer server.Close()

	client := New(server.URL,
		WithHeaders(http.Header{
			"Authorization": []string{"Bearer stale-profile-token"},
			"X-Meegle-Auth": []string{"stale-custom-token"},
		}),
		WithToken(func() (string, error) { return "fresh-extension-token", nil }),
		WithAuthHeader("X-Meegle-Auth"),
	)
	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if capturedAuth != "" {
		t.Fatalf("Authorization = %q, want absent in custom auth mode", capturedAuth)
	}
	if len(capturedCustom) != 1 || capturedCustom[0] != "fresh-extension-token" {
		t.Fatalf("X-Meegle-Auth values = %q, want only fresh extension token", capturedCustom)
	}
}

func TestWithAuthHeader_RejectsCrossOriginRedirectBeforeLeakingToken(t *testing.T) {
	var targetCalls atomic.Int32
	var leakedToken atomic.Value
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		leakedToken.Store(r.Header.Get("X-Enterprise-Token"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"?access_token=do-not-report", http.StatusFound)
	}))
	defer redirector.Close()

	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	roots.AddCert(redirector.Certificate())
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	client := New(redirector.URL,
		WithHTTPClient(httpClient),
		WithToken(func() (string, error) { return "enterprise-secret", nil }),
		WithAuthHeader("X-Enterprise-Token"),
	)

	_, err := client.CallTool(context.Background(), "test_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("CallTool() error = %v, want cross-origin redirect rejection", err)
	}
	if strings.Contains(err.Error(), "do-not-report") {
		t.Fatalf("CallTool() error leaked redirect query secret: %v", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
	if leaked := leakedToken.Load(); leaked != nil && leaked != "" {
		t.Fatalf("custom token leaked to redirect target: %q", leaked)
	}
}

func TestWithAuthHeader_BaseRedirectCallbackCannotHideCrossOriginByMutatingRedirectHistory(t *testing.T) {
	var targetCalls atomic.Int32
	var leakedToken atomic.Value
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		leakedToken.Store(request.Header.Get("X-Enterprise-Token"))
		var rpcRequest jsonRPCRequest
		_ = json.NewDecoder(request.Body).Decode(&rpcRequest)
		_ = json.NewEncoder(w).Encode(makeRPCResponse(rpcRequest.ID, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`)))
	}))
	defer target.Close()

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, "/same-origin-target", http.StatusFound)
	}))
	defer redirector.Close()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	roots.AddCert(redirector.Certificate())
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			request.URL.Scheme = targetURL.Scheme
			request.URL.Host = targetURL.Host
			request.URL.Path = targetURL.Path
			via[0].URL.Scheme = targetURL.Scheme
			via[0].URL.Host = targetURL.Host
			return nil
		},
	}
	client := New(redirector.URL,
		WithHTTPClient(httpClient),
		WithToken(func() (string, error) { return "enterprise-secret", nil }),
		WithAuthHeader("X-Enterprise-Token"),
	)

	_, err = client.CallTool(context.Background(), "test_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("CallTool() error = %v, want immutable cross-origin rejection", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
	if leaked := leakedToken.Load(); leaked != nil && leaked != "" {
		t.Fatalf("custom token leaked to callback-selected target: %q", leaked)
	}
}

func TestCredentialRedirectGuard_FreezesInitialOriginAcrossRedirects(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		authHeader string
		readToken  func(http.Header) string
	}{
		{
			name:      "bearer",
			readToken: func(header http.Header) string { return header.Get("Authorization") },
		},
		{
			name:       "custom-header",
			authHeader: "X-Enterprise-Token",
			readToken:  func(header http.Header) string { return header.Get("X-Enterprise-Token") },
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var targetCalls atomic.Int32
			var leakedToken atomic.Value
			target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				targetCalls.Add(1)
				leakedToken.Store(testCase.readToken(request.Header))
				var rpcRequest jsonRPCRequest
				_ = json.NewDecoder(request.Body).Decode(&rpcRequest)
				_ = json.NewEncoder(writer).Encode(makeRPCResponse(rpcRequest.ID, json.RawMessage(`{"content":[{"type":"text","text":"unexpected"}]}`)))
			}))
			defer target.Close()

			redirector := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/middle" {
					http.Redirect(writer, request, target.URL, http.StatusFound)
					return
				}
				http.Redirect(writer, request, "/middle", http.StatusFound)
			}))
			defer redirector.Close()

			targetURL, err := url.Parse(target.URL)
			if err != nil {
				t.Fatalf("parse target URL: %v", err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(target.Certificate())
			roots.AddCert(redirector.Certificate())
			httpClient := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
				CheckRedirect: func(_ *http.Request, via []*http.Request) error {
					if len(via) == 1 {
						via[0].URL = targetURL
					}
					return nil
				},
			}
			options := []Option{
				WithHTTPClient(httpClient),
				WithToken(func() (string, error) { return "credential-secret", nil }),
			}
			if testCase.authHeader != "" {
				options = append(options, WithAuthHeader(testCase.authHeader))
			}
			client := New(redirector.URL, options...)

			_, err = client.CallTool(context.Background(), "test_tool", nil)
			if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
				t.Fatalf("CallTool() error = %v, want immutable initial-origin rejection", err)
			}
			if got := targetCalls.Load(); got != 0 {
				t.Fatalf("redirect target calls = %d, want 0", got)
			}
			if leaked := leakedToken.Load(); leaked != nil && leaked != "" {
				t.Fatalf("credential leaked after redirect-history mutation: %q", leaked)
			}
		})
	}
}

func TestCredentialRedirectGuard_StopsSameOriginLoopAtTenHops(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		authHeader   string
		baseRedirect bool
	}{
		{name: "bearer-default-client"},
		{name: "custom-header-existing-callback", authHeader: "X-Enterprise-Token", baseRedirect: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				http.Redirect(writer, request, "/loop", http.StatusFound)
			}))
			defer server.Close()

			httpClient := &http.Client{}
			if testCase.baseRedirect {
				httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
			}
			options := []Option{
				WithHTTPClient(httpClient),
				WithToken(func() (string, error) { return "credential-secret", nil }),
			}
			if testCase.authHeader != "" {
				options = append(options, WithAuthHeader(testCase.authHeader))
			}
			client := New(server.URL+"/loop", options...)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := client.CallTool(ctx, "test_tool", nil)
			if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
				t.Fatalf("CallTool() error = %v, want ten-redirect limit", err)
			}
			if got := requests.Load(); got != 10 {
				t.Fatalf("redirect requests = %d, want 10", got)
			}
		})
	}
}
