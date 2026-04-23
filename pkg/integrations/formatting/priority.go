// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import "sort"

// GlobalColumnPriority is the seed priority list for auto-column-selection
// when the user does not pass --select. Hits come first in priority order;
// remaining keys are appended alphabetically up to the column cap.
var GlobalColumnPriority = []string{
	"id", "key", "work_item_id", "work_item_type_key",
	"name", "title", "subject", "display_name",
	"state", "status", "state_key",
	"owner", "assignee", "created_by", "user_key",
	"email",
	"created_at", "updated_at", "finish_time",
}

// ResolveColumns picks the final column order for a row.
//   - If selected is non-empty, that list is used verbatim (honoring user intent).
//   - Otherwise: priority hits (in priority order) come first; remaining keys
//     are appended in alphabetical order. Result is capped at maxCols.
func ResolveColumns(row map[string]any, selected []string, maxCols int, priority []string) []string {
	if len(selected) > 0 {
		return append([]string(nil), selected...)
	}
	if row == nil || maxCols <= 0 {
		return nil
	}
	present := make(map[string]struct{}, len(row))
	for k := range row {
		present[k] = struct{}{}
	}
	ordered := make([]string, 0, maxCols)
	used := make(map[string]struct{}, maxCols)
	for _, key := range priority {
		if _, ok := present[key]; !ok {
			continue
		}
		ordered = append(ordered, key)
		used[key] = struct{}{}
		if len(ordered) >= maxCols {
			return ordered
		}
	}
	rest := make([]string, 0, len(present))
	for k := range present {
		if _, ok := used[k]; !ok {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if len(ordered) >= maxCols {
			break
		}
		ordered = append(ordered, k)
	}
	return ordered
}
