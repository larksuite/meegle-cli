// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package discovery

import (
	"context"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

// ToolLister is the interface for listing MCP tools.
type ToolLister interface {
	ListTools(ctx context.Context) ([]types.ToolDefinition, error)
}

// DiscoverTools fetches and returns all available tools from the MCP server.
func DiscoverTools(ctx context.Context, client ToolLister) ([]types.ToolDefinition, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	if tools == nil {
		return []types.ToolDefinition{}, nil
	}
	return tools, nil
}
