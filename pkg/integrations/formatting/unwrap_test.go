// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestUnwrapWrapperObject_SingleListKey(t *testing.T) {
	data := map[string]any{"list": []any{1, 2, 3}}
	got := UnwrapWrapperObject(data)
	want := []any{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestUnwrapWrapperObject_SingleDataKey(t *testing.T) {
	data := map[string]any{"data": []any{"a", "b"}}
	got := UnwrapWrapperObject(data)
	want := []any{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestUnwrapWrapperObject_SingleItemsAndResults(t *testing.T) {
	if got := UnwrapWrapperObject(map[string]any{"items": []any{1}}); !reflect.DeepEqual(got, []any{1}) {
		t.Fatalf("items: got %v", got)
	}
	if got := UnwrapWrapperObject(map[string]any{"results": []any{2}}); !reflect.DeepEqual(got, []any{2}) {
		t.Fatalf("results: got %v", got)
	}
}

// Multi-key wrappers must NEVER be unwrapped: siblings like total /
// page_info are first-class metadata the renderer must preserve.
func TestUnwrapWrapperObject_KeepsMetadataSiblings(t *testing.T) {
	data := map[string]any{
		"list":  []any{map[string]any{"id": 1}},
		"total": 1,
		"page_info": map[string]any{
			"page": 1,
		},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("multi-key wrapper must stay as map, got %T", got)
	}
	if _, has := m["total"]; !has {
		t.Fatalf("total sibling lost: %v", m)
	}
	if _, has := m["page_info"]; !has {
		t.Fatalf("page_info sibling lost: %v", m)
	}
}

func TestUnwrapWrapperObject_WrapperKeyPresentButNotArray(t *testing.T) {
	// Do NOT unwrap {"data": 5} — value is not an array.
	data := map[string]any{"data": 5}
	got := UnwrapWrapperObject(data)
	if _, isMap := got.(map[string]any); !isMap {
		t.Fatalf("should stay as map, got %T", got)
	}
}

func TestUnwrapWrapperObject_NestedObjectUnderWrapper(t *testing.T) {
	// workitem query returns {"data": {"1": [...]}}. With recursive
	// single-key peel, UnwrapWrapperObject drills through data → "1" and
	// returns the inner []any so table/ndjson can render the records.
	data := map[string]any{"data": map[string]any{"1": []any{"x"}}}
	got := UnwrapWrapperObject(data)
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("should peel to []any, got %T: %v", got, got)
	}
	if len(arr) != 1 || arr[0] != "x" {
		t.Fatalf("want [\"x\"], got %v", arr)
	}
}

func TestUnwrapWrapperObject_PlainArrayPassThrough(t *testing.T) {
	data := []any{1, 2, 3}
	got := UnwrapWrapperObject(data)
	if !reflect.DeepEqual(got, data) {
		t.Fatalf("want pass-through, got %v", got)
	}
}

func TestUnwrapWrapperObject_NoWrapperKey(t *testing.T) {
	data := map[string]any{"other": []any{1, 2}, "another": "x"}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok || !reflect.DeepEqual(m, data) {
		t.Fatalf("want pass-through map, got %v (%T)", got, got)
	}
}

func TestUnwrapWrapperObject_Primitive(t *testing.T) {
	for _, v := range []any{"hello", 42, true, nil} {
		got := UnwrapWrapperObject(v)
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("want pass-through for %v, got %v", v, got)
		}
	}
}

func TestUnwrapWrapperObject_RawMessage(t *testing.T) {
	raw := json.RawMessage(`{"list":[{"id":1},{"id":2}]}`)
	got := UnwrapWrapperObject(raw)
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("want []any after normalize, got %T", got)
	}
	if len(arr) != 2 {
		t.Fatalf("want 2 elements, got %d", len(arr))
	}
}

// Two array-valued keys means there is no unambiguous single wrapper;
// UnwrapWrapperObject must not guess — the caller sees both.
func TestUnwrapWrapperObject_MultipleArrayKeysStayAsMap(t *testing.T) {
	data := map[string]any{
		"list": []any{1, 2},
		"data": []any{"a", "b"},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map preserved, got %T", got)
	}
	if _, has := m["list"]; !has {
		t.Fatalf("list lost: %v", m)
	}
	if _, has := m["data"]; !has {
		t.Fatalf("data lost: %v", m)
	}
}

// Fallback: resource-specific wrapper names unwrap when the only key
// holds an array. Covers {"comments": [...]} / {"records": [...]} /
// {"work_items": [...]}.
func TestUnwrapWrapperObject_SingleKeyFallback(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
	}{
		{"comments", map[string]any{"comments": []any{map[string]any{"id": 1}}}},
		{"records", map[string]any{"records": []any{"r1", "r2"}}},
		{"work_items", map[string]any{"work_items": []any{map[string]any{"id": 9}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := UnwrapWrapperObject(c.data)
			if _, ok := got.([]any); !ok {
				t.Fatalf("want []any, got %T", got)
			}
		})
	}
}

// Fallback should NOT trigger when there are multiple keys of any kind —
// workitem get returns {"work_item_id": 1, "name": "foo", "fields": [...]}
// and unwrapping "fields" would silently drop the rest of the record.
func TestUnwrapWrapperObject_MultiKeyNoFallback(t *testing.T) {
	data := map[string]any{
		"work_item_id": 1,
		"name":         "foo",
		"fields":       []any{map[string]any{"k": "v"}},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("must stay as map, got %T", got)
	}
	if _, has := m["work_item_id"]; !has {
		t.Fatalf("lost record fields: %v", m)
	}
}

// Fallback does not fire when the single key's value is not an array.
func TestUnwrapWrapperObject_SingleKeyNonArray(t *testing.T) {
	got := UnwrapWrapperObject(map[string]any{"record": map[string]any{"k": "v"}})
	if _, isMap := got.(map[string]any); !isMap {
		t.Fatalf("want pass-through map, got %T", got)
	}
}

// Pagination siblings are preserved: the outer map has more than one
// key so unwrapping would drop the pagination metadata.
func TestUnwrapWrapperObject_ArrayPlusPaginationStays(t *testing.T) {
	data := map[string]any{
		"comments":   []any{map[string]any{"id": 1}, map[string]any{"id": 2}},
		"pagination": map[string]any{"page_num": 1, "total": 2},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("multi-key wrapper must stay as map, got %T", got)
	}
	if _, has := m["pagination"]; !has {
		t.Fatalf("pagination sibling lost: %v", m)
	}
}

// total sibling is preserved alongside the records array — multi-key
// wrappers are never peeled.
func TestUnwrapWrapperObject_ArrayPlusTotalStays(t *testing.T) {
	data := map[string]any{
		"records": []any{"r1", "r2", "r3"},
		"total":   3,
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("multi-key wrapper must stay as map, got %T", got)
	}
	if _, has := m["total"]; !has {
		t.Fatalf("total sibling lost: %v", m)
	}
}

// Any record with siblings beyond the single array key stays intact —
// the caller (or --select) is responsible for drilling in explicitly.
func TestUnwrapWrapperObject_ArrayPlusRealDataSibling_NoUnwrap(t *testing.T) {
	data := map[string]any{
		"fields": []any{map[string]any{"k": "v"}},
		"name":   "some workitem",
		"owner":  "alice",
		"total":  1,
	}
	got := UnwrapWrapperObject(data)
	if _, isMap := got.(map[string]any); !isMap {
		t.Fatalf("must stay as map (name/owner are real data), got %T", got)
	}
}

// Post-select object drill-in: --select=creator produces {creator: {name,
// email}}; we want table to show name/email as KV rows, not the whole
// creator object compacted into one cell.
func TestUnwrapWrapperObject_PeelsSingleKeyMap(t *testing.T) {
	data := map[string]any{
		"creator": map[string]any{"name": "alice", "email": "a@x"},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want peeled map, got %T: %v", got, got)
	}
	if m["name"] != "alice" || m["email"] != "a@x" {
		t.Fatalf("missing peeled keys: %v", m)
	}
}

// --select=a.b.c produces {a: {b: {c: val}}}; recursive peel should drill
// to the innermost non-trivial map.
func TestUnwrapWrapperObject_PeelsSingleKeyMapChain(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "leaf",
			},
		},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T: %v", got, got)
	}
	if v, has := m["c"]; !has || v != "leaf" {
		t.Fatalf("want {c: leaf}, got %v", m)
	}
}

// Peeling stops as soon as the map has more than one key.
func TestUnwrapWrapperObject_StopsOnMultiKey(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": 1, "c": 2},
	}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", got)
	}
	if len(m) != 2 || m["b"] != 1 || m["c"] != 2 {
		t.Fatalf("want {b:1,c:2}, got %v", m)
	}
}

// Single-key map with scalar value must stay as a map — user-visible
// KV row is more informative than the bare scalar.
func TestUnwrapWrapperObject_ScalarSingleKeyStays(t *testing.T) {
	data := map[string]any{"creator": "alice"}
	got := UnwrapWrapperObject(data)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", got)
	}
	if m["creator"] != "alice" {
		t.Fatalf("want {creator: alice}, got %v", m)
	}
}
