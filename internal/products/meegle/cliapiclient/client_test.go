// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

func TestClientConfigDecodesGatewayEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != configPath {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer token-a" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Tt-Logid", "trace-config")
		_, _ = writer.Write([]byte(`{"code":0,"data":{"handoff_suggestion":{"available":true,"limits":{"max_query_bytes":20000,"max_related_context_items":20}}}}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Token: func() (string, error) { return "token-a", nil }})
	response, err := client.Config(context.Background())
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if !response.HandoffSuggestion.Available || response.HandoffSuggestion.RejectCode != "" || response.HandoffSuggestion.Limits.MaxRelatedContextItems != 20 {
		t.Fatalf("response = %#v", response)
	}
	if response.LogID != "trace-config" || response.HandoffSuggestion.LogID != "trace-config" {
		t.Fatalf("config logids = %q / %q", response.LogID, response.HandoffSuggestion.LogID)
	}
}

func TestClientCreateLinkSendsUserQueryAndTypedRelatedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != linksPath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["user_query"] != "summarize" {
			t.Fatalf("body = %#v", body)
		}
		contexts, ok := body["related_context"].([]any)
		if !ok || len(contexts) != 1 {
			t.Fatalf("related_context = %#v", body["related_context"])
		}
		contextItem, ok := contexts[0].(map[string]any)
		if !ok || contextItem["type"] != float64(ContextTypeWorkItem) {
			t.Fatalf("related_context item = %#v", contexts[0])
		}
		workItem, ok := contextItem["work_item"].(map[string]any)
		if !ok || workItem["work_item_id"] != "42" || workItem["project_key"] != "p" || workItem["work_item_type_key"] != "story" {
			t.Fatalf("work_item = %#v", contextItem["work_item"])
		}
		if _, exposed := workItem["key"]; exposed {
			t.Fatalf("facade request must not expose AI key: %#v", workItem)
		}
		writer.Header().Set("X-Tt-Logid", "trace-create")
		_, _ = writer.Write([]byte(`{"available":true,"expires_at":1787299200,"url":"https://example.com/agent?ctx_token=opaque"}`))
	}))
	defer server.Close()

	response, err := New(Config{BaseURL: server.URL}).CreateLink(context.Background(), CreateLinkRequest{
		UserQuery: "summarize",
		RelatedContext: []RelatedContext{{
			Type: ContextTypeWorkItem,
			WorkItem: &WorkItemContext{
				ProjectKey: "p", WorkItemTypeKey: "story", WorkItemID: "42",
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateLink() error = %v", err)
	}
	if !response.Available || response.ExpiresAt != 1787299200 || response.URL == "" {
		t.Fatalf("response = %#v", response)
	}
	if response.LogID != "trace-create" {
		t.Fatalf("create-link LogID = %q", response.LogID)
	}
}

func TestClientUpdatePreferenceSendsTypeAndPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != preferencePath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		preferences, ok := body["preferences"].([]any)
		if len(body) != 1 || !ok || len(preferences) != 1 {
			t.Fatalf("body = %#v", body)
		}
		item, ok := preferences[0].(map[string]any)
		if !ok || item["type"] != "handoff_suggestions" || item["payload"] != `{"mode":"off"}` {
			t.Fatalf("preference item = %#v", preferences[0])
		}
		writer.Header().Set("X-Tt-Logid", "trace-preference")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	response, err := client.UpdatePreference(context.Background(), "off")
	if err != nil {
		t.Fatalf("UpdatePreference() error = %v", err)
	}
	if !response.Success {
		t.Fatalf("response = %#v", response)
	}
	if response.LogID != "trace-preference" {
		t.Fatalf("preference LogID = %q", response.LogID)
	}
}

func TestClientRetriesOnceAfterUnauthorizedRefresh(t *testing.T) {
	token := "stale"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") == "Bearer stale" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL: server.URL,
		Token:   func() (string, error) { return token, nil },
		Refresh: func() error {
			token = "fresh"
			return nil
		},
	})
	response, err := client.UpdatePreference(context.Background(), "auto")
	if err != nil {
		t.Fatalf("GetPreference() error = %v", err)
	}
	if !response.Success || requests != 2 {
		t.Fatalf("response = %#v, requests = %d", response, requests)
	}
}

func TestDecodeResponseRejectsBusinessBaseResponseError(t *testing.T) {
	var target Availability
	err := decodeResponse([]byte(`{"available":false,"BaseResp":{"StatusCode":123,"StatusMessage":"denied"}}`), &target)
	if err == nil {
		t.Fatal("decodeResponse() error = nil")
	}
}

func TestClientMapsInvalidParamEnvelopeToNonRetryableError(t *testing.T) {
	tests := map[string]struct {
		responseBody string
		wantMessage  string
	}{
		"direct error code": {
			responseBody: `{"code":20006,"message":"Invalid Param"}`,
			wantMessage:  handoffInvalidParamMessage,
		},
		"gateway query limit wrapper": {
			responseBody: `{"code":50000,"msg":"biz error: id=1000050156, code=20006, message=Invalid Param, cause=[user_query exceeds 20000 bytes], chain=[facade.handler]"}`,
			wantMessage:  handoffQueryLimitMessage,
		},
		"gateway context limit wrapper": {
			responseBody: `{"code":50000,"msg":"biz error: id=1000050156, code=20006, message=Invalid Param, cause=[related_context exceeds 20 items], chain=[facade.handler]"}`,
			wantMessage:  handoffContextLimitMessage,
		},
		"base response": {
			responseBody: `{"BaseResp":{"StatusCode":20006,"StatusMessage":"Invalid Param"}}`,
			wantMessage:  handoffInvalidParamMessage,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.responseBody))
			}))
			defer server.Close()

			_, err := New(Config{BaseURL: server.URL}).CreateLink(context.Background(), CreateLinkRequest{UserQuery: "too large"})
			var meegleErr *meerrors.MeegleError
			if !errors.As(err, &meegleErr) {
				t.Fatalf("CreateLink() error = %v, want MeegleError", err)
			}
			if meegleErr.Code != "HANDOFF_API_INVALID_PARAM" || meegleErr.ExitCode != 1 {
				t.Fatalf("CreateLink() error = %#v", meegleErr)
			}
			payload := meegleErr.ErrorPayload()
			if payload["message"] != test.wantMessage || payload["suggestion"] != handoffInvalidParamHint {
				t.Fatalf("invalid parameter error payload = %#v", payload)
			}
			if retryable, _ := payload["retryable"].(bool); retryable {
				t.Fatalf("invalid parameter error must not be retryable: %#v", payload)
			}
			for _, internal := range []string{"1000050156", "code=20006", "cause=[", "chain=[", "facade.handler"} {
				if strings.Contains(meegleErr.Error(), internal) || strings.Contains(meegleErr.Suggestion, internal) {
					t.Fatalf("invalid parameter error leaks %q: %#v", internal, meegleErr)
				}
			}
		})
	}
}

func TestClientKeepsOtherEnvelopeErrorsAsServerResponseErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":50000,"msg":"internal dependency failed"}`))
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).CreateLink(context.Background(), CreateLinkRequest{UserQuery: "hello"})
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) {
		t.Fatalf("CreateLink() error = %v, want MeegleError", err)
	}
	if meegleErr.Code != "HANDOFF_API_INVALID_RESPONSE" || meegleErr.ExitCode != 2 {
		t.Fatalf("CreateLink() error = %#v", meegleErr)
	}
}

func TestHTTPErrorCarriesResponseLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Tt-Logid", "trace-500")
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, MaxRetries: -1})
	_, err := client.Config(context.Background())
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) {
		t.Fatalf("error = %T %v, want *MeegleError", err, err)
	}
	if meegleErr.LogID != "trace-500" {
		t.Fatalf("LogID = %q, want trace-500", meegleErr.LogID)
	}
}
