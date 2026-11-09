// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"encoding/json"
	"testing"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

// The production MCP server emits the trace id as a content entry prefixed
// with "log_id:" (underscore). An earlier version of this code matched only
// "logid:", which silently dropped every id — the slog.Debug hop hid the
// breakage until --envelope tried to surface meta.logid and saw nothing. Pin
// the live prefix here so a server-side rename or local prefix typo fails
// loudly instead of leaking back into the same silent-drop bug.
func TestUnwrapResponse_CapturesLogIDFromLiveServerPrefix(t *testing.T) {
	raw := mustMarshal(t, mcpToolResponse{
		Content: []mcpContentEntry{
			{Type: "text", Text: `{"ok":true}`},
			{Type: "text", Text: "log_id: 20260519145706D6A0F7B0A114DB405B66"},
		},
	})

	got, err := unwrapResponse(raw)
	if err != nil {
		t.Fatalf("unwrapResponse failed: %v", err)
	}
	if got.LogID != "20260519145706D6A0F7B0A114DB405B66" {
		t.Errorf("expected stripped log_id, got %q", got.LogID)
	}
	// Data must NOT carry the log_id text entry — otherwise downstream
	// formatters would render it as part of the payload.
	dataMap, ok := got.Data.(map[string]any)
	if !ok || dataMap["ok"] != true {
		t.Errorf("expected Data to be the JSON object {ok:true}, got %#v", got.Data)
	}
}

func TestUnwrapResponse_ErrorCarriesLogIDWithoutAddingItToMessage(t *testing.T) {
	raw := mustMarshal(t, mcpToolResponse{
		Content: []mcpContentEntry{
			{Type: "text", Text: "backend rejected the request"},
			{Type: "text", Text: "log_id:trace-error-123"},
		},
		IsError: true,
	})
	_, err := unwrapResponse(raw)
	if err == nil {
		t.Fatal("expected backend error")
	}
	if got := meerrors.BackendLogID(err); got != "trace-error-123" {
		t.Fatalf("BackendLogID() = %q", got)
	}
	if got := err.Error(); got != "backend rejected the request" {
		t.Fatalf("error message = %q", got)
	}
}

// Historical alias: tolerate the older "logid:" prefix in case the server
// emits both shapes during a migration window.
func TestUnwrapResponse_AcceptsLegacyLogidPrefix(t *testing.T) {
	raw := mustMarshal(t, mcpToolResponse{
		Content: []mcpContentEntry{
			{Type: "text", Text: "logid:abc"},
			{Type: "text", Text: `{"ok":true}`},
		},
	})
	got, err := unwrapResponse(raw)
	if err != nil {
		t.Fatalf("unwrapResponse failed: %v", err)
	}
	if got.LogID != "abc" {
		t.Errorf("expected LogID=abc, got %q", got.LogID)
	}
}

func TestUnwrapResponse_NoLogIDLeavesFieldEmpty(t *testing.T) {
	raw := mustMarshal(t, mcpToolResponse{
		Content: []mcpContentEntry{
			{Type: "text", Text: `{"ok":true}`},
		},
	})
	got, err := unwrapResponse(raw)
	if err != nil {
		t.Fatalf("unwrapResponse failed: %v", err)
	}
	if got.LogID != "" {
		t.Errorf("expected empty LogID when server omits the entry, got %q", got.LogID)
	}
}

func TestUnwrapResponse_PlainTextFallbackUnchanged(t *testing.T) {
	raw := mustMarshal(t, mcpToolResponse{Content: []mcpContentEntry{{Type: "text", Text: "plain response"}}})
	got, err := unwrapResponse(raw)
	if err != nil {
		t.Fatalf("unwrapResponse: %v", err)
	}
	if got.Data != "plain response" || got.Raw != "plain response" {
		t.Fatalf("response = %#v, want unchanged plain-text fallback", got)
	}
}

func TestUnwrapResponsePreservesBackendRetryAdvice(t *testing.T) {
	for _, test := range []struct {
		text      string
		retryable bool
	}{
		{text: "error=MCPGatewayRequestMissing,message=argument arguments.view_id is required,retriable=false"},
		{text: "error=ErrWorkflowNotFound,message=Workflow Not Found,retriable=false"},
		{text: "error=ErrWorkingHourRecordCannotRecord,message=所属工作项尚未开启工时登记,retriable=false"},
		{text: "error=RPCRequestTimeout,message=The tool request timed out. Please try again.,retriable=true", retryable: true},
		{text: "error=RPCError,message=temporary failure,retriable=true", retryable: true},
		{text: "error=RPCError,message=embedded,retriable=false,details\nmore details,retriable=true", retryable: true},
		{text: "error=StableError,message=embedded,retriable=true,retriable=false"},
		{text: "error=RPCError,message=,retriable=true", retryable: true},
		{text: "error=RPCError,message=failed,retriable=TRUE"},
		{text: "error=RPCError,message=failed,retriable=true trailing text"},
		{text: "error=RPCError,message=failed,retriable=true\n"},
		{text: "error=RPCError,message=failed,retryable=true"},
		{text: "error=RPCError,message=failed"},
		{text: "error=RPCError,retriable=true"},
		{text: "error=,message=failed,retriable=true"},
		{text: "error=RPCError,other=value,message=failed,retriable=true"},
		{text: "ordinary error,retriable=true"},
		{text: "prefix error=RPCError,message=failed,retriable=true"},
		{text: "plain error"},
		{text: ""},
	} {
		t.Run(test.text, func(t *testing.T) {
			_, err := unwrapResponse(mustMarshal(t, mcpToolResponse{
				IsError: true,
				Content: []mcpContentEntry{{Type: "text", Text: test.text}, {Type: "text", Text: "log_id:trace-123"}},
			}))
			me, ok := err.(*meerrors.MeegleError)
			if !ok || me.ErrorPayload()["retryable"] != test.retryable {
				t.Fatalf("error = %#v, want retryable=%v", err, test.retryable)
			}
			if me.Code != "SERVER_CALL_FAILED" || me.ExitCode != 2 || me.LogID != "trace-123" {
				t.Fatalf("error contract changed: %#v", me)
			}
			wantMessage := test.text
			if wantMessage == "" {
				wantMessage = "server returned error"
			}
			if me.Message != wantMessage {
				t.Fatalf("message = %q, want %q", me.Message, wantMessage)
			}
		})
	}
}

func TestUnwrapResponseRetryAdviceUsesFirstContentOnly(t *testing.T) {
	const contract = "error=RPCError,message=failed,retriable=true"
	for _, content := range [][]mcpContentEntry{
		nil,
		{{Type: "image"}, {Type: "text", Text: contract}},
		{{Type: "text", Text: "log_id:trace-123"}, {Type: "text", Text: contract}},
		{{Type: "text", Text: "plain error"}, {Type: "text", Text: contract}},
	} {
		_, err := unwrapResponse(mustMarshal(t, mcpToolResponse{IsError: true, Content: content}))
		me, ok := err.(*meerrors.MeegleError)
		if !ok || me.ErrorPayload()["retryable"] != false {
			t.Fatalf("content = %#v, error = %#v; want false", content, err)
		}
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
