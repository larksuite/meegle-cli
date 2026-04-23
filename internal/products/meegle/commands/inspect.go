// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

func NewInspectCmd(mappedCommands []types.MappedCommand) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect [command]",
		Short: "Show parameter details for a command (e.g. inspect workitem.create)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return inspectListAll(mappedCommands)
			}
			return inspectSingle(mappedCommands, args[0])
		},
	}
}

// inspectListAll lists all registered dynamic commands grouped by resource.
func inspectListAll(mappedCommands []types.MappedCommand) error {
	if len(mappedCommands) == 0 {
		fmt.Println("No commands available (not connected to server or cache is empty)")
		return nil
	}

	// Collect resources in order of first appearance
	var order []string
	groups := make(map[string][]int)
	for i, c := range mappedCommands {
		if _, exists := groups[c.Resource]; !exists {
			order = append(order, c.Resource)
		}
		groups[c.Resource] = append(groups[c.Resource], i)
	}

	// Compute max method name width for alignment
	maxWidth := 0
	for _, c := range mappedCommands {
		if len(c.Method) > maxWidth {
			maxWidth = len(c.Method)
		}
	}

	for i, resource := range order {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(resource)
		for _, idx := range groups[resource] {
			c := mappedCommands[idx]
			fmt.Printf("  %-*s  %s\n", maxWidth, c.Method, c.Description)
		}
	}
	return nil
}

// inspectSingle shows detailed parameter info for a single command.
func inspectSingle(mappedCommands []types.MappedCommand, name string) error {
	var resource, method string
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		resource, method = parts[0], parts[1]
	} else if strings.Contains(name, " ") {
		parts := strings.SplitN(name, " ", 2)
		resource, method = parts[0], parts[1]
	} else {
		return fmt.Errorf("command not found: %s\nRun meegle inspect to see all available commands", name)
	}

	for _, c := range mappedCommands {
		if c.Resource == resource && c.Method == method {
			return printCommandDetail(c)
		}
	}

	return fmt.Errorf("command not found: %s\nRun meegle inspect to see all available commands", name)
}

// formatParamTypeLabel returns a human-readable type label for a parameter.
func formatParamTypeLabel(p types.ToolParameter) string {
	if p.Type == "array" && p.Items != nil && p.Items.Type != "" {
		return p.Items.Type + "[]"
	}
	return p.Type
}

// printCommandDetail displays detailed parameter information for a single command.
func printCommandDetail(cmd types.MappedCommand) error {
	type paramInfo struct {
		flag     string
		typeStr  string
		required bool
		desc     string
	}

	var params []paramInfo
	for _, p := range cmd.Parameters {
		if p.Name == "url" {
			continue
		}
		params = append(params, paramInfo{
			flag:     "--" + strings.ReplaceAll(p.Name, "_", "-"),
			typeStr:  formatParamTypeLabel(p),
			required: p.Required,
			desc:     p.Description,
		})
	}

	if cmd.HasFields {
		params = append(params, paramInfo{
			flag:    "--set",
			typeStr: "key=value",
			desc:    "Set a field as key=value (repeatable, value supports JSON)",
		})
	}

	params = append(params, paramInfo{
		flag:    "--params",
		typeStr: "json",
		desc:    "Full JSON parameters (--set and flags override matching fields)",
	})

	// Print header
	fmt.Printf("\n  meegle %s %s\n", cmd.Resource, cmd.Method)
	fmt.Printf("  %s\n\n", cmd.Description)

	// Split required / optional
	var required, optional []paramInfo
	for _, p := range params {
		if p.required {
			required = append(required, p)
		} else {
			optional = append(optional, p)
		}
	}

	if len(required) > 0 {
		fmt.Println("  Required parameters:")
		for _, p := range required {
			fmt.Printf("    %s <%s>\n", p.flag, p.typeStr)
			if p.desc != "" {
				fmt.Printf("      %s\n", p.desc)
			}
		}
		fmt.Println()
	}

	if len(optional) > 0 {
		fmt.Println("  Optional parameters:")
		for _, p := range optional {
			fmt.Printf("    %s <%s>\n", p.flag, p.typeStr)
			if p.desc != "" {
				fmt.Printf("      %s\n", p.desc)
			}
		}
		fmt.Println()
	}

	return nil
}
