// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"fmt"
	"net/http"
	"testing"
)

func TestCallbackServerSuccess(t *testing.T) {
	server, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer server.Close()
	state := "test-state-123"
	go func() {
		url := fmt.Sprintf("%s?code=auth-code-456&state=%s", server.RedirectURI, state)
		http.Get(url)
	}()
	result, err := server.WaitForCallback(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Code != "auth-code-456" {
		t.Errorf("expected auth-code-456, got %s", result.Code)
	}
}

func TestCallbackServerStateMismatch(t *testing.T) {
	server, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer server.Close()
	go func() {
		url := fmt.Sprintf("%s?code=code&state=wrong", server.RedirectURI)
		http.Get(url)
	}()
	_, err = server.WaitForCallback("expected-state")
	if err == nil {
		t.Error("expected error for state mismatch")
	}
}

func TestCallbackServerNoCode(t *testing.T) {
	server, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer server.Close()
	go func() {
		url := fmt.Sprintf("%s?state=s", server.RedirectURI)
		http.Get(url)
	}()
	_, err = server.WaitForCallback("s")
	if err == nil {
		t.Error("expected error for missing code")
	}
}
