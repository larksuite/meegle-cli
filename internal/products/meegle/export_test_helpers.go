// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// ExportMapToolsForTest exposes dynamic.MapTools for external tests.
func ExportMapToolsForTest(tools []types.ToolDefinition) []types.MappedCommand {
	return dynamic.MapTools(tools)
}

// ExportBuildCommandTreeForTest exposes buildCommandTree for external tests.
func ExportBuildCommandTreeForTest(commands []types.MappedCommand) *registry.CommandTree {
	return buildCommandTree(commands)
}
