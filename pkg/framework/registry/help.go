// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"fmt"
	"strings"
)

type HelpGenerator struct {
	AppName     string
	GlobalFlags []FlagDef
}

// Render produces a complete help text for any node regardless of depth.
// The output follows the canonical layout:
//
//	<brief>
//	<blank line>
//	<long description>
//	<blank line>
//	Usage:
//	  <path> [command|flags] [args]
//	<blank line>
//	Available Commands:   (if group)
//	  <name>  <brief>
//	<blank line>
//	Args:                 (if leaf with positional args)
//	  <name>  <description>
//	<blank line>
//	Flags:
//	  --<name>   <description>
//	<blank line>
//	Examples:             (if any)
//	  # <description>
//	  <command>
//	<blank line>
//	Use "<path> [command] --help" for more information about a command.
func (g *HelpGenerator) Render(node *CommandNode) string {
	return g.RenderFiltered(node, nil)
}

// RenderFiltered renders help while allowing the final command tree to hide
// additional immediate children (for example, enterprise policy decisions
// applied after the registry was built).
func (g *HelpGenerator) RenderFiltered(node *CommandNode, include func(*CommandNode) bool) string {
	if node == nil {
		return ""
	}
	var sections []string

	// Brief + long description
	if brief := strings.TrimSpace(node.Help.Brief); brief != "" {
		sections = append(sections, brief)
	}
	if long := strings.TrimSpace(node.Help.Long); long != "" {
		sections = append(sections, long)
	}

	// Usage line
	path := node.FullPathString()
	if path == "" {
		path = node.Name
	}
	if appName := strings.TrimSpace(g.AppName); appName != "" {
		path = appName + " " + path
	}
	visible := filteredVisibleChildren(node, include)
	usage := buildUsageLine(path, node, len(visible) > 0)
	sections = append(sections, "Usage:\n  "+usage)

	// Available Commands
	if len(visible) > 0 {
		lines := []string{"Available Commands:"}
		for _, child := range visible {
			lines = append(lines, fmt.Sprintf("  %-20s %s", child.Name, strings.TrimSpace(child.Help.Brief)))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// Args (for leaf/executable nodes)
	if node.IsExecutable() && len(node.Args) > 0 {
		lines := []string{"Args:"}
		for _, arg := range node.Args {
			marker := arg.Name
			if arg.Required {
				marker = "<" + marker + ">"
			} else {
				marker = "[" + marker + "]"
			}
			if arg.Variadic {
				marker += "..."
			}
			desc := strings.TrimSpace(arg.Description)
			if desc == "" {
				desc = arg.Name
			}
			lines = append(lines, fmt.Sprintf("  %-20s %s", marker, desc))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// Local flags
	localFlags := node.Flags
	if len(localFlags) > 0 {
		lines := []string{"Flags:"}
		for _, flag := range localFlags {
			if flag.Hidden {
				continue
			}
			lines = append(lines, formatFlagLine(flag))
		}
		if len(lines) > 1 {
			sections = append(sections, strings.Join(lines, "\n"))
		}
	}

	// Global / inherited flags (builtin + tree-level)
	allGlobal := make([]FlagDef, 0, len(BuiltinFlags)+len(g.GlobalFlags))
	allGlobal = append(allGlobal, BuiltinFlags...)
	allGlobal = append(allGlobal, g.GlobalFlags...)
	allGlobal = append(allGlobal, node.InheritedFlags()...)
	if len(allGlobal) > 0 {
		lines := []string{"Global Flags:"}
		for _, flag := range allGlobal {
			if flag.Hidden {
				continue
			}
			lines = append(lines, formatFlagLine(flag))
		}
		if len(lines) > 1 {
			sections = append(sections, strings.Join(lines, "\n"))
		}
	}

	// Examples
	if len(node.Help.Examples) > 0 {
		lines := []string{"Examples:"}
		for _, ex := range node.Help.Examples {
			if desc := strings.TrimSpace(ex.Description); desc != "" {
				lines = append(lines, "  # "+desc)
			}
			if cmd := strings.TrimSpace(ex.Command); cmd != "" {
				lines = append(lines, "  "+cmd)
			}
		}
		if len(lines) > 1 {
			sections = append(sections, strings.Join(lines, "\n"))
		}
	}

	// Drill-down hint for groups
	if len(visible) > 0 {
		sections = append(sections, fmt.Sprintf("Use \"%s [command] --help\" for more information about a command.", path))
	}

	return strings.Join(sections, "\n\n")
}

func filteredVisibleChildren(node *CommandNode, include func(*CommandNode) bool) []*CommandNode {
	children := sortedVisibleChildren(node)
	if include == nil {
		return children
	}
	filtered := children[:0]
	for _, child := range children {
		if include(child) {
			filtered = append(filtered, child)
		}
	}
	return filtered
}

func buildUsageLine(path string, node *CommandNode, hasVisibleChildren bool) string {
	parts := []string{path}
	if hasVisibleChildren {
		parts = append(parts, "[command]")
	}
	for _, arg := range node.Args {
		name := arg.Name
		if arg.Variadic {
			name += "..."
		}
		if arg.Required {
			parts = append(parts, "<"+name+">")
		} else {
			parts = append(parts, "["+name+"]")
		}
	}
	if node.IsExecutable() || len(node.Flags) > 0 {
		parts = append(parts, "[flags]")
	}
	return strings.Join(parts, " ")
}

func formatFlagLine(flag FlagDef) string {
	name := "--" + flag.Name
	if flag.Short != "" {
		name = "-" + flag.Short + ", " + name
	}
	suffix := ""
	if flag.Type != FlagTypeBool {
		suffix = " " + flag.Type
	}
	desc := strings.TrimSpace(flag.Description)
	if flag.Required {
		if desc != "" {
			desc += " "
		}
		desc += "(required)"
	}
	if len(flag.Enum) > 0 {
		desc += fmt.Sprintf(" (%s)", strings.Join(flag.Enum, "|"))
	}
	if flag.Default != nil && flag.Default != "" && flag.Default != false {
		desc += fmt.Sprintf(" (default %v)", flag.Default)
	}
	if flag.Deprecated != "" {
		desc += " [deprecated: " + flag.Deprecated + "]"
	}
	return fmt.Sprintf("  %-24s %s", name+suffix, desc)
}
