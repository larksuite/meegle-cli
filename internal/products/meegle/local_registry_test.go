// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

func TestMeegleLocalSetupRegistersHandoffOutsideMCPDiscovery(t *testing.T) {
	tree, err := NewMeegleLocalSetup().Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	reg, err := registry.New(tree)
	if err != nil {
		t.Fatalf("registry.New() error = %v", err)
	}

	tests := map[string]string{
		"ai-handoff availability": ExecutorKindCLIAPI,
		"ai-handoff create-link":  ExecutorKindCLIAPI,
		"preference handoff auto": ExecutorKindCLIAPI,
		"preference handoff ask":  ExecutorKindCLIAPI,
		"preference handoff off":  ExecutorKindCLIAPI,
	}
	for path, kind := range tests {
		node := reg.GetByPath(path)
		if node == nil || !node.IsExecutable() {
			t.Fatalf("command %q is not executable", path)
		}
		if got := node.Meta.Tags[TagExecutorKind]; got != kind {
			t.Fatalf("command %q executor kind = %q, want %q", path, got, kind)
		}
	}
	if reg.GetByPath("preference handoff reset") != nil {
		t.Fatal("reset must not be exposed before atomic unset is available")
	}
	for _, oldPath := range []string{"ai-handoff +availability", "ai-handoff +create-link"} {
		if reg.GetByPath(oldPath) != nil {
			t.Fatalf("legacy plus-prefixed command %q must not be exposed", oldPath)
		}
	}

	createLink := reg.GetByPath("ai-handoff create-link")
	if len(createLink.Flags) != 2 || createLink.Flags[0].Name != "query" || !createLink.Flags[0].Required ||
		createLink.Flags[1].Name != "related-context" || createLink.Flags[1].Type != registry.FlagTypeStringArray {
		t.Fatalf("create-link flags = %#v", createLink.Flags)
	}
	if strings.TrimSpace(createLink.Help.Long) == "" || len(createLink.Help.Examples) < 2 {
		t.Fatalf("create-link help is incomplete: %#v", createLink.Help)
	}
	availability := reg.GetByPath("ai-handoff availability")
	if !strings.Contains(availability.Help.Long, "MEEGLE_AI_HANDOFF=disabled") ||
		!strings.Contains(createLink.Help.Long, "MEEGLE_AI_HANDOFF=disabled") {
		t.Fatal("handoff command help must document the local hard-disable")
	}
	if !strings.Contains(createLink.Help.Long, "current login host") {
		t.Fatal("create-link help must document the returned URL host rewrite")
	}
}

func TestLocalMappedCommandsDerivesNestedCommandsAndParameters(t *testing.T) {
	commands := localMappedCommands()
	byPath := make(map[string]int, len(commands))
	for index, command := range commands {
		byPath[command.Resource+" "+command.Method] = index
	}

	for _, path := range []string{
		"ai-handoff availability",
		"ai-handoff create-link",
		"preference handoff auto",
		"preference handoff ask",
		"preference handoff off",
	} {
		if _, ok := byPath[path]; !ok {
			t.Fatalf("inspect metadata missing %q: %#v", path, commands)
		}
	}

	createLink := commands[byPath["ai-handoff create-link"]]
	if len(createLink.Parameters) != 2 || createLink.Parameters[0].Name != "query" || !createLink.Parameters[0].Required {
		t.Fatalf("create-link inspect parameters = %#v", createLink.Parameters)
	}
	contextParam := createLink.Parameters[1]
	if contextParam.Name != "related-context" || contextParam.Type != "array" || contextParam.Items == nil || contextParam.Items.Type != "string" {
		t.Fatalf("related-context inspect parameter = %#v", contextParam)
	}
}
