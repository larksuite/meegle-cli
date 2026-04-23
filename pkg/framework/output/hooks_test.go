// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"reflect"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/executor"
)

func TestFieldSelectorHookNoOp(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{Options: &FormatOptions{}, Result: &executor.RawResult{}}
	data := map[string]any{"a": 1, "b": 2}
	out, err := hook.Process(ctx, data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(out, data) {
		t.Fatalf("pass-through violated: %v", out)
	}
}

func TestFieldSelectorHookProjectsFlatFields(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"key", "priority"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{"key": "PROJ", "priority": 3, "ignored": true})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	projected := data.(map[string]any)
	if len(projected) != 2 || projected["key"] != "PROJ" || projected["priority"] != 3 {
		t.Fatalf("projected = %#v", projected)
	}
}

func TestFieldSelectorHookProjectsNestedFields(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"fields.status"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{"fields": map[string]any{"status": "done", "priority": 1}})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	projected := data.(map[string]any)
	fields := projected["fields"].(map[string]any)
	if fields["status"] != "done" {
		t.Fatalf("fields = %#v", fields)
	}
	if _, ok := fields["priority"]; ok {
		t.Fatalf("expected nested projection to omit priority: %#v", fields)
	}
}

func TestFieldSelectorHookArrayProjection(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"id"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, []any{
		map[string]any{"id": 1, "name": "a"},
		map[string]any{"id": 2, "name": "b"},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	arr := data.([]any)
	if arr[0].(map[string]any)["id"] != 1 {
		t.Fatalf("got %v", arr)
	}
	if _, ok := arr[0].(map[string]any)["name"]; ok {
		t.Fatalf("should drop name: %v", arr)
	}
}

// Drill into an array element by numeric index segment: --select=nodes.0
// should return the first element under the nodes key.
func TestFieldSelectorHook_NumericIndexFirstElement(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"nodes.0"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"nodes": []any{
			map[string]any{"id": "a", "name": "first"},
			map[string]any{"id": "b", "name": "second"},
		},
		"other": "drop",
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	// Result shape: {"nodes": {"0": {id: "a", name: "first"}}}.
	// CompositeFormatter's UnwrapWrapperObject peels the chain for table
	// rendering; at this layer we just verify the leaf is reachable.
	outer := data.(map[string]any)
	nodes := outer["nodes"].(map[string]any)
	first := nodes["0"].(map[string]any)
	if first["id"] != "a" || first["name"] != "first" {
		t.Fatalf("wrong element reached: %v", first)
	}
}

// Drill further into a field of a specific array element:
// --select=nodes.0.id should yield just the id of the first element.
func TestFieldSelectorHook_NumericIndexWithNestedField(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"nodes.0.id"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"nodes": []any{
			map[string]any{"id": "a", "name": "first"},
			map[string]any{"id": "b"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	outer := data.(map[string]any)
	inner := outer["nodes"].(map[string]any)["0"].(map[string]any)
	if inner["id"] != "a" {
		t.Fatalf("want id 'a', got %v", inner)
	}
	if _, has := inner["name"]; has {
		t.Fatalf("name must be filtered: %v", inner)
	}
}

// Out-of-range index resolves to "not found", so the projection returns
// an empty map rather than panicking.
func TestFieldSelectorHook_NumericIndexOutOfRange(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"nodes.5"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"nodes": []any{map[string]any{"id": "a"}},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	m := data.(map[string]any)
	if len(m) != 0 {
		t.Fatalf("want empty map for out-of-range, got %v", m)
	}
}

// Non-numeric segment on an array broadcasts: the remaining path is
// applied to each map element, preserving structure.
func TestFieldSelectorHook_NonNumericOnArrayBroadcasts(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"nodes.name"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"nodes": []any{
			map[string]any{"name": "foo", "age": 10},
			map[string]any{"name": "bar", "age": 20},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	nodes := data.(map[string]any)["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("want 2 broadcast items, got %v", nodes)
	}
	first := nodes[0].(map[string]any)
	if first["name"] != "foo" {
		t.Fatalf("first element name = %v", first)
	}
	if _, has := first["age"]; has {
		t.Fatalf("age must be filtered in broadcast: %v", first)
	}
}

// Broadcast across a wrapped array: --select list.work_item_info.work_item_name
// on {"list":[{"work_item_info":{"work_item_name":"x"}}, ...]} drills into
// each element and preserves the {list: [{work_item_info: {work_item_name}}]}
// structure.
func TestFieldSelectorHook_BroadcastNestedPath(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"list.work_item_info.work_item_name"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"list": []any{
			map[string]any{
				"work_item_info": map[string]any{"work_item_name": "task A", "work_item_id": 1},
				"status":         "done",
			},
			map[string]any{
				"work_item_info": map[string]any{"work_item_name": "task B", "work_item_id": 2},
			},
		},
		"total": 2,
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	root := data.(map[string]any)
	if _, has := root["total"]; has {
		t.Fatalf("unselected top-level keys must be dropped: %v", root)
	}
	list := root["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 projected list items, got %d", len(list))
	}
	first := list[0].(map[string]any)["work_item_info"].(map[string]any)
	if first["work_item_name"] != "task A" {
		t.Fatalf("want task A, got %v", first)
	}
	if _, has := first["work_item_id"]; has {
		t.Fatalf("sibling fields must be filtered: %v", first)
	}
}

// Two broadcast paths over the same array merge per-index into the same
// record, so --select list.a,list.b yields [{a,b}, {a,b}, ...].
func TestFieldSelectorHook_BroadcastMultiPathPerIndexMerge(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"list.a", "list.b"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"list": []any{
			map[string]any{"a": 1, "b": 2, "c": 3},
			map[string]any{"a": 4, "b": 5, "c": 6},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	list := data.(map[string]any)["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 items, got %d", len(list))
	}
	first := list[0].(map[string]any)
	if first["a"] != 1 || first["b"] != 2 {
		t.Fatalf("want {a:1,b:2}, got %v", first)
	}
	if _, has := first["c"]; has {
		t.Fatalf("c must be filtered: %v", first)
	}
	second := list[1].(map[string]any)
	if second["a"] != 4 || second["b"] != 5 {
		t.Fatalf("want {a:4,b:5}, got %v", second)
	}
}

// Broadcast skips non-map elements (primitives, nested arrays); only the
// map items in the array receive the per-element projection.
func TestFieldSelectorHook_BroadcastSkipsNonMapElements(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"list.name"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"list": []any{
			"skip me",
			map[string]any{"name": "keep", "other": "drop"},
			42,
			map[string]any{"name": "also keep"},
		},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	list := data.(map[string]any)["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 map items retained, got %d: %v", len(list), list)
	}
	if list[0].(map[string]any)["name"] != "keep" {
		t.Fatalf("first item wrong: %v", list[0])
	}
	if list[1].(map[string]any)["name"] != "also keep" {
		t.Fatalf("second item wrong: %v", list[1])
	}
}

// Single-select of an array key returns the full array untouched (path
// ends before the array, so broadcasting does not apply).
func TestFieldSelectorHook_SelectWholeArrayKeyPreservesItems(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"list"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"list":  []any{map[string]any{"a": 1, "b": 2}},
		"total": 1,
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	root := data.(map[string]any)
	if _, has := root["total"]; has {
		t.Fatalf("total must be filtered: %v", root)
	}
	list := root["list"].([]any)
	first := list[0].(map[string]any)
	if first["a"] != 1 || first["b"] != 2 {
		t.Fatalf("list items should be preserved verbatim, got %v", first)
	}
}

// Map key that happens to be named "0" still resolves via the map branch —
// the map case runs before any array handling.
func TestFieldSelectorHook_MapKeyZeroStillWorks(t *testing.T) {
	hook := FieldSelectorHook{}
	ctx := &Context{
		Options: &FormatOptions{Select: []string{"groups.0"}},
		Result:  &executor.RawResult{},
	}
	data, err := hook.Process(ctx, map[string]any{
		"groups": map[string]any{"0": "default", "1": "extra"},
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	m := data.(map[string]any)
	groups := m["groups"].(map[string]any)
	if groups["0"] != "default" {
		t.Fatalf("map-key '0' should resolve: %v", groups)
	}
}
