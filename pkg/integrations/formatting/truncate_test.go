// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBalanced_ShortObjectNoTrunc(t *testing.T) {
	got := TruncateBalanced(`{"a":1}`, 20)
	if got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateBalanced_LongObjectKeepsBrackets(t *testing.T) {
	raw := `{"name":"alice","email":"alice@example.com","age":30}`
	got := TruncateBalanced(raw, 20)
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Fatalf("want balanced braces, got %q", got)
	}
	if !strings.Contains(got, ",…}") {
		t.Fatalf("want ,…} suffix, got %q", got)
	}
	if utf8.RuneCountInString(got) > 20 {
		t.Fatalf("exceeded limit: runes=%d, %q", utf8.RuneCountInString(got), got)
	}
}

func TestTruncateBalanced_ArrayOfObjects(t *testing.T) {
	raw := `[{"k":"v"},{"k2":"v2"},{"k3":"v3"}]`
	got := TruncateBalanced(raw, 15)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("want balanced brackets, got %q", got)
	}
	if !strings.Contains(got, ",…]") {
		t.Fatalf("want ,…] suffix, got %q", got)
	}
}

func TestTruncateBalanced_PrimitiveString(t *testing.T) {
	got := TruncateBalanced("a very long string that overflows", 10)
	if utf8.RuneCountInString(got) > 10 {
		t.Fatalf("len %d > 10: %q", utf8.RuneCountInString(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want trailing ellipsis: %q", got)
	}
}

func TestTruncateBalanced_Zero(t *testing.T) {
	got := TruncateBalanced("abc", 0)
	if got != "abc" {
		t.Fatalf("maxWidth=0 should pass through, got %q", got)
	}
}

func TestTruncateBalanced_ExactFit(t *testing.T) {
	got := TruncateBalanced(`{"a":1}`, 7) // 7 chars
	if got != `{"a":1}` {
		t.Fatalf("exact-fit should pass through, got %q", got)
	}
}

// Regression: previously we returned "[]" when no top-level comma fit in
// budget — the inline array of objects looked empty. The fix cuts at item
// boundaries (after a top-level "}") as well as at commas.
func TestTruncateBalanced_ArrayCutAtItemBoundary(t *testing.T) {
	raw := `[{"field_key":"priority","field_value":"P1"},{"field_key":"assignee","field_value":"alice"}]`
	got := TruncateBalanced(raw, 60)
	if got == "[]" {
		t.Fatalf("regression: got hollow [] for non-empty array: %q", got)
	}
	if !strings.Contains(got, `"priority"`) {
		t.Fatalf("should include first item content: %q", got)
	}
	if !strings.HasSuffix(got, ",…]") {
		t.Fatalf("want ,…] suffix when more items follow: %q", got)
	}
}

// Regression: when even a single item exceeds budget, fall back to plain
// tail-truncate instead of emitting "[]".
func TestTruncateBalanced_SingleItemOverBudget(t *testing.T) {
	raw := `[{"field_key":"priority","field_value":"P1"}]`
	got := TruncateBalanced(raw, 20)
	if got == "[]" {
		t.Fatalf("regression: got hollow [] for oversized item: %q", got)
	}
	if !strings.Contains(got, "field") {
		t.Fatalf("should show some content: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want trailing …: %q", got)
	}
}

// Regression: single long object also must not go hollow.
func TestTruncateBalanced_SingleObjectOverBudget(t *testing.T) {
	raw := `{"very_long_key_name":"very_long_value_that_overflows"}`
	got := TruncateBalanced(raw, 20)
	if got == "{}" {
		t.Fatalf("regression: got hollow {} for oversized object: %q", got)
	}
	if !strings.HasSuffix(got, "…") && !strings.HasSuffix(got, ",…}") {
		t.Fatalf("want truncation marker: %q", got)
	}
}
