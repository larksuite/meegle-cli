// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

// The shared session lives under a single namespace: SessionStep resolves the
// identity once and writes it under `session.*`. Each backend runtime then
// consumes it its own way — the MCP runtime PROJECTS `session.* -> mcp.*`
// (because the MCP client reads `mcp.*`), while the CLI-API runtime reads
// `session.*` directly. These two contracts are wired by hand in separate
// files (session.go, mcp_runtime.go, cliapi_runtime.go), so a key added to one
// side but forgotten on the other would silently drop identity — an auth or
// host regression that compiles cleanly. The tests below lock both directions.

// resolvedSessionKeys enumerates every `session.*` key SessionStep can emit for
// a fully-populated keychain identity (SourceStore is the richest source: it is
// the only one that attaches the token manager and store). Both projection
// directions are asserted against this list, so extending SessionStep without
// extending the consumers fails here.
var resolvedSessionKeys = []string{
	"session.host",
	"session.token",
	"session.headers",
	"session.identity_source",
	"session.access_token_header",
	"session.profile",
	"session.http_client",
	"session.user_agent",
	"session.store",
	"session.token_manager",
}

// fullyResolvedSessionState runs the real SessionStep against a SourceStore
// identity so every session.* key is populated the same way production does.
func fullyResolvedSessionState(t *testing.T) *pipeline.PipelineContext {
	t.Helper()
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "session.example.com")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "tok-session")
	t.Setenv("MEEGLE_ACCESS_TOKEN_HEADER", "x-meegle-auth")
	t.Setenv("MEEGLE_USER_AGENT", "proj-caller")

	step := &SessionStep{HTTPClient: &http.Client{}}
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			FullPath: []string{"workitem", "get-brief"},
			Flags:    map[string]any{},
		},
		OutputConfig: map[string]any{},
	}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("session step: %v", err)
	}
	return state
}

// mcpConsumedKeys enumerates every mcp.* key the MCP client and its siblings
// read out of pipeline state (newMcpClientFromState + attachment shortcuts).
// The MCP runtime does not set these directly — it projects them from the
// session.* namespace — so this is the authoritative list the projection MUST
// cover. Keep it in sync with the readers if they change; that is the point.
//
// mcp.server_url and mcp.injected are intentionally excluded: they are only
// ever set by the SDK inject path (never by SessionStep/projection), and CLI
// mode derives server_url from mcp.host as a fallback.
var mcpConsumedKeys = []string{
	"mcp.host",
	"mcp.token",
	"mcp.headers",
	"mcp.access_token_header",
	"mcp.http_client",
	"mcp.user_agent",
	"mcp.token_manager",
	"mcp.store",
}

// Direction 1 (structural): every mcp.* key the MCP client consumes must be
// supplied by projectResolvedSessionToMCP from a matching session.* key. This
// is the cheap drift detector in the consumer direction: if the MCP client
// starts reading mcp.foo but the projection doesn't produce it (or SessionStep
// stops producing session.foo), this fails immediately — before any command
// silently loses part of its identity.
func TestSessionProjection_MCPProjectionCoversEveryConsumedKey(t *testing.T) {
	state := fullyResolvedSessionState(t)

	// Precondition: SourceEnv (env-var identity) does not attach a token
	// manager or store, so those two session keys are legitimately absent here.
	// Assert the rest are present so the projection assertions mean something.
	for _, key := range resolvedSessionKeys {
		if key == "session.store" || key == "session.token_manager" {
			continue
		}
		if _, ok := state.OutputConfig[key]; !ok {
			t.Fatalf("precondition: SessionStep did not populate %q; update this test or SessionStep", key)
		}
	}

	if err := (&MCPRuntime{Session: &SessionStep{}}).PrepareSession(context.Background(), state); err != nil {
		t.Fatalf("prepare session: %v", err)
	}

	for _, mcpKey := range mcpConsumedKeys {
		sessionKey := "session." + strings.TrimPrefix(mcpKey, "mcp.")
		want, present := state.OutputConfig[sessionKey]
		if !present {
			// Only store/token_manager may be absent (SourceEnv). Anything else
			// missing means SessionStep failed to resolve a consumed identity.
			if sessionKey == "session.store" || sessionKey == "session.token_manager" {
				continue
			}
			t.Errorf("MCP consumes %q but SessionStep produced no %q", mcpKey, sessionKey)
			continue
		}
		got, ok := state.OutputConfig[mcpKey]
		if !ok {
			t.Errorf("MCP consumes %q but projectResolvedSessionToMCP did not project it from %q", mcpKey, sessionKey)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("projected %q = %v, want %v (same value as %q)", mcpKey, got, want, sessionKey)
		}
	}
}

// Direction 1 (behavioral): the projected mcp.* keys must actually reach the
// MCP transport. Drives the real McpExecutorStep against a fake MCP server and
// asserts host, custom auth header, and user-agent — all of which arrived only
// via the session.* -> mcp.* projection — land on the wire.
func TestSessionProjection_MCPRequestUsesProjectedIdentity(t *testing.T) {
	var gotAuth, gotCustom, gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("x-meegle-auth")
		gotUA = r.Header.Get("User-Agent")
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

	state := fullyResolvedSessionState(t)
	if err := (&MCPRuntime{Session: &SessionStep{}}).PrepareSession(context.Background(), state); err != nil {
		t.Fatalf("prepare session: %v", err)
	}
	// Point the projected identity at the fake server; server_url is CLI-mode
	// derived from host, so override it explicitly for the test transport.
	state.OutputConfig["mcp.server_url"] = server.URL
	state.Parsed.Node = &registry.CommandNode{HandlerRef: "get_work_item"}
	state.Values = map[string]any{}

	if err := (&McpExecutorStep{}).Execute(context.Background(), state); err != nil {
		t.Fatalf("mcp executor: %v", err)
	}
	// Custom auth header mode: token rides x-meegle-auth and Authorization is suppressed.
	if gotCustom != "tok-session" {
		t.Errorf("x-meegle-auth = %q, want tok-session (token lost in projection)", gotCustom)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (custom-header mode suppresses it)", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "meegle-cli") || !strings.HasSuffix(gotUA, " proj-caller") {
		t.Errorf("User-Agent = %q, want 'meegle-cli[/ver] proj-caller'", gotUA)
	}
}

// Direction 2 (behavioral): the CLI-API runtime reads session.* directly (no
// projection). Drives a real availability call through the real
// newCLIAPIClientFromState against a fake API server and asserts the identity
// resolved by SessionStep reaches the wire. A renamed/dropped session.* key
// that CLI-API still reads would surface here as a missing header or wrong host.
//
// A TLS test server is used so the client's real BaseURL construction
// (GetAPIBaseURL forces https from session.host) is exercised end to end; the
// server's own client is injected via session.http_client so the cert is
// trusted without weakening the transport under test.
func TestSessionProjection_CLIAPIRequestUsesResolvedSession(t *testing.T) {
	var gotCustom, gotUA, gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCustom = r.Header.Get("x-meegle-auth")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"available": true, "mode": "auto"},
		})
	}))
	defer server.Close()

	state := fullyResolvedSessionState(t)
	// Redirect the session identity at the TLS server: BaseURL is built from
	// session.host (https-forced), and session.http_client must trust the cert.
	state.OutputConfig["session.host"] = strings.TrimPrefix(server.URL, "https://")
	state.OutputConfig["session.http_client"] = server.Client()

	client := newCLIAPIClientFromState(state)
	availability, err := client.Availability(context.Background())
	if err != nil {
		t.Fatalf("availability call: %v", err)
	}
	if availability == nil || !availability.Available {
		t.Fatalf("availability = %+v, want available=true", availability)
	}
	if gotPath != "/goapi/v5/meeglecli/config" {
		t.Errorf("path = %q, want /goapi/v5/meeglecli/config", gotPath)
	}
	if gotCustom != "tok-session" {
		t.Errorf("x-meegle-auth = %q, want tok-session (session.token/access_token_header not read)", gotCustom)
	}
	if !strings.HasPrefix(gotUA, "meegle-cli") || !strings.HasSuffix(gotUA, " proj-caller") {
		t.Errorf("User-Agent = %q, want 'meegle-cli[/ver] proj-caller' (session.user_agent not read)", gotUA)
	}
}
