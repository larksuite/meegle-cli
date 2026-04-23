// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"reflect"
	"testing"
)

func TestResolveColumns_PriorityFirst(t *testing.T) {
	row := map[string]any{
		"avatar_url": "x",
		"id":         1,
		"email":      "a@x",
		"name":       "alice",
		"updated_at": "2026-04-20",
		"extra1":     "",
		"extra2":     "",
	}
	got := ResolveColumns(row, nil, 6, GlobalColumnPriority)
	// priority hits: id, name, email, updated_at → 4 columns in priority order;
	// then alphabetical fill: avatar_url, extra1 (2 more to reach 6)
	want := []string{"id", "name", "email", "updated_at", "avatar_url", "extra1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v\n got %v", want, got)
	}
}

func TestResolveColumns_ExplicitSelect(t *testing.T) {
	row := map[string]any{"a": 1, "b": 2, "c": 3}
	got := ResolveColumns(row, []string{"c", "a"}, 10, GlobalColumnPriority)
	if !reflect.DeepEqual(got, []string{"c", "a"}) {
		t.Fatalf("select should be honored verbatim: %v", got)
	}
}

func TestResolveColumns_MaxCaps(t *testing.T) {
	row := map[string]any{"x1": 1, "x2": 2, "x3": 3, "x4": 4}
	got := ResolveColumns(row, nil, 2, GlobalColumnPriority)
	if len(got) != 2 {
		t.Fatalf("want 2 columns, got %d (%v)", len(got), got)
	}
}

func TestResolveColumns_NilRow(t *testing.T) {
	got := ResolveColumns(nil, nil, 5, GlobalColumnPriority)
	if got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}
