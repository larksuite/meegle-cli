// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package discovery

import (
	"context"
	"fmt"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

type mockClient struct {
	tools []types.ToolDefinition
	err   error
}

func (m *mockClient) ListTools(ctx context.Context) ([]types.ToolDefinition, error) {
	return m.tools, m.err
}

func TestDiscoverTools(t *testing.T) {
	client := &mockClient{
		tools: []types.ToolDefinition{
			{Name: "create_workitem", Description: "Create a work item",
				Parameters: []types.ToolParameter{{Name: "project_key", Type: "string", Required: true}}},
		},
	}
	tools, err := DiscoverTools(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "create_workitem" {
		t.Errorf("expected create_workitem, got %s", tools[0].Name)
	}
}

func TestDiscoverToolsEmpty(t *testing.T) {
	client := &mockClient{tools: nil}
	tools, err := DiscoverTools(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestDiscoverToolsError(t *testing.T) {
	client := &mockClient{err: fmt.Errorf("connection failed")}
	_, err := DiscoverTools(context.Background(), client)
	if err == nil {
		t.Error("expected error")
	}
}
