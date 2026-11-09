// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import "testing"

func TestLiteralCommandNameRequiresOptIn(t *testing.T) {
	for _, name := range []string{"Report", "export_csv", "Export_CSV-v2"} {
		for _, optedIn := range []bool{false, true} {
			node := &CommandNode{Name: name, HandlerRef: "handler", Help: HelpText{Brief: "Export data"}}
			if optedIn {
				node.Meta.Tags = map[string]string{TagLiteralCommandName: "true"}
			}
			result := ValidateTree(&CommandTree{Nodes: []*CommandNode{node}})
			if result.HasErrors() == optedIn {
				t.Fatalf("name=%q opt-in=%v: %v", name, optedIn, result)
			}
		}
	}
}

func TestLiteralCommandNameStillRejectsUnsafeNamesAndConflicts(t *testing.T) {
	for _, name := range []string{"", "-export", "1export", "export csv", "export.csv", "export/csv", "export\n", "导出"} {
		node := &CommandNode{Name: name, HandlerRef: "handler", Help: HelpText{Brief: "Export data"}, Meta: NodeMeta{Tags: map[string]string{TagLiteralCommandName: "true"}}}
		if result := ValidateTree(&CommandTree{Nodes: []*CommandNode{node}}); !result.HasErrors() {
			t.Fatalf("accepted unsafe name %q", name)
		}
	}
	parent := &CommandNode{Name: "report", Help: HelpText{Brief: "Reports"}}
	for _, name := range []string{"export", "Export"} {
		parent.Children = append(parent.Children, &CommandNode{Name: name, HandlerRef: name, Help: HelpText{Brief: "Export data"}, Meta: NodeMeta{Tags: map[string]string{TagLiteralCommandName: "true"}}})
	}
	if result := ValidateTree(&CommandTree{Nodes: []*CommandNode{parent}}); !result.HasErrors() {
		t.Fatal("case-insensitive sibling collision accepted")
	}
}
