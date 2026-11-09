// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

func configureToolsPaginationCLI(t *testing.T, server *httptest.Server) {
	t.Helper()
	setupTestDir(t)
	if err := SaveProfileConfig("pagination", MeegleConfig{Host: server.URL, AccessToken: "local-test-token"}); err != nil {
		t.Fatal(err)
	}
	previousArgs, previousClient := os.Args, http.DefaultClient
	os.Args = []string{"meegle", "pagination", "tool104", "--profile", "pagination", "--format", "json"}
	http.DefaultClient = server.Client()
	t.Cleanup(func() { os.Args, http.DefaultClient = previousArgs, previousClient })
}

func executeToolsPaginationCLI(t *testing.T) (*cliapp.App, string, error) {
	t.Helper()
	app, err := NewCLIAppContext(context.Background(), "test", nil)
	if err != nil {
		return nil, "", err
	}
	var stdout, stderr bytes.Buffer
	err = app.ExecuteWithIO(context.Background(), os.Args[1:], &stdout, &stderr)
	return app, strings.TrimSpace(stdout.String()), err
}

func TestCLIToolsPaginationDiscoversExecutesAndCaches104(t *testing.T) {
	var listCalls, toolCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp_server/v1" || r.Header.Get("Authorization") != "Bearer local-test-token" {
			t.Error("normal CLI route or identity changed")
		}
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
				Name   string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := map[string]any{}
		switch req.Method {
		case "tools/list":
			page := listCalls.Add(1)
			start, end := 1, 101
			if page == 1 {
				if req.Params.Cursor != "" {
					t.Error("first page has a cursor")
				}
				result["nextCursor"] = "page-100"
			} else if page == 2 && req.Params.Cursor == "page-100" {
				start, end = 101, 105
			} else {
				t.Errorf("unexpected discovery page %d", page)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			tools := make([]map[string]any, 0, end-start)
			for index := start; index < end; index++ {
				tools = append(tools, map[string]any{
					"name":        fmt.Sprintf("pagination__tool%03d", index),
					"description": "Pagination test tool",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				})
			}
			result["tools"] = tools
		case "tools/call":
			toolCalls.Add(1)
			if req.Params.Name != "pagination__tool104" {
				t.Errorf("called tool %q", req.Params.Name)
			}
			result["content"] = []any{map[string]any{"type": "text", "text": `{"tool":"pagination__tool104"}`}}
		default:
			t.Errorf("unexpected RPC method %q", req.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	configureToolsPaginationCLI(t, server)
	for attempt := 0; attempt < 2; attempt++ {
		app, output, err := executeToolsPaginationCLI(t)
		var data map[string]string
		decodeErr := json.Unmarshal([]byte(output), &data)
		if err != nil || decodeErr != nil || len(data) != 1 || data["tool"] != "pagination__tool104" {
			t.Fatalf("CLI attempt %d: output=%q err=%v", attempt, output, err)
		}
		group, _, err := app.RootCommand().Find([]string{"pagination"})
		if err != nil || group == nil || len(group.Commands()) != 104 {
			t.Fatalf("command tree does not contain all 104 tools: err=%v", err)
		}
	}
	cached, err := NewToolCache(GetCacheDir(), "pagination", DefaultTTL).Get()
	if err != nil || cached == nil || cached.Stale || len(cached.Tools) != 104 || cached.Tools[103].Name != "pagination__tool104" {
		t.Fatalf("incomplete cache: value=%v err=%v", cached, err)
	}
	if listCalls.Load() != 2 || toolCalls.Load() != 2 {
		t.Fatalf("list=%d tool=%d; want two pages then cache reuse", listCalls.Load(), toolCalls.Load())
	}
}

func TestCLIToolsPaginationFailureDoesNotCachePartialDirectory(t *testing.T) {
	for _, kind := range []string{"http", "rpc", "cursor"} {
		for _, existingCache := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", kind, existingCache), func(t *testing.T) {
				var calls, executions atomic.Int64
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     int64  `json:"id"`
						Method string `json:"method"`
					}
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.Method != "tools/list" {
						executions.Add(1)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if calls.Add(1) == 1 {
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"pagination__tool104"}],"nextCursor":"next"}}`, req.ID)
						return
					}
					switch kind {
					case "http":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "rpc":
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"test failure"}}`, req.ID)
					case "cursor":
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":null}}`, req.ID)
					}
				}))
				defer server.Close()
				configureToolsPaginationCLI(t, server)
				cache := NewToolCache(GetCacheDir(), "pagination", DefaultTTL)
				previous := []types.ToolDefinition{{Name: "pagination__previous"}}
				var before []byte
				if existingCache {
					if err := cache.Set(previous); err != nil {
						t.Fatal(err)
					}
					var err error
					before, err = os.ReadFile(cache.storage.filePath)
					if err != nil {
						t.Fatal(err)
					}
				}
				os.Args = append(os.Args, "--refresh")
				app, _, runErr := executeToolsPaginationCLI(t)
				if (!existingCache && runErr == nil) || executions.Load() != 0 || calls.Load() != 2 {
					t.Fatalf("partial directory accepted: calls=%d executions=%d err=%v", calls.Load(), executions.Load(), runErr)
				}
				if app == nil {
					t.Fatal("normal CLI failed to construct")
				}
				found, remaining, findErr := app.RootCommand().Find([]string{"pagination", "tool104"})
				if findErr == nil && len(remaining) == 0 && found.Name() == "tool104" {
					t.Fatal("first-page tool was registered after pagination failed")
				}
				cached, err := cache.Get()
				if err != nil {
					t.Fatal(err)
				}
				if existingCache {
					previousCommand, remaining, findErr := app.RootCommand().Find([]string{"pagination", "previous"})
					if findErr != nil || len(remaining) != 0 || previousCommand == nil || previousCommand.Name() != "previous" {
						t.Fatal("previous complete directory was not used as fallback")
					}
					after, err := os.ReadFile(cache.storage.filePath)
					if err != nil || !bytes.Equal(before, after) || cached == nil || !reflect.DeepEqual(cached.Tools, previous) {
						t.Fatalf("previous complete cache changed: err=%v", err)
					}
				} else if cached != nil {
					t.Fatal("failed pagination created a cache")
				}
			})
		}
	}
}
