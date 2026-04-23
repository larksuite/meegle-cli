// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package types

// MappedCommand is a ToolDefinition mapped to a CLI resource/method structure.
type MappedCommand struct {
	Resource    string
	Method      string
	ToolName    string
	Description string
	Parameters  []ToolParameter
	HasFields   bool
}
