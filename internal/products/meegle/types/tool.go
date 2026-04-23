// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package types

// ParameterItems describes the element type for array parameters.
type ParameterItems struct {
	Type string `json:"type"`
}

// ToolParameter describes a single parameter of an MCP tool.
type ToolParameter struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Items       *ParameterItems `json:"items,omitempty"`
}

// ToolMetadata contains optional resource/method hints from the MCP server.
type ToolMetadata struct {
	Resource string `json:"resource,omitempty"`
	Method   string `json:"method,omitempty"`
}

// ToolDefinition represents an MCP tool as returned by tools/list.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  []ToolParameter `json:"parameters,omitempty"`
	Metadata    *ToolMetadata   `json:"metadata,omitempty"`
}
