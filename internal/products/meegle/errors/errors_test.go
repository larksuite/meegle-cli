// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientError(t *testing.T) {
	err := NewClientError("INVALID_PARAMS", "invalid parameter format")
	if err.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", err.ExitCode)
	}
	if err.Error() != "invalid parameter format" {
		t.Errorf("unexpected message: %s", err.Error())
	}

	data, _ := json.Marshal(err)
	var m map[string]string
	json.Unmarshal(data, &m)
	if m["error"] != "INVALID_PARAMS" {
		t.Errorf("expected error code INVALID_PARAMS, got %s", m["error"])
	}
}

func TestServerError(t *testing.T) {
	err := NewServerError("SERVER_CALL_FAILED", "server returned error")
	if err.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %d", err.ExitCode)
	}
}

func TestErrorWithSuggestion(t *testing.T) {
	err := NewClientError("AUTH_EXPIRED", "authentication expired").WithSuggestion("meegle auth login")
	data, _ := json.Marshal(err)
	var m map[string]string
	json.Unmarshal(data, &m)
	if m["suggestion"] != "meegle auth login" {
		t.Errorf("expected suggestion, got %s", m["suggestion"])
	}
}

func TestErrorWithoutSuggestion(t *testing.T) {
	err := NewClientError("FAIL", "failed")
	data, _ := json.Marshal(err)
	s := string(data)
	if strings.Contains(s, "suggestion") {
		t.Error("suggestion should be omitted when empty")
	}
}

func TestErrorPayload_ClientErrorIncludesSuggestion(t *testing.T) {
	err := NewClientError("AUTH_EXPIRED", "authentication expired").
		WithSuggestion("meegle auth login")
	rec := err.ErrorPayload()
	if rec["code"] != "AUTH_EXPIRED" {
		t.Fatalf("code=%v", rec["code"])
	}
	if rec["message"] != "authentication expired" {
		t.Fatalf("message=%v", rec["message"])
	}
	if rec["retryable"] != false {
		t.Fatalf("client error must not be retryable: %v", rec["retryable"])
	}
	if rec["suggestion"] != "meegle auth login" {
		t.Fatalf("suggestion lost: %v", rec["suggestion"])
	}
}

func TestErrorPayload_ServerErrorIsRetryable(t *testing.T) {
	rec := NewServerError("BACKEND_5XX", "upstream 503").ErrorPayload()
	if rec["retryable"] != true {
		t.Fatalf("server error must be retryable, got %v", rec["retryable"])
	}
	if _, has := rec["suggestion"]; has {
		t.Fatalf("no suggestion expected, got %v", rec["suggestion"])
	}
}

func TestErrorPayload_NilReceiver(t *testing.T) {
	var e *MeegleError
	if rec := e.ErrorPayload(); rec != nil {
		t.Fatalf("nil receiver should return nil map, got %v", rec)
	}
}
