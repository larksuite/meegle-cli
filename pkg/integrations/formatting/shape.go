// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import "encoding/json"

// DataShape classifies an arbitrary payload for table rendering.
type DataShape int

const (
	ShapeEmpty DataShape = iota
	ShapePrimitive
	ShapeObject
	ShapePrimitiveArray
	ShapeObjectArray
	ShapeMixedArray
)

// DetectShape inspects data and returns its shape. Inputs wrapped as
// json.RawMessage or []byte are decoded first.
func DetectShape(data any) DataShape {
	v := Normalize(data)
	switch typed := v.(type) {
	case nil:
		return ShapeEmpty
	case map[string]any:
		if len(typed) == 0 {
			return ShapeEmpty
		}
		return ShapeObject
	case []any:
		if len(typed) == 0 {
			return ShapeEmpty
		}
		hasObject := false
		hasPrimitive := false
		for _, item := range typed {
			switch item.(type) {
			case map[string]any:
				hasObject = true
			case []any:
				// Nested arrays count as non-primitive for column-selection
				// purposes (can't fit sensibly in a simple cell).
				hasObject = true
			default:
				hasPrimitive = true
			}
		}
		if hasObject && hasPrimitive {
			return ShapeMixedArray
		}
		if hasObject {
			return ShapeObjectArray
		}
		return ShapePrimitiveArray
	default:
		return ShapePrimitive
	}
}

// Normalize unwraps json.RawMessage / []byte to native Go values.
func Normalize(data any) any {
	switch v := data.(type) {
	case json.RawMessage:
		if json.Valid(v) {
			var decoded any
			if json.Unmarshal(v, &decoded) == nil {
				return decoded
			}
		}
		return data
	case []byte:
		if json.Valid(v) {
			var decoded any
			if json.Unmarshal(v, &decoded) == nil {
				return decoded
			}
		}
		return data
	}
	return data
}
