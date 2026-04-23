// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// Optional `+` prefix marks scenario / client-side sugar commands that have
// no 1:1 MCP tool behind them (e.g. `workitem +batch-get` fans out over the
// real `workitem get`). The rest of the name still follows the
// `[a-z][a-z0-9-]*` shape so paths stay predictable in shell completion.
var commandNamePattern = regexp.MustCompile(`^\+?[a-z][a-z0-9-]*$`)

// protocolKeywords are terms that must not appear in user-facing text
// (help, brief, examples) in pipeline/ and output/ layers.
var protocolKeywords = []string{"mcp", "json-rpc", "json_rpc", "tool_call", "tools/list", "tools/call"}

type ValidationError struct {
	Path    string
	Field   string
	Rule    string
	Level   string
	Message string
}

type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError
}

func (r *ValidationResult) HasErrors() bool {
	return r != nil && len(r.Errors) > 0
}

func (r *ValidationResult) Error() string {
	if r == nil {
		return ""
	}
	parts := make([]string, 0, len(r.Errors))
	for _, item := range r.Errors {
		parts = append(parts, fmt.Sprintf("[%s] %s", item.Rule, item.Message))
	}
	return strings.Join(parts, "; ")
}

// ValidateTree runs all validation rules (R1–R14) against a CommandTree.
// Rules R1-R10, R13 produce errors; R11, R12, R14 produce warnings.
func ValidateTree(tree *CommandTree) *ValidationResult {
	result := &ValidationResult{}
	if tree == nil {
		result.Errors = append(result.Errors, ValidationError{Rule: "TREE_NIL", Level: "error", Message: "command tree is nil"})
		return result
	}
	// Collect all handler refs for R12 (handler_reuse)
	handlerCount := make(map[string]int)
	countHandlers(tree.Nodes, handlerCount)

	for _, node := range tree.Nodes {
		validateNode(result, node, nil, handlerCount)
	}
	return result
}

func countHandlers(nodes []*CommandNode, counts map[string]int) {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if ref := strings.TrimSpace(node.HandlerRef); ref != "" {
			counts[ref]++
		}
		countHandlers(node.Children, counts)
	}
}

func validateNode(result *ValidationResult, node *CommandNode, path []string, handlerCount map[string]int) {
	if node == nil {
		return
	}
	currentPath := append(append([]string(nil), path...), node.Name)
	pathString := strings.Join(currentPath, ".")

	// R2: name_format
	if strings.TrimSpace(node.Name) == "" {
		result.Errors = append(result.Errors, ValidationError{Path: pathString, Field: "name", Rule: "R_NAME_REQUIRED", Level: "error", Message: "node name is required"})
	} else if !commandNamePattern.MatchString(node.Name) {
		result.Errors = append(result.Errors, ValidationError{Path: pathString, Field: "name", Rule: "R_NAME_FORMAT", Level: "error", Message: fmt.Sprintf("invalid node name %q", node.Name)})
	}

	// R3: dead_end
	if !node.IsExecutable() && len(node.Children) == 0 {
		result.Errors = append(result.Errors, ValidationError{Path: pathString, Field: "handler_ref", Rule: "R_DEAD_END", Level: "error", Message: "node must be executable or have children"})
	}

	// R4: missing_brief
	if strings.TrimSpace(node.Help.Brief) == "" {
		result.Errors = append(result.Errors, ValidationError{Path: pathString, Field: "help.brief", Rule: "R_MISSING_BRIEF", Level: "error", Message: fmt.Sprintf("node %q is missing help.brief", node.Name)})
	}

	// R11: unknown_handler (warn – we can only check if HandlerRef is non-empty; deeper
	// cross-reference validation requires a handler registry and is done at assembly time)
	// R12: handler_reuse
	if ref := strings.TrimSpace(node.HandlerRef); ref != "" && handlerCount[ref] > 1 {
		result.Warnings = append(result.Warnings, ValidationError{Path: pathString, Field: "handler_ref", Rule: "R_HANDLER_REUSE", Level: "warning", Message: fmt.Sprintf("handler_ref %q is referenced by multiple nodes", ref)})
	}

	// R14: protocol_leak in user-facing text
	for _, text := range []string{node.Help.Brief, node.Help.Long} {
		lower := strings.ToLower(text)
		for _, kw := range protocolKeywords {
			if strings.Contains(lower, kw) {
				result.Warnings = append(result.Warnings, ValidationError{Path: pathString, Field: "help", Rule: "R_PROTOCOL_LEAK", Level: "warning", Message: fmt.Sprintf("help text contains protocol keyword %q", kw)})
				break
			}
		}
	}
	for _, ex := range node.Help.Examples {
		lower := strings.ToLower(ex.Description + " " + ex.Command)
		for _, kw := range protocolKeywords {
			if strings.Contains(lower, kw) {
				result.Warnings = append(result.Warnings, ValidationError{Path: pathString, Field: "help.examples", Rule: "R_PROTOCOL_LEAK", Level: "warning", Message: fmt.Sprintf("example text contains protocol keyword %q", kw)})
				break
			}
		}
	}

	validateFlags(result, pathString, node.Flags)
	validateArgs(result, pathString, node.Args)
	validateChildren(result, pathString, node.Children)
	for _, child := range node.Children {
		validateNode(result, child, currentPath, handlerCount)
	}
}

func validateFlags(result *ValidationResult, path string, flags []FlagDef) {
	seenNames := make(map[string]struct{})
	seenShorts := make(map[string]struct{})
	builtin := make(map[string]struct{})
	for _, flag := range BuiltinFlags {
		builtin[strings.ToLower(flag.Name)] = struct{}{}
	}
	for _, flag := range flags {
		nameKey := strings.ToLower(flag.Name)
		// R7: dup_flag
		if _, exists := seenNames[nameKey]; exists {
			result.Errors = append(result.Errors, ValidationError{Path: path, Field: flag.Name, Rule: "R_DUP_FLAG", Level: "error", Message: fmt.Sprintf("duplicate flag %q", flag.Name)})
		}
		seenNames[nameKey] = struct{}{}
		// R13: builtin_flag_conflict
		if _, exists := builtin[nameKey]; exists {
			result.Errors = append(result.Errors, ValidationError{Path: path, Field: flag.Name, Rule: "R_BUILTIN_FLAG_CONFLICT", Level: "error", Message: fmt.Sprintf("flag %q conflicts with builtin flag", flag.Name)})
		}
		// R9: required_with_default
		if flag.Required && flag.Default != nil {
			result.Errors = append(result.Errors, ValidationError{Path: path, Field: flag.Name, Rule: "R_REQUIRED_WITH_DEFAULT", Level: "error", Message: fmt.Sprintf("required flag %q cannot have default", flag.Name)})
		}
		// R8: short_conflict
		if strings.TrimSpace(flag.Short) != "" {
			shortKey := strings.ToLower(flag.Short)
			if _, exists := seenShorts[shortKey]; exists {
				result.Errors = append(result.Errors, ValidationError{Path: path, Field: flag.Name, Rule: "R_SHORT_CONFLICT", Level: "error", Message: fmt.Sprintf("short flag -%s conflicts", flag.Short)})
			}
			seenShorts[shortKey] = struct{}{}
		}
	}
}

func validateArgs(result *ValidationResult, path string, args []ArgDef) {
	// R10: variadic_position
	for index, arg := range args {
		if arg.Variadic && index != len(args)-1 {
			result.Errors = append(result.Errors, ValidationError{Path: path, Field: arg.Name, Rule: "R_VARIADIC_POSITION", Level: "error", Message: fmt.Sprintf("variadic arg %q must be last", arg.Name)})
		}
	}
}

func validateChildren(result *ValidationResult, path string, children []*CommandNode) {
	// R5: dup_child, R6: alias_conflict
	seen := make(map[string]struct{})
	for _, child := range children {
		if child == nil {
			continue
		}
		for _, name := range append([]string{child.Name}, child.Aliases...) {
			key := strings.ToLower(name)
			if _, exists := seen[key]; exists {
				result.Errors = append(result.Errors, ValidationError{Path: path, Field: name, Rule: "R_CHILD_CONFLICT", Level: "error", Message: fmt.Sprintf("child name or alias %q conflicts with sibling", name)})
			}
			seen[key] = struct{}{}
		}
	}
}
