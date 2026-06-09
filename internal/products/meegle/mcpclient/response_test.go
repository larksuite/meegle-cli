// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"encoding/json"
	"testing"
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

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
