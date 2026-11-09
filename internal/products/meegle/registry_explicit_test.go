// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

func TestCommandRegistrySelectionAndSnapshots(t *testing.T) {
	commands := []CommandDefinition{{Command: types.MappedCommand{Resource: "workitem", Method: "get", ToolName: "sample_get", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer"}, {Name: "values", Type: "array", Items: &types.ParameterItems{Type: "string"}}}}, Result: RawResult}}
	bindings := []ShortcutBinding{{Kind: ShortcutBatchGet, ToolName: "sample_batch", Parameters: commands[0].Command.Parameters}, {Kind: ShortcutDownload, ToolName: "sample_download"}}
	setup := productRegistry(t, commands, bindings)
	commands[0].Command.ToolName = "mutated"
	commands[0].Command.Parameters[0].Name = "mutated"
	commands[0].Command.Parameters[1].Items.Type = "object"
	bindings[0].ToolName = "mutated"
	bindings[0].Parameters[0].Name = "mutated"
	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	get := findChild(findNodeByName(tree.Nodes, "workitem"), "get")
	batch := findChild(findNodeByName(tree.Nodes, "workitem"), "+batch-get")
	if get.HandlerRef != "sample_get" || batch.HandlerRef != "sample_batch" {
		t.Fatalf("bindings %s %s", get.HandlerRef, batch.HandlerRef)
	}
	if get.Flags[0].Name != "work-item-id" || get.Meta.Tags["mcp_param_items"] != `{"values":"string"}` {
		t.Fatalf("snapshot=%#v", get)
	}
	attachment := findNodeByName(tree.Nodes, "attachment")
	if len(attachment.Children) != 1 || attachment.Children[0].Name != "+download" {
		t.Fatalf("selection=%#v", attachment.Children)
	}
	attachment.Children[0].Flags[0].Name = "mutated"
	attachment.Children[0].Args[0].Name = "mutated"
	batch.Aliases[0] = "mutated"
	mapped := setup.MappedCommands()
	mapped[0].Parameters[1].Items.Type = "mutated"
	next, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	download := findChild(findNodeByName(next.Nodes, "attachment"), "+download")
	if download.Flags[0].Name != "project-key" || download.Args[0].Name != "file-url" {
		t.Fatal("tree mutation leaked")
	}
	if setup.MappedCommands()[0].Parameters[1].Items.Type != "string" {
		t.Fatal("mapped snapshot mutation leaked")
	}
	if findChild(findNodeByName(next.Nodes, "workitem"), "+batch-get").Aliases[0] != "+get-batch" {
		t.Fatal("alias mutation leaked")
	}
	empty := productRegistry(t, nil, nil)
	tree, err = empty.Setup(context.Background())
	if err != nil || len(tree.Nodes) != 0 {
		t.Fatalf("empty registry=%v %v", tree, err)
	}
}

func TestCommandRegistryInvalidDefinitions(t *testing.T) {
	valid := types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get"}
	for _, test := range []struct {
		name      string
		commands  []CommandDefinition
		shortcuts []ShortcutBinding
	}{
		{"path", []CommandDefinition{{Command: types.MappedCommand{Resource: "bad/path", Method: "get", ToolName: "sample_get"}}}, nil},
		{"empty tool", []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get"}}}, nil},
		{"reserved flag", []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get", Parameters: []types.ToolParameter{{Name: "format", Type: "string"}}}}}, nil},
		{"duplicate flag", []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "string"}, {Name: "work-item-id", Type: "string"}}}}}, nil},
		{"duplicate path", []CommandDefinition{{Command: valid}, {Command: valid}}, nil},
		{"result mode", []CommandDefinition{{Command: valid, Result: "invalid"}}, nil},
		{"missing dependency", nil, []ShortcutBinding{{Kind: ShortcutCurrentUser}}},
		{"unknown shortcut", nil, []ShortcutBinding{{Kind: "unknown", ToolName: "sample_get"}}},
		{"batch parameter", nil, []ShortcutBinding{{Kind: ShortcutBatchGet, ToolName: "sample_get"}}},
		{"shortcut result", nil, []ShortcutBinding{{Kind: ShortcutDownload, ToolName: "sample_get", Result: RawResult}}},
		{"shortcut duplicate", nil, []ShortcutBinding{{Kind: ShortcutCurrentUser, ToolName: "a"}, {Kind: ShortcutCurrentUser, ToolName: "b"}}},
		{"ordinary shortcut collision", []CommandDefinition{{Command: types.MappedCommand{Resource: "user", Method: "me", ToolName: "sample_user"}}}, []ShortcutBinding{{Kind: ShortcutCurrentUser, ToolName: "sample_user"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCommandRegistry(test.commands, test.shortcuts); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}

func TestCommandRegistryDefaultTreeUnchanged(t *testing.T) {
	// The existing mapping path still chooses the built-in scenario bindings.
	commands := []types.MappedCommand{{Resource: "workitem", Method: "get", ToolName: "get_work_item", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer"}}}, {Resource: "user", Method: "search", ToolName: "search_user_info"}}
	tree := buildCommandTree(commands)
	user := findChild(findNodeByName(tree.Nodes, "user"), "me")
	if user == nil || user.HandlerRef != "search_user_info" {
		t.Fatalf("default user binding=%#v", user)
	}
	batch := findChild(findNodeByName(tree.Nodes, "workitem"), "+batch-get")
	if batch == nil || batch.HandlerRef != "get_work_item" {
		t.Fatalf("default batch=%#v", batch)
	}
	if !strings.HasPrefix(batch.Help.Long, "Client-side fan-out over `meegle workitem get`, one call per ID.\n") {
		t.Fatalf("default batch help=%q", batch.Help.Long)
	}
	selected := productRegistry(t, []CommandDefinition{{Command: commands[0]}, {Command: commands[1]}}, nil)
	explicit, err := selected.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if findChild(findNodeByName(explicit.Nodes, "user"), "me") != nil || findChild(findNodeByName(explicit.Nodes, "workitem"), "+batch-get") != nil {
		t.Fatal("unselected shortcuts registered")
	}
	if !reflect.DeepEqual(findChild(findNodeByName(explicit.Nodes, "workitem"), "get").Flags, convertParameters(commands[0].Parameters)) {
		t.Fatal("ordinary schema changed")
	}
	if validation := registry.ValidateTree(explicit); validation.HasErrors() {
		t.Fatal(validation)
	}
}
