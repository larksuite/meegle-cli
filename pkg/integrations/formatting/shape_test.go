// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"encoding/json"
	"testing"
)

func TestDetectShape(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want DataShape
	}{
		{"nil", nil, ShapeEmpty},
		{"empty map", map[string]any{}, ShapeEmpty},
		{"empty array", []any{}, ShapeEmpty},
		{"primitive string", "s", ShapePrimitive},
		{"primitive int", 42, ShapePrimitive},
		{"map", map[string]any{"a": 1}, ShapeObject},
		{"primitive array", []any{"a", "b"}, ShapePrimitiveArray},
		{"object array", []any{map[string]any{"a": 1}, map[string]any{"b": 2}}, ShapeObjectArray},
		{"mixed array", []any{"x", map[string]any{"a": 1}}, ShapeMixedArray},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectShape(c.in); got != c.want {
				t.Fatalf("want %v got %v", c.want, got)
			}
		})
	}
}

func TestDetectShape_DecodesRawMessage(t *testing.T) {
	raw := json.RawMessage(`[{"a":1},{"a":2}]`)
	if got := DetectShape(raw); got != ShapeObjectArray {
		t.Fatalf("want ObjectArray, got %v", got)
	}
}
