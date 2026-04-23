// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/olekukonko/tablewriter"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

// TableView is the table-mode Formatter. It is shape-aware and produces
// deterministic output even when the input payload is irregular.
type TableView struct {
	ColumnPriority []string
	MaxColumns     int       // default 8; --verbose bumps to 16
	MaxCellWidth   int       // default 40
	Stderr         io.Writer // hints target; defaults to os.Stderr
}

// DefaultTableView returns a TableView with the standard policies.
func DefaultTableView() TableView {
	return TableView{
		ColumnPriority: GlobalColumnPriority,
		MaxColumns:     8,
		MaxCellWidth:   40,
		Stderr:         os.Stderr,
	}
}

// Encode renders data as an ASCII table according to the shape-aware rules.
func (v TableView) Encode(data any, opts *frameworkoutput.FormatOptions) ([]byte, error) {
	if v.MaxColumns == 0 {
		v = DefaultTableView()
	}
	maxCols := v.MaxColumns
	if opts != nil && opts.Verbose {
		maxCols *= 2
	}
	normalized := Normalize(data)
	switch DetectShape(normalized) {
	case ShapeEmpty:
		v.hint("(no data returned)")
		return nil, nil
	case ShapePrimitive:
		return []byte(fmt.Sprintf("%v\n", normalized)), nil
	case ShapePrimitiveArray:
		arr := normalized.([]any)
		var buf bytes.Buffer
		for _, item := range arr {
			fmt.Fprintln(&buf, item)
		}
		return buf.Bytes(), nil
	case ShapeObject:
		return v.encodeObject(normalized.(map[string]any), selectedList(opts))
	case ShapeObjectArray, ShapeMixedArray:
		return v.encodeArray(normalized.([]any), selectedList(opts), maxCols)
	}
	return nil, nil
}

func selectedList(opts *frameworkoutput.FormatOptions) []string {
	if opts == nil {
		return nil
	}
	return opts.Select
}

func (v TableView) encodeObject(obj map[string]any, selected []string) ([]byte, error) {
	// Per spec §"形状 A/B": user --select keys are authoritative — missing
	// keys render as empty cells, never silently dropped. Auto-derive is the
	// fallback for two cases:
	//   - no user selection (selected is empty)
	//   - every selected expression is stale because post-select unwrap has
	//     peeled the outer key away (allStale signal) — emitting a table of
	//     empty cells would be hostile, so we show the inner record instead
	// partial misses (user typo / schema drift) keep the user's list verbatim
	// and surface the dropped keys to stderr.
	keys, dropped, allStale := reconcileSelect(obj, selected)
	if len(keys) == 0 {
		keys = make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if allStale {
			dropped = nil
		}
	}
	var buf bytes.Buffer
	tw := tablewriter.NewTable(&buf, tablewriter.WithHeader([]string{"key", "value"}))
	for _, k := range keys {
		val := lookupCellValue(obj, k)
		_ = tw.Append([]string{k, RenderCell(val, v.MaxCellWidth)})
	}
	_ = tw.Render()
	if len(dropped) > 0 {
		v.hint(fmt.Sprintf("(ignoring unknown --select keys: %s)", strings.Join(dropped, ", ")))
	}
	return buf.Bytes(), nil
}

// reconcileSelect classifies a user's --select list against firstRow:
//   - kept: the ORIGINAL selected list (verbatim, order-preserving) whenever
//     at least one expression resolves in firstRow; callers render missing
//     keys as empty cells per spec.
//   - dropped: the subset of selected that did not resolve, for a stderr
//     hint so typos don't get silently hidden.
//   - allStale: true when *every* expression failed to resolve — the signal
//     that post-unwrap has peeled the outer key away, and callers should
//     fall through to auto-derived column/key lists instead of emitting a
//     table full of empty cells.
//
// An empty selected slice returns (nil, nil, false): no user intent, no
// fallback needed.
func reconcileSelect(obj map[string]any, selected []string) (kept, dropped []string, allStale bool) {
	if len(selected) == 0 {
		return nil, nil, false
	}
	hasResolvable := false
	for _, expr := range selected {
		if keyPathExists(obj, expr) {
			hasResolvable = true
		} else {
			dropped = append(dropped, expr)
		}
	}
	if !hasResolvable {
		return nil, selected, true
	}
	return selected, dropped, false
}

func keyPathExists(obj map[string]any, expr string) bool {
	if !strings.Contains(expr, ".") {
		_, ok := obj[expr]
		return ok
	}
	segments := strings.Split(expr, ".")
	var current any = obj
	for i, seg := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, has := m[seg]
		if !has {
			return false
		}
		if i == len(segments)-1 {
			return true
		}
		current = next
	}
	return true
}

func (v TableView) encodeArray(arr []any, selected []string, maxCols int) ([]byte, error) {
	firstRow := firstObjectRow(arr)
	if firstRow == nil {
		// Fallback: no object rows, render as primitive listing.
		var buf bytes.Buffer
		for _, item := range arr {
			fmt.Fprintln(&buf, item)
		}
		return buf.Bytes(), nil
	}
	// Per spec §"形状 A": user --select is authoritative. Missing keys
	// render as empty cells; we only fall through to auto-derive columns
	// when every expression is stale (post-unwrap peeled the outer key
	// away — see reconcileSelect for the rationale). Dropped keys are
	// suppressed in that case because they were legitimately unwrapped,
	// not typos.
	kept, dropped, allStale := reconcileSelect(firstRow, selected)
	if allStale {
		kept = nil
		dropped = nil
	}
	cols := ResolveColumns(firstRow, kept, maxCols, v.ColumnPriority)
	if len(cols) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	tw := tablewriter.NewTable(&buf, tablewriter.WithHeader(cols))
	for _, item := range arr {
		row, ok := item.(map[string]any)
		if !ok {
			cells := make([]string, len(cols))
			cells[0] = RenderCell(item, v.MaxCellWidth)
			_ = tw.Append(cells)
			continue
		}
		cells := make([]string, len(cols))
		for i, k := range cols {
			cells[i] = RenderCell(lookupCellValue(row, k), v.MaxCellWidth)
		}
		_ = tw.Append(cells)
	}
	_ = tw.Render()
	total := len(firstRow)
	if len(cols) < total && len(kept) == 0 {
		v.hint(fmt.Sprintf("(showing %d of %d fields; use --select=... or --format json for full view)", len(cols), total))
	}
	if len(dropped) > 0 {
		v.hint(fmt.Sprintf("(ignoring unknown --select keys: %s)", strings.Join(dropped, ", ")))
	}
	return buf.Bytes(), nil
}

func firstObjectRow(arr []any) map[string]any {
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// lookupCellValue resolves a dotted path within a row.
func lookupCellValue(row map[string]any, expr string) any {
	if !strings.Contains(expr, ".") {
		return row[expr]
	}
	segments := strings.Split(expr, ".")
	var current any = row
	for _, seg := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[seg]
	}
	return current
}

func (v TableView) hint(msg string) {
	w := v.Stderr
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintln(w, msg)
}
