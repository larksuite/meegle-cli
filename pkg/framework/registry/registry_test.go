// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import "testing"

func TestRegistryResolveWithAliasAndFlags(t *testing.T) {
	tree := &CommandTree{
		Nodes: []*CommandNode{
			{
				Name: "workitem",
				Help: HelpText{Brief: "Manage work items"},
				Children: []*CommandNode{{
					Name:       "search",
					Help:       HelpText{Brief: "Search work items"},
					Aliases:    []string{"s"},
					HandlerRef: "core.workitem.search",
				}},
			},
		},
	}
	reg, err := New(tree)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	node, remaining := reg.Resolve([]string{"workitem", "s", "--query", "test"})
	if node == nil {
		t.Fatal("expected resolved node")
	}
	if node.Name != "search" {
		t.Fatalf("resolved node = %s", node.Name)
	}
	if len(remaining) != 2 || remaining[0] != "--query" {
		t.Fatalf("unexpected remaining args: %#v", remaining)
	}
}

func TestRegistryRejectsBuiltinFlagConflicts(t *testing.T) {
	tree := &CommandTree{
		Nodes: []*CommandNode{{
			Name:       "workitem",
			Help:       HelpText{Brief: "Manage work items"},
			HandlerRef: "root.handler",
			Flags:      []FlagDef{{Name: "format", Type: FlagTypeString}},
		}},
	}
	if _, err := New(tree); err == nil {
		t.Fatal("expected validation error")
	}
}
