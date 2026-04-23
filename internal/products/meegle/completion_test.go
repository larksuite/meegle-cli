// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestExtractStringField(t *testing.T) {
	extract := extractStringField("project_key")

	tests := []struct {
		name string
		data any
		want []string
	}{
		{
			name: "direct array",
			data: []any{
				map[string]any{"project_key": "proj_a", "name": "A"},
				map[string]any{"project_key": "proj_b", "name": "B"},
			},
			want: []string{"proj_a", "proj_b"},
		},
		{
			name: "nested in items",
			data: map[string]any{
				"items": []any{
					map[string]any{"project_key": "proj_x"},
				},
			},
			want: []string{"proj_x"},
		},
		{
			name: "empty data",
			data: nil,
			want: nil,
		},
		{
			name: "missing field",
			data: []any{
				map[string]any{"other_key": "val"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extract(tt.data)
			if len(got) != len(tt.want) {
				t.Fatalf("len: got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestToSlice(t *testing.T) {
	tests := []struct {
		name string
		data any
		want int
	}{
		{"direct array", []any{1, 2, 3}, 3},
		{"map with items", map[string]any{"items": []any{1}}, 1},
		{"map with list", map[string]any{"list": []any{1, 2}}, 2},
		{"map with data", map[string]any{"data": []any{1, 2, 3}}, 3},
		{"nil", nil, 0},
		{"string", "hello", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toSlice(tt.data)
			if len(got) != tt.want {
				t.Errorf("len: got %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestMakeCompletionFuncNilClient(t *testing.T) {
	rule := completionRules["project-key"]
	fn := makeCompletionFunc(rule, nil)
	results, directive := fn(&cobra.Command{}, nil, "")
	if len(results) != 0 {
		t.Errorf("expected empty results with nil client, got %v", results)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp")
	}
}

func TestMakeCompletionFuncNilParams(t *testing.T) {
	rule := completionRules["work-item-type-key"]
	fn := makeCompletionFunc(rule, nil)
	cmd := &cobra.Command{}
	cmd.Flags().String("project-key", "", "")
	results, directive := fn(cmd, nil, "")
	if len(results) != 0 {
		t.Errorf("expected empty results, got %v", results)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp")
	}
}

func TestRegisterFlagCompletions(t *testing.T) {
	root := &cobra.Command{Use: "meegle"}
	root.PersistentFlags().String("format", "json", "output format")

	sub := &cobra.Command{Use: "create"}
	sub.Flags().String("project-key", "", "project key")
	sub.Flags().String("work-item-type-key", "", "type key")

	group := &cobra.Command{Use: "workitem"}
	group.AddCommand(sub)
	root.AddCommand(group)

	// Should not panic with nil client
	RegisterFlagCompletions(root, nil)
}
