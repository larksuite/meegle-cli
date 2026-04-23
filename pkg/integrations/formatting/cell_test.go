// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderCell_Primitives(t *testing.T) {
	if got := RenderCell("foo", 20); got != "foo" {
		t.Fatalf("%q", got)
	}
	if got := RenderCell(42, 20); got != "42" {
		t.Fatalf("%q", got)
	}
	if got := RenderCell(true, 20); got != "true" {
		t.Fatalf("%q", got)
	}
	if got := RenderCell(false, 20); got != "false" {
		t.Fatalf("%q", got)
	}
	if got := RenderCell(nil, 20); got != "" {
		t.Fatalf("%q", got)
	}
}

// Regression: JSON numbers unmarshal to float64. work_item_id 1234567 used to
// render as "1.234567e+06"; we render integer-valued floats as ints.
func TestRenderCell_IntegerValuedFloat(t *testing.T) {
	if got := RenderCell(float64(1234567), 20); got != "1234567" {
		t.Fatalf("want integer form, got %q", got)
	}
	if got := RenderCell(float64(1713600000), 20); got != "1713600000" {
		t.Fatalf("want integer form for timestamp, got %q", got)
	}
}

func TestRenderCell_NonIntegerFloat(t *testing.T) {
	if got := RenderCell(1.5, 20); got != "1.5" {
		t.Fatalf("%q", got)
	}
	if got := RenderCell(0.25, 20); got != "0.25" {
		t.Fatalf("%q", got)
	}
}

func TestRenderCell_PrimitiveArrayCommaJoin(t *testing.T) {
	got := RenderCell([]any{"a", "b", "c"}, 20)
	if got != "a, b, c" {
		t.Fatalf("%q", got)
	}
}

func TestRenderCell_NumericArray(t *testing.T) {
	got := RenderCell([]any{1, 2, 3}, 20)
	if got != "1, 2, 3" {
		t.Fatalf("%q", got)
	}
}

func TestRenderCell_NestedObjectCompact(t *testing.T) {
	got := RenderCell(map[string]any{"name": "alice"}, 40)
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Fatalf("not balanced: %q", got)
	}
	if !strings.Contains(got, `"name":"alice"`) {
		t.Fatalf("missing key: %q", got)
	}
}

func TestRenderCell_NestedObjectTruncated(t *testing.T) {
	got := RenderCell(map[string]any{"n": "alice", "e": "a@x", "r": "admin", "z": "zzz"}, 20)
	if utf8.RuneCountInString(got) > 20 {
		t.Fatalf("too long: %d %q", utf8.RuneCountInString(got), got)
	}
	if !strings.HasSuffix(got, "…}") {
		t.Fatalf("want …} suffix: %q", got)
	}
}

func TestRenderCell_ObjectArrayInline(t *testing.T) {
	got := RenderCell([]any{
		map[string]any{"k": "v"},
		map[string]any{"k2": "v2"},
	}, 60)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("not balanced: %q", got)
	}
	if !strings.Contains(got, `{"k":"v"}`) {
		t.Fatalf("missing element: %q", got)
	}
}

func TestRenderCell_PrimitiveOverflowTruncated(t *testing.T) {
	got := RenderCell("a very long stringggggggggg", 10)
	if utf8.RuneCountInString(got) > 10 {
		t.Fatalf("too long: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want trailing …: %q", got)
	}
}
