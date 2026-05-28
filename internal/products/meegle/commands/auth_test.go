// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

// jsonRPCEnvelope is a minimal local mirror of the unexported envelope inside
// the mcpclient package — duplicated here because the field set is small and
// stable, and we do not want to export internal RPC types just for tests.
type jsonRPCEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// listToolsHandler returns an HTTP handler that responds to tools/list with
// the canonical empty-tools envelope. Used by the "server accepts the token"
// case where we only care that the call returns nil error.
func listToolsHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		resp := jsonRPCEnvelope{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[]}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// TestVerifyMCPClient_TokenValid pins that a 200 from tools/list resolves to
// the empty success reason — the only state that yields authenticated:true.
func TestVerifyMCPClient_TokenValid(t *testing.T) {
	server := httptest.NewServer(listToolsHandler(t))
	defer server.Close()

	client := mcpclient.New(server.URL)
	if reason := verifyMCPClient(context.Background(), client); reason != "" {
		t.Errorf("expected empty reason on 200, got %q", reason)
	}
}

// TestVerifyMCPClient_TokenRejected pins that a 401 maps to the
// token-rejected reason — exit code 1 territory, user needs auth login.
func TestVerifyMCPClient_TokenRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := mcpclient.New(server.URL)
	reason := verifyMCPClient(context.Background(), client)
	if reason != statusReasonTokenRejected {
		t.Errorf("expected %q, got %q", statusReasonTokenRejected, reason)
	}
}

// TestVerifyMCPClient_TokenRejectedAfterRefreshFailure pins the store-token
// path: mcpclient refreshes after 401, then returns AUTH_EXPIRED when refresh
// fails. auth status must still classify that as exit-code-1 auth rejection,
// not retryable server-unreachable.
func TestVerifyMCPClient_TokenRejectedAfterRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := mcpclient.New(server.URL, mcpclient.WithRefreshFunc(func() error {
		return stderrors.New("refresh failed")
	}))
	reason := verifyMCPClient(context.Background(), client)
	if reason != statusReasonTokenRejected {
		t.Errorf("expected %q, got %q", statusReasonTokenRejected, reason)
	}
}

// TestVerifyMCPClient_ServerUnreachable_5xx pins that a 5xx (server up but
// failing) maps to the retryable "server unreachable" reason — exit code 2
// territory, cron should retry rather than re-login.
func TestVerifyMCPClient_ServerUnreachable_5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := mcpclient.New(server.URL)
	reason := verifyMCPClient(context.Background(), client)
	if !strings.HasPrefix(reason, statusReasonServerUnreachable) {
		t.Errorf("expected reason to start with %q, got %q", statusReasonServerUnreachable, reason)
	}
}

// TestVerifyMCPClient_ServerUnreachable_ConnRefused pins that a dead host
// (no listener) still resolves to the retryable category, not the rejected
// category — connection refused is a network-layer signal, not an auth one.
func TestVerifyMCPClient_ServerUnreachable_ConnRefused(t *testing.T) {
	server := httptest.NewServer(listToolsHandler(t))
	url := server.URL
	server.Close() // shut down before the request to provoke connection refused

	client := mcpclient.New(url)
	reason := verifyMCPClient(context.Background(), client)
	if !strings.HasPrefix(reason, statusReasonServerUnreachable) {
		t.Errorf("expected reason to start with %q, got %q", statusReasonServerUnreachable, reason)
	}
	if reason == statusReasonTokenRejected {
		t.Errorf("network failure must not be misclassified as token rejection")
	}
}

// TestStatusExitCode covers the full reason → exit code mapping in one
// place. The map is small enough that table-driven is overkill in size but
// captures the contract cleanly for future reviewers.
func TestStatusExitCode(t *testing.T) {
	cases := []struct {
		name string
		r    StatusResult
		want int
	}{
		{"authenticated", StatusResult{Authenticated: true}, 0},
		{"no local token", StatusResult{Reason: statusReasonNoLocalToken}, 1},
		{"token rejected", StatusResult{Reason: statusReasonTokenRejected}, 1},
		{"server unreachable", StatusResult{Reason: statusReasonServerUnreachable + ": dial tcp: connection refused"}, 2},
		{"server unreachable 5xx", StatusResult{Reason: statusReasonServerUnreachable + ": server returned error (500)"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusExitCode(&tc.r); got != tc.want {
				t.Errorf("statusExitCode(%+v) = %d, want %d", tc.r, got, tc.want)
			}
		})
	}
}
