// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

// UnwrapWrapperObject performs loss-less drill-in before table/ndjson
// rendering so that single-wrapper payloads render naturally:
//
//   - Single-key map whose value is []any → returns the []any
//     (e.g. {"list":[...]}  →  [...]).
//   - Single-key map whose value is a map[string]any → returns the inner
//     map, and repeats (e.g. {"creator":{"name":"alice","email":"x"}}
//     → {"name":"alice","email":"x"}).
//
// Peeling is strictly loss-less: it stops as soon as the current map has
// zero or more than one key, so {"list":[...], "total":N} is never peeled
// — metadata siblings are preserved and the renderer shows the outer
// KV/structure. Scalar values under a single key are also left alone so
// {"creator":"alice"} stays a KV row instead of collapsing into "alice".
func UnwrapWrapperObject(data any) any {
	for {
		obj, ok := Normalize(data).(map[string]any)
		if !ok {
			return data
		}
		if arr, ok := arrayUnwrapStep(obj); ok {
			data = arr
			continue
		}
		if inner, ok := objectUnwrapStep(obj); ok {
			data = inner
			continue
		}
		return data
	}
}

// arrayUnwrapStep returns the inner []any when obj has exactly one key
// and that key's value is an array. Multi-key wrappers like
// {"list":[...], "total":N} are left untouched so metadata siblings
// survive.
func arrayUnwrapStep(obj map[string]any) ([]any, bool) {
	if len(obj) != 1 {
		return nil, false
	}
	for _, val := range obj {
		if arr, isArr := val.([]any); isArr {
			return arr, true
		}
	}
	return nil, false
}

// objectUnwrapStep peels a single-key map whose value is itself a map.
// {"creator": {"name": "alice", "email": "x"}} → {"name": "alice",
// "email": "x"}. Returns (nil, false) when obj has !=1 keys or its
// single value is not a map.
func objectUnwrapStep(obj map[string]any) (map[string]any, bool) {
	if len(obj) != 1 {
		return nil, false
	}
	for _, val := range obj {
		if m, isMap := val.(map[string]any); isMap {
			return m, true
		}
	}
	return nil, false
}
