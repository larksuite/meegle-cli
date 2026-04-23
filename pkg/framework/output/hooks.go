// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"strconv"
	"strings"
)

type FieldSelectorHook struct{}

func (h FieldSelectorHook) Name() string { return "field_selector" }

func (h FieldSelectorHook) Process(ctx *Context, data any) (any, error) {
	if ctx == nil || ctx.Options == nil || len(ctx.Options.Select) == 0 {
		return data, nil
	}
	paths := make([][]string, 0, len(ctx.Options.Select))
	for _, expr := range ctx.Options.Select {
		paths = append(paths, strings.Split(expr, "."))
	}
	return projectValue(data, paths), nil
}

func projectValue(value any, paths [][]string) any {
	switch typed := normalizeProjectionInput(value).(type) {
	case map[string]any:
		out := map[string]any{}
		for _, path := range paths {
			projectMapPath(typed, path, out)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, projectValue(item, paths))
		}
		return out
	default:
		return value
	}
}

// projectMapPath walks path in source and merges the resolved structure
// into target. The traversal preserves the nested map shape so that
// multiple select expressions can accumulate into the same target
// without clobbering each other.
//
// Array segments:
//   - Numeric segment ("0"/"1"/...) — indexes into the array and is
//     placed under a map keyed by that numeric string (preserves the
//     legacy {"nodes":{"0":...}} shape used by NumericIndex tests).
//   - Non-numeric segment — broadcasts: the remaining path is applied to
//     each map element of the array, per-index results are merged into
//     the existing []any at target[key] (so `list.a,list.b` accumulates
//     both fields into each record).
//
// Non-map array elements are skipped during broadcast — the projection
// target is a map-of-fields, not a scalar list.
func projectMapPath(source map[string]any, path []string, target map[string]any) {
	if len(path) == 0 || target == nil {
		return
	}
	key := path[0]
	next, ok := source[key]
	if !ok {
		return
	}
	remaining := path[1:]
	if len(remaining) == 0 {
		target[key] = next
		return
	}
	switch v := normalizeProjectionInput(next).(type) {
	case map[string]any:
		child, _ := target[key].(map[string]any)
		if child == nil {
			child = map[string]any{}
			target[key] = child
		}
		projectMapPath(v, remaining, child)
	case []any:
		if idx, err := strconv.Atoi(remaining[0]); err == nil {
			projectArrayIndex(v, remaining, idx, target, key)
			return
		}
		target[key] = projectArrayBroadcast(v, remaining, target[key])
	default:
		// Path continues past a scalar — treat as miss.
	}
}

// projectArrayIndex handles numeric-segment array access. The legacy
// shape is preserved: target[key] becomes a map whose string-numeric keys
// point at each referenced element (so `--select nodes.0` yields
// {"nodes":{"0": elem}} rather than [elem]).
func projectArrayIndex(arr []any, remaining []string, idx int, target map[string]any, key string) {
	if idx < 0 || idx >= len(arr) {
		return
	}
	childMap, _ := target[key].(map[string]any)
	if childMap == nil {
		childMap = map[string]any{}
		target[key] = childMap
	}
	idxStr := remaining[0]
	afterIdx := remaining[1:]
	elem := arr[idx]
	if len(afterIdx) == 0 {
		childMap[idxStr] = elem
		return
	}
	elemMap, ok := normalizeProjectionInput(elem).(map[string]any)
	if !ok {
		return
	}
	innerChild, _ := childMap[idxStr].(map[string]any)
	if innerChild == nil {
		innerChild = map[string]any{}
		childMap[idxStr] = innerChild
	}
	projectMapPath(elemMap, afterIdx, innerChild)
}

// projectArrayBroadcast applies remaining over each map element of arr
// and returns a new []any. When existing already holds a []any from a
// prior select expression, per-index maps are reused so multi-expression
// selections (e.g. list.a,list.b) accumulate fields into the same record.
func projectArrayBroadcast(arr []any, remaining []string, existing any) []any {
	existingArr, _ := existing.([]any)
	out := make([]any, 0, len(arr))
	for i, item := range arr {
		itemMap, ok := normalizeProjectionInput(item).(map[string]any)
		if !ok {
			continue
		}
		var childOut map[string]any
		if i < len(existingArr) {
			if em, ok := existingArr[i].(map[string]any); ok {
				childOut = em
			}
		}
		if childOut == nil {
			childOut = map[string]any{}
		}
		projectMapPath(itemMap, remaining, childOut)
		out = append(out, childOut)
	}
	return out
}

func normalizeProjectionInput(value any) any {
	switch typed := value.(type) {
	case map[string]any, []any:
		return typed
	case json.RawMessage:
		var decoded any
		if json.Valid(typed) && json.Unmarshal(typed, &decoded) == nil {
			return decoded
		}
		return value
	case []byte:
		var decoded any
		if json.Valid(typed) && json.Unmarshal(typed, &decoded) == nil {
			return decoded
		}
		return value
	default:
		encoded, err := json.Marshal(value)
		if err != nil || !json.Valid(encoded) {
			return value
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return value
		}
		return decoded
	}
}
