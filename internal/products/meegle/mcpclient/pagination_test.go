// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

func TestListToolsPagination100Plus4(t *testing.T) {
	const cursor = "opaque /+?= 游标\n"
	var calls atomic.Int64
	var previousID atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		page := calls.Add(1)
		if req.ID <= previousID.Swap(req.ID) {
			t.Error("request IDs did not advance")
		}
		if req.Method != "tools/list" {
			t.Errorf("method = %q", req.Method)
		}
		start, end := 0, 100
		result := map[string]any{"nextCursor": cursor}
		if page == 1 {
			if len(req.Params) != 0 {
				t.Errorf("first params = %s, want omitted", req.Params)
			}
		} else {
			var params map[string]string
			if err := json.Unmarshal(req.Params, &params); err != nil || len(params) != 1 || params["cursor"] != cursor {
				t.Errorf("continuation params = %s, err = %v", req.Params, err)
			}
			start, end = 100, 104
			delete(result, "nextCursor")
		}
		tools := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			tools = append(tools, map[string]any{"name": fmt.Sprintf("tool%03d", i)})
		}
		result["tools"] = tools
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil || len(tools) != 104 || calls.Load() != 2 {
		t.Fatalf("tools = %d, calls = %d, error = %v; want 104 tools from two pages", len(tools), calls.Load(), err)
	}
	for i, tool := range tools {
		if tool.Name != fmt.Sprintf("tool%03d", i) {
			t.Fatalf("tool[%d] = %q, order changed", i, tool.Name)
		}
	}
}

// Serve actual RPC envelopes without re-encoding result bytes or their padding.
func toolPagesServer(t *testing.T, pages ...string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		index := int(calls.Add(1)) - 1
		if req.Method != "tools/list" || index >= len(pages) {
			t.Errorf("unexpected request %d: method %q", index+1, req.Method)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, pages[index])
	}))
	return server, calls
}

func TestListToolDirectorySinglePage(t *testing.T) {
	for _, raw := range []string{
		`{"tools":[]}`, `{"tools":null}`, `{}`, `null`,
		`{"tools":[{"name":"same"},{"name":"same"},null,{"name":"bad","inputSchema":{"type":["string","number"]}}],"nextCursor":""}`,
		`{"tools":[` + strings.TrimSuffix(strings.Repeat(`{"name":"sample"},`, maxDiscoveredTools+1), ",") + `]}`,
	} {
		server, calls := toolPagesServer(t, raw)
		client := New(server.URL)
		got, err := client.ListToolDirectory(context.Background())
		server.Close()
		if err != nil || string(got) != raw || calls.Load() != 1 {
			t.Fatalf("single page changed: bytes=%d, calls=%d, err=%v", len(got), calls.Load(), err)
		}
		want, wantErr := DecodeToolDirectory([]byte(raw))
		decoded, decodeErr := DecodeToolDirectory(got)
		if !reflect.DeepEqual(want, decoded) || fmt.Sprint(wantErr) != fmt.Sprint(decodeErr) {
			t.Fatal("single-page decoder behavior changed")
		}
	}
}

func TestListToolsPaginationEmptyPagesAndDiagnostics(t *testing.T) {
	server, calls := toolPagesServer(t,
		`{"tools":[],"nextCursor":"a"}`,
		`{"tools":[{"name":"same"},{"name":"bad","inputSchema":{"type":["object","string"]}}],"nextCursor":"b"}`,
		`{"tools":[],"nextCursor":"c"}`,
		`{"tools":[null,{"name":"same"}],"nextCursor":""}`,
	)
	defer server.Close()
	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil || len(tools) != 4 || calls.Load() != 4 {
		t.Fatalf("tools=%d calls=%d err=%v", len(tools), calls.Load(), err)
	}
	if tools[0].Name != "same" || tools[3].Name != "same" || tools[1].Issue == nil ||
		tools[1].Issue.Code != "unsupported_schema_union" || tools[2].Name != "tools[2]" ||
		tools[2].Issue == nil || tools[2].Issue.Code != "invalid_tool_definition" {
		t.Fatalf("order, duplicates or diagnostics changed: %+v", tools)
	}
}

func TestListToolsPaginationInvalidContinuation(t *testing.T) {
	for _, test := range []struct {
		name  string
		pages []string
		want  string
	}{
		{"repeated", []string{`{"tools":[],"nextCursor":"secret-a"}`, `{"tools":[],"nextCursor":"secret-a"}`}, "cursor repeated"},
		{"cycle", []string{`{"tools":[],"nextCursor":"secret-a"}`, `{"tools":[],"nextCursor":"secret-b"}`, `{"tools":[],"nextCursor":"secret-a"}`}, "cursor repeated"},
		{"invalid later shape", []string{`{"tools":[],"nextCursor":"a"}`, `{"tools":{}}`}, "parse tools/list result"},
		{"missing later tools", []string{`{"tools":[],"nextCursor":"a"}`, `{}`}, "tools array"},
		{"null later tools", []string{`{"tools":[],"nextCursor":"a"}`, `{"tools":null}`}, "tools array"},
		{"missing first tools", []string{`{"nextCursor":"a"}`}, "tools array"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, calls := toolPagesServer(t, test.pages...)
			defer server.Close()
			tools, err := New(server.URL).ListTools(context.Background())
			if tools != nil || err == nil || !strings.Contains(err.Error(), test.want) || calls.Load() != int64(len(test.pages)) {
				t.Fatalf("tools=%v calls=%d err=%v", tools, calls.Load(), err)
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatal("error contains cursor contents")
			}
		})
	}
	for _, value := range []string{`null`, `1`, `false`, `[]`, `{}`, `["secret-cursor"]`} {
		for _, later := range []bool{false, true} {
			t.Run(fmt.Sprintf("cursor=%s/later=%t", value, later), func(t *testing.T) {
				pages := []string{`{"tools":[],"nextCursor":` + value + `}`}
				if later {
					pages = append([]string{`{"tools":[{"name":"first"}],"nextCursor":"a"}`}, pages...)
				}
				server, calls := toolPagesServer(t, pages...)
				defer server.Close()
				raw, err := New(server.URL).ListToolDirectory(context.Background())
				if raw != nil || err == nil || err.Error() != "tools/list nextCursor must be a string" || calls.Load() != int64(len(pages)) {
					t.Fatalf("raw bytes=%d calls=%d err=%v", len(raw), calls.Load(), err)
				}
			})
		}
	}
}

func TestListToolDirectoryPreservesRawDefinitions(t *testing.T) {
	const rawTool = `{"name":"custom","metadata":{"resource":[],"custom":1.2300,"text":"<tag>"},"inputSchema":{"type":"object"}}`
	server, _ := toolPagesServer(t, `{"tools":[],"nextCursor":"a"}`, `{"tools":[`+rawTool+`]}`)
	defer server.Close()
	raw, err := New(server.URL).ListToolDirectory(context.Background())
	if err != nil || string(raw) != `{"tools":[`+rawTool+`]}` {
		t.Fatalf("raw tool policy changed: bytes=%d err=%v", len(raw), err)
	}
	// The caller can apply a tolerant metadata decoder to these original bytes.
	// The public decoder continues to isolate this malformed optional metadata.
	tools, err := DecodeToolDirectory(raw)
	if err != nil || len(tools) != 1 || tools[0].Issue == nil || tools[0].Issue.Code != "invalid_tool_definition" {
		t.Fatalf("public metadata policy changed: tools=%+v err=%v", tools, err)
	}
}

func TestListToolsPaginationFailures(t *testing.T) {
	for _, kind := range []string{"http", "rpc", "transport", "malformed result"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req jsonRPCRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				if calls.Add(1) == 1 {
					_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, json.RawMessage(`{"tools":[{"name":"first"}],"nextCursor":"next"}`)))
					return
				}
				switch kind {
				case "http":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "rpc":
					_ = json.NewEncoder(w).Encode(jsonRPCResponse{ID: req.ID, Error: &jsonRPCError{Code: -32603, Message: "test RPC failure"}})
				case "transport":
					// An incomplete response exercises the real body-read failure path.
					w.Header().Set("Content-Length", "100")
					_, _ = io.WriteString(w, "{")
				case "malformed result":
					_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, json.RawMessage(`[]`)))
				}
			}))
			defer server.Close()
			var closed atomic.Int64
			client := New(server.URL, WithHTTPClient(&http.Client{Transport: retryAdviceRoundTripper(func(req *http.Request) (*http.Response, error) {
				response, err := server.Client().Transport.RoundTrip(req)
				if err == nil {
					response.Body = paginationBody{response.Body, func() { closed.Add(1) }}
				}
				return response, err
			})}))
			tools, err := client.ListTools(context.Background())
			if tools != nil || err == nil || calls.Load() != 2 {
				t.Fatalf("tools=%v calls=%d err=%v", tools, calls.Load(), err)
			}
			if closed.Load() != 2 {
				t.Fatalf("closed response bodies=%d, want 2", closed.Load())
			}
			var structured *meerrors.MeegleError
			if kind == "http" && (!errors.As(err, &structured) || structured.Code != "SERVER_HTTP_ERROR" || structured.HTTPStatus != 503) {
				t.Fatalf("HTTP semantics changed: %v", err)
			}
			if kind == "rpc" && (!errors.As(err, &structured) || structured.Code != "SERVER_CALL_FAILED" || structured.Message != "test RPC failure") {
				t.Fatalf("RPC semantics changed: %v", err)
			}
			if kind == "transport" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("body error lost: %v", err)
			}
		})
	}
}

func repeatedToolPage(count int, cursor string) string {
	tools := strings.TrimSuffix(strings.Repeat(`{"name":"sample"},`, count), ",")
	return `{"tools":[` + tools + `],"nextCursor":"` + cursor + `"}`
}

func paddedToolPage(size int, cursor string) string {
	base := `{"tools":[{"name":"sample"}],"padding":"","nextCursor":"` + cursor + `"}`
	return strings.Replace(base, `"padding":""`, `"padding":"`+strings.Repeat("x", size-len(base))+`"`, 1)
}

func TestListToolsPaginationLimits(t *testing.T) {
	for _, test := range []struct {
		name  string
		pages []string
		want  string
		count int
	}{
		{"exact tool count", []string{repeatedToolPage(1024, "a"), repeatedToolPage(1024, "")}, "", maxDiscoveredTools},
		{"tool count", []string{repeatedToolPage(2048, "a"), repeatedToolPage(1, "")}, "2048-tool limit", 0},
		{"first page tool count", []string{repeatedToolPage(2049, "a")}, "2048-tool limit", 0},
		{"exact cumulative bytes", []string{paddedToolPage(maxToolsListResponseBytes/2, "a"), paddedToolPage(maxToolsListResponseBytes/2, "")}, "", 2},
		{"cumulative bytes", []string{paddedToolPage(maxToolsListResponseBytes/2, "a"), paddedToolPage(maxToolsListResponseBytes/2+1, "")}, "cumulative result exceeds", 0},
		{"later response bytes", []string{repeatedToolPage(1, "a"), paddedToolPage(maxToolsListResponseBytes+1, "")}, "response exceeds", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, calls := toolPagesServer(t, test.pages...)
			defer server.Close()
			tools, err := New(server.URL).ListTools(context.Background())
			if test.want != "" {
				if tools != nil || err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("tools=%d err=%v", len(tools), err)
				}
			} else if err != nil || len(tools) != test.count {
				t.Fatalf("tools=%d err=%v", len(tools), err)
			}
			if calls.Load() != int64(len(test.pages)) {
				t.Fatalf("calls=%d want %d", calls.Load(), len(test.pages))
			}
		})
	}
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("page cap/terminal=%t", terminal), func(t *testing.T) {
			pages := make([]string, maxToolsListPages)
			for i := range pages {
				pages[i] = fmt.Sprintf(`{"tools":[],"nextCursor":"%d"}`, i)
			}
			if terminal {
				pages[len(pages)-1] = `{"tools":[]}`
			}
			server, calls := toolPagesServer(t, pages...)
			defer server.Close()
			tools, err := New(server.URL).ListTools(context.Background())
			if terminal {
				if err != nil || tools == nil || len(tools) != 0 {
					t.Fatalf("last allowed page rejected: tools=%v err=%v", tools, err)
				}
			} else if tools != nil || err == nil || !strings.Contains(err.Error(), "256-page limit") {
				t.Fatalf("tools=%v err=%v", tools, err)
			}
			if calls.Load() != maxToolsListPages {
				t.Fatalf("calls=%d want %d", calls.Load(), maxToolsListPages)
			}
		})
	}
}

type paginationBody struct {
	io.ReadCloser
	onClose func()
}

func (body paginationBody) Close() error {
	err := body.ReadCloser.Close()
	body.onClose()
	return err
}

func TestListToolsPaginationContext(t *testing.T) {
	for _, kind := range []string{"canceled before", "expired before", "canceled between", "expired between"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if strings.HasPrefix(kind, "expired") {
				cancel()
				deadline := time.Now().Add(-time.Second)
				if kind == "expired between" {
					deadline = time.Now().Add(50 * time.Millisecond)
				}
				ctx, cancel = context.WithDeadline(context.Background(), deadline)
			}
			defer cancel()
			between := strings.HasSuffix(kind, "between")
			if !between && kind == "canceled before" {
				cancel()
			}
			server, calls := toolPagesServer(t, `{"tools":[{"name":"first"}],"nextCursor":"next"}`)
			defer server.Close()
			var closed atomic.Int64
			httpClient := &http.Client{Transport: retryAdviceRoundTripper(func(req *http.Request) (*http.Response, error) {
				response, err := server.Client().Transport.RoundTrip(req)
				if err == nil {
					response.Body = paginationBody{response.Body, func() {
						closed.Add(1)
						if kind == "canceled between" {
							cancel()
						} else if kind == "expired between" {
							<-ctx.Done()
						}
					}}
				}
				return response, err
			})}
			tools, err := New(server.URL, WithHTTPClient(httpClient)).ListTools(ctx)
			if tools != nil || ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
				t.Fatalf("tools=%v err=%v context=%v", tools, err, ctx.Err())
			}
			wantCalls := int64(0)
			if between {
				wantCalls = 1
			}
			if calls.Load() != wantCalls || closed.Load() != wantCalls {
				t.Fatalf("calls=%d closed=%d want=%d", calls.Load(), closed.Load(), wantCalls)
			}
		})
	}
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("in flight/deadline=%t", deadline), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			var calls atomic.Int64
			stopped := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if calls.Add(1) == 1 {
					_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[],"nextCursor":"a"}}`)
					return
				}
				if !deadline {
					cancel()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
					t.Error("server request context was not canceled")
				}
				close(stopped)
			}))
			defer server.Close()
			tools, err := New(server.URL).ListTools(ctx)
			if tools != nil || err == nil || calls.Load() != 2 {
				t.Fatalf("tools=%v calls=%d err=%v", tools, calls.Load(), err)
			}
			if !deadline && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
			if deadline {
				var structured *meerrors.MeegleError
				if !errors.As(err, &structured) || structured.Code != "SERVER_TIMEOUT" {
					t.Fatalf("existing timeout semantics changed: %v", err)
				}
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("canceled server request still running")
			}
		})
	}
}

func TestListToolsPaginationTransportAndRefresh(t *testing.T) {
	var calls, closed, tokenCalls, refreshed atomic.Int64
	var current atomic.Value
	current.Store("old-test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Test-Token") == "" || r.Header.Get("Authorization") != "" ||
			r.Header.Get("X-Test-Instance") != "same-client" || r.Header.Get("User-Agent") != "pagination-test" {
			t.Error("client identity/headers not preserved")
		}
		var req struct {
			ID     int64             `json:"id"`
			Params map[string]string `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := `{"tools":[{"name":"first"}],"nextCursor":"opaque"}`
		if req.Params != nil {
			if len(req.Params) != 1 || req.Params["cursor"] != "opaque" {
				t.Error("retry changed cursor")
			}
			if r.Header.Get("X-Test-Token") == "old-test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			result = `{"tools":[{"name":"last"}]}`
		}
		_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, json.RawMessage(result)))
	}))
	defer server.Close()
	client := New(server.URL,
		WithToken(func() (string, error) { tokenCalls.Add(1); return current.Load().(string), nil }),
		WithAuthHeader("X-Test-Token"), WithHeaders(http.Header{"X-Test-Instance": {"same-client"}}),
		WithUserAgent("pagination-test"),
		WithRefreshFunc(func() error { refreshed.Add(1); current.Store("new-test-token"); return nil }),
		WithHTTPClient(&http.Client{Transport: retryAdviceRoundTripper(func(req *http.Request) (*http.Response, error) {
			response, err := server.Client().Transport.RoundTrip(req)
			if err == nil {
				response.Body = paginationBody{response.Body, func() { closed.Add(1) }}
			}
			return response, err
		})}),
	)
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 2 || calls.Load() != 3 || closed.Load() != 3 || tokenCalls.Load() != 3 || refreshed.Load() != 1 {
		t.Fatalf("tools=%d calls=%d closed=%d tokens=%d refresh=%d err=%v", len(tools), calls.Load(), closed.Load(), tokenCalls.Load(), refreshed.Load(), err)
	}
}

func TestListToolsPaginationConcurrentAndReentry(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			ID     int64             `json:"id"`
			Params map[string]string `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := `{"tools":[{"name":"first"}],"nextCursor":"shared-opaque-cursor"}`
		if req.Params != nil {
			if req.Params["cursor"] != "shared-opaque-cursor" {
				t.Error("cursor changed")
			}
			result = `{"tools":[{"name":"last"}]}`
		}
		_ = json.NewEncoder(w).Encode(makeRPCResponse(req.ID, json.RawMessage(result)))
	}))
	defer server.Close()
	client := New(server.URL)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 2; j++ {
				tools, err := client.ListTools(context.Background())
				if err != nil || len(tools) != 2 || tools[0].Name != "first" || tools[1].Name != "last" {
					t.Errorf("concurrent directory: tools=%d err=%v", len(tools), err)
				}
			}
		}()
	}
	group.Wait()
	if calls.Load() != 32 {
		t.Fatalf("concurrent calls=%d want 32", calls.Load())
	}

	// An HTTP extension can re-enter the same client before delivery. Each
	// listing owns its cursor state; the nested request uses a context marker.
	type nestedKey struct{}
	client.httpClient = &http.Client{Transport: retryAdviceRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Context().Value(nestedKey{}) == nil {
			tools, err := client.ListTools(context.WithValue(req.Context(), nestedKey{}, true))
			if err != nil || len(tools) != 2 {
				t.Errorf("nested directory: tools=%d err=%v", len(tools), err)
			}
		}
		return server.Client().Transport.RoundTrip(req)
	})}
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 2 || calls.Load() != 38 {
		t.Fatalf("reentry tools=%d calls=%d err=%v", len(tools), calls.Load(), err)
	}
}

func TestListToolsPaginationDefinitionLimits(t *testing.T) {
	oversized := `{"name":"oversized","description":"` + strings.Repeat("<", maxToolDefinitionBytes) + `"}`
	properties := make(map[string]any, maxToolParameters+1)
	for i := 0; i <= maxToolParameters; i++ {
		properties[fmt.Sprintf("param%d", i)] = map[string]any{"type": "string"}
	}
	tooManyParams, err := json.Marshal(map[string]any{
		"name": "too_many_params", "inputSchema": map[string]any{"type": "object", "properties": properties},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := toolPagesServer(t, `{"tools":[{"name":"first"}],"nextCursor":"a"}`,
		`{"tools":[`+oversized+`,`+string(tooManyParams)+`,{"name":"last"}]}`)
	defer server.Close()
	tools, err := New(server.URL).ListTools(context.Background())
	if err != nil || len(tools) != 4 || tools[0].Name != "first" || tools[3].Name != "last" ||
		tools[1].Issue == nil || tools[1].Issue.Code != "tool_definition_too_large" ||
		tools[2].Issue == nil || tools[2].Issue.Code != "invalid_tool_definition" {
		t.Fatalf("definition/parameter isolation changed: count=%d err=%v", len(tools), err)
	}
}
