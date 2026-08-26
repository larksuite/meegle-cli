// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"testing"
)

func TestCompositeSetupMergesTreesAndGlobalFlags(t *testing.T) {
	setup := NewCompositeSetup(
		NewStaticSetup(&CommandTree{
			Version:     "dynamic",
			GlobalFlags: []FlagDef{{Name: "profile", Type: FlagTypeString}},
			Nodes:       []*CommandNode{{Name: "workitem", HandlerRef: "tool.workitem"}},
		}),
		NewStaticSetup(&CommandTree{
			Version: "local",
			Nodes:   []*CommandNode{{Name: "ai-handoff", HandlerRef: "local.availability"}},
		}),
	)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if tree.Version != "dynamic+local" {
		t.Fatalf("Version = %q, want dynamic+local", tree.Version)
	}
	if len(tree.Nodes) != 2 || tree.Nodes[1].Name != "ai-handoff" {
		t.Fatalf("Nodes = %#v", tree.Nodes)
	}
	if len(tree.GlobalFlags) != 1 || tree.GlobalFlags[0].Name != "profile" {
		t.Fatalf("GlobalFlags = %#v", tree.GlobalFlags)
	}
}

func TestCompositeSetupRejectsDuplicateRootNamesAndAliases(t *testing.T) {
	setup := NewCompositeSetup(
		NewStaticSetup(&CommandTree{Nodes: []*CommandNode{{Name: "workitem", Aliases: []string{"wi"}}}}),
		NewStaticSetup(&CommandTree{Nodes: []*CommandNode{{Name: "WI"}}}),
	)

	if _, err := setup.Setup(context.Background()); err == nil {
		t.Fatal("Setup() error = nil, want duplicate root command error")
	}
}
