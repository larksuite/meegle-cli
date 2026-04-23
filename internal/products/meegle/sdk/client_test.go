// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"testing"
)

func TestNewClient_withDirectToken(t *testing.T) {
	client := NewClient(withDirectHost("example.com"), withDirectToken("test-token"))
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.mcp == nil {
		t.Fatal("expected non-nil mcp client")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_withDirectHeaders(t *testing.T) {
	client := NewClient(
		withDirectHost("example.com"),
		withDirectToken("test-token"),
		withDirectHeaders(map[string]string{"X-Custom": "value"}),
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromProfile_MissingProfile(t *testing.T) {
	// Non-existent profile should return error or empty client, not panic
	_, err := NewClientFromProfile("nonexistent-xxx")
	_ = err // may or may not error, just shouldn't panic
}

func TestNewClientWithMcpClient_Nil(t *testing.T) {
	// Passing nil should not panic
	client := NewClientWithMcpClient(nil)
	if client == nil {
		t.Fatal("expected non-nil client wrapper even with nil mcp")
	}
}
