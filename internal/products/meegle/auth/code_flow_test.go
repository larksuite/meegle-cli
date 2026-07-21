// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestExchangeTokenRejectsSuccessResponseWithoutAccessToken(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "business error envelope",
			body: `{"code":10021,"error":{"id":1000050646},"data":{}}`,
		},
		{
			name: "explicit empty token",
			body: `{"access_token":"","refresh_token":"still-present"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := exchangeToken(context.Background(), server.URL, url.Values{"grant_type": {"refresh_token"}})
			if err == nil {
				t.Fatal("expected missing access_token to fail")
			}
			if !strings.Contains(err.Error(), "access_token") {
				t.Fatalf("expected access_token validation error, got %v", err)
			}
		})
	}
}

func TestExchangeTokenAcceptsValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer server.Close()

	got, err := exchangeToken(context.Background(), server.URL, url.Values{"grant_type": {"refresh_token"}})
	if err != nil {
		t.Fatalf("exchangeToken failed: %v", err)
	}
	if got.AccessToken != "fresh" || got.RefreshToken != "refresh" || got.ExpiresIn != 3600 {
		t.Fatalf("unexpected token response: %+v", got)
	}
}
