// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// productCommandTree validates explicit execution bindings and snapshots the
// mutable command metadata before the framework builds its per-app nodes.
func productCommandTree(tree *registry.CommandTree, handlers map[string]executor.Handler) (*registry.CommandTree, error) {
	if tree == nil {
		return nil, explicitRegistryError("command tree is nil")
	}
	// CompositeSetup already checks root names and aliases; ValidateTree checks
	// sibling paths deeper in the tree when the framework initializes it.
	if _, err := registry.NewCompositeSetup(registry.NewStaticSetup(tree)).Setup(context.Background()); err != nil {
		return nil, explicitRegistryError(err.Error())
	}
	snapshot := *tree
	snapshot.GlobalFlags = snapshotProductFlags(tree.GlobalFlags)
	var err error
	snapshot.Nodes, err = snapshotProductNodes(tree.Nodes, handlers)
	return &snapshot, err
}

func snapshotProductNodes(nodes []*registry.CommandNode, handlers map[string]executor.Handler) ([]*registry.CommandNode, error) {
	result := make([]*registry.CommandNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		kind := node.Meta.Tags[TagExecutorKind]
		if kind == ExecutorKindHandler {
			if strings.TrimSpace(node.HandlerRef) == "" || handlers[node.HandlerRef] == nil {
				return nil, explicitRegistryError(fmt.Sprintf("command %q references an unconfigured handler %q", node.Name, node.HandlerRef))
			}
		} else if kind != "" && kind != ExecutorKindMCP {
			return nil, runtimeResolutionError(kind)
		}
		copyNode := *node
		copyNode.Meta.Tags = cloneProductHeaders(node.Meta.Tags)
		copyNode.Flags = snapshotProductFlags(node.Flags)
		var err error
		copyNode.Children, err = snapshotProductNodes(node.Children, handlers)
		if err != nil {
			return nil, err
		}
		result = append(result, &copyNode)
	}
	return result, nil
}

func validateProductCommandPaths(root *cobra.Command) error {
	seen := make(map[string]bool)
	for _, child := range root.Commands() {
		for _, name := range append([]string{child.Name()}, child.Aliases...) {
			key := strings.ToLower(name)
			if seen[key] {
				return explicitRegistryError(fmt.Sprintf("duplicate command path %q under %q", name, root.CommandPath()))
			}
			seen[key] = true
		}
		if err := validateProductCommandPaths(child); err != nil {
			return err
		}
	}
	return nil
}

func snapshotProductFlags(flags []registry.FlagDef) []registry.FlagDef {
	snapshot := append([]registry.FlagDef(nil), flags...)
	for i := range snapshot {
		snapshot[i].Enum = append([]string(nil), flags[i].Enum...)
	}
	return snapshot
}
