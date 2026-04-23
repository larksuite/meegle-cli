// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import "strings"

const (
	FlagTypeString      = "string"
	FlagTypeInt         = "int"
	FlagTypeFloat       = "float"
	FlagTypeBool        = "bool"
	FlagTypeStringSlice = "string-slice"
	FlagTypeStringArray = "string-array"
	FlagTypeIntSlice    = "int-slice"
	FlagTypeObject      = "object"
	FlagTypeObjectSlice = "object-slice"
)

type CommandTree struct {
	Version     string
	GlobalFlags []FlagDef
	Nodes       []*CommandNode
}

type CommandNode struct {
	Name       string
	Aliases    []string
	Help       HelpText
	Flags      []FlagDef
	Args       []ArgDef
	Children   []*CommandNode
	HandlerRef string
	Meta       NodeMeta

	parent     *CommandNode
	childIndex map[string]*CommandNode
	fullPath   []string
}

type HelpText struct {
	Brief        string
	Long         string
	TemplateVars map[string]string
	Examples     []Example
}

type Example struct {
	Description string
	Command     string
}

type FlagDef struct {
	Name        string
	Short       string
	Type        string
	Required    bool
	Default     any
	Enum        []string
	Hidden      bool
	Description string
	Deprecated  string
}

type ArgDef struct {
	Name        string
	Required    bool
	Variadic    bool
	Description string
}

type NodeMeta struct {
	Hidden     bool
	Dangerous  bool
	Deprecated string
	Scope      string
	Tags       map[string]string
}

func (n *CommandNode) IsExecutable() bool {
	return n != nil && strings.TrimSpace(n.HandlerRef) != ""
}

func (n *CommandNode) IsGroup() bool {
	return n != nil && len(n.Children) > 0
}

func (n *CommandNode) FullPath() []string {
	if n == nil {
		return nil
	}
	out := make([]string, len(n.fullPath))
	copy(out, n.fullPath)
	return out
}

func (n *CommandNode) FullPathString() string {
	return strings.Join(n.fullPath, " ")
}

func (n *CommandNode) InheritedFlags() []FlagDef {
	if n == nil {
		return nil
	}
	var chain [][]FlagDef
	for parent := n.parent; parent != nil; parent = parent.parent {
		if len(parent.Flags) > 0 {
			chain = append(chain, parent.Flags)
		}
	}
	var out []FlagDef
	for index := len(chain) - 1; index >= 0; index-- {
		out = append(out, chain[index]...)
	}
	return out
}

func (n *CommandNode) VisibleChildren() []*CommandNode {
	if n == nil {
		return nil
	}
	out := make([]*CommandNode, 0, len(n.Children))
	for _, child := range n.Children {
		if child == nil || child.Meta.Hidden {
			continue
		}
		out = append(out, child)
	}
	return out
}
