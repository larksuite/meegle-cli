// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tree     *CommandTree
	root     *CommandNode
	byPath   map[string]*CommandNode
	allNodes []*CommandNode
}

func New(tree *CommandTree) (*Registry, error) {
	if tree == nil {
		return nil, fmt.Errorf("command tree is nil")
	}
	result := ValidateTree(tree)
	if result.HasErrors() {
		return nil, result
	}

	root := &CommandNode{Name: "", childIndex: map[string]*CommandNode{}}
	reg := &Registry{
		tree:   tree,
		root:   root,
		byPath: make(map[string]*CommandNode),
	}

	for _, node := range tree.Nodes {
		if node == nil {
			continue
		}
		built, err := reg.buildNode(root, node, nil)
		if err != nil {
			return nil, err
		}
		root.Children = append(root.Children, built)
		for _, name := range allNames(built) {
			root.childIndex[name] = built
		}
	}
	return reg, nil
}

func (r *Registry) Root() *CommandNode {
	if r == nil {
		return nil
	}
	return r.root
}

func (r *Registry) Tree() *CommandTree {
	if r == nil {
		return nil
	}
	return r.tree
}

func (r *Registry) Resolve(args []string) (*CommandNode, []string) {
	if r == nil || r.root == nil {
		return nil, args
	}
	current := r.root
	consumed := 0
	for consumed < len(args) {
		token := args[consumed]
		if strings.HasPrefix(token, "-") {
			break
		}
		next, ok := current.childIndex[strings.ToLower(token)]
		if !ok {
			break
		}
		current = next
		consumed++
	}
	if current == r.root {
		return nil, args
	}
	return current, args[consumed:]
}

func (r *Registry) GetByPath(path string) *CommandNode {
	if r == nil {
		return nil
	}
	return r.byPath[normalizePath(path)]
}

func (r *Registry) Walk(fn func(node *CommandNode, depth int) bool) {
	if r == nil || fn == nil {
		return
	}
	var walk func(node *CommandNode, depth int) bool
	walk = func(node *CommandNode, depth int) bool {
		if node == nil {
			return true
		}
		if !fn(node, depth) {
			return false
		}
		for _, child := range node.Children {
			if !walk(child, depth+1) {
				return false
			}
		}
		return true
	}
	for _, child := range r.root.Children {
		if !walk(child, 0) {
			return
		}
	}
}

func (r *Registry) ExecutableCommands() []*CommandNode {
	if r == nil {
		return nil
	}
	out := make([]*CommandNode, 0)
	for _, node := range r.allNodes {
		if node.IsExecutable() {
			out = append(out, node)
		}
	}
	return out
}

func (r *Registry) AllFlagsForNode(node *CommandNode) []FlagDef {
	if node == nil {
		return append([]FlagDef(nil), BuiltinFlags...)
	}
	merged := make([]FlagDef, 0, len(BuiltinFlags)+len(r.tree.GlobalFlags)+len(node.InheritedFlags())+len(node.Flags))
	merged = append(merged, BuiltinFlags...)
	merged = append(merged, r.tree.GlobalFlags...)
	merged = append(merged, node.InheritedFlags()...)
	merged = append(merged, node.Flags...)
	return dedupFlags(merged)
}

func (r *Registry) buildNode(parent *CommandNode, source *CommandNode, fullPath []string) (*CommandNode, error) {
	path := append(append([]string(nil), fullPath...), source.Name)
	node := &CommandNode{
		Name:       source.Name,
		Aliases:    append([]string(nil), source.Aliases...),
		Help:       source.Help,
		Flags:      append([]FlagDef(nil), source.Flags...),
		Args:       append([]ArgDef(nil), source.Args...),
		HandlerRef: source.HandlerRef,
		Meta:       source.Meta,
		parent:     parent,
		fullPath:   path,
		childIndex: make(map[string]*CommandNode),
	}
	for _, child := range source.Children {
		builtChild, err := r.buildNode(node, child, path)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, builtChild)
		for _, name := range allNames(builtChild) {
			node.childIndex[name] = builtChild
		}
	}
	key := normalizePath(strings.Join(path, "."))
	r.byPath[key] = node
	r.allNodes = append(r.allNodes, node)
	return node, nil
}

func dedupFlags(flags []FlagDef) []FlagDef {
	indexByName := make(map[string]int)
	ordered := make([]FlagDef, 0, len(flags))
	for _, flag := range flags {
		key := strings.ToLower(flag.Name)
		if pos, ok := indexByName[key]; ok {
			ordered[pos] = flag
			continue
		}
		indexByName[key] = len(ordered)
		ordered = append(ordered, flag)
	}
	return ordered
}

func normalizePath(path string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '.' || r == '/' || r == ' '
	})
	return strings.ToLower(strings.Join(parts, "."))
}

func allNames(node *CommandNode) []string {
	out := []string{strings.ToLower(node.Name)}
	for _, alias := range node.Aliases {
		out = append(out, strings.ToLower(alias))
	}
	return out
}

func sortedVisibleChildren(node *CommandNode) []*CommandNode {
	children := node.VisibleChildren()
	sort.Slice(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})
	return children
}
