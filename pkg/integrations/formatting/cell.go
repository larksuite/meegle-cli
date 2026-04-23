// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// RenderCell returns the table-cell string for an arbitrary value.
//
// Rules (matching the design spec §格式行为规约 > table):
//   - nil → ""
//   - string/number/bool → Sprint, truncated by maxWidth
//   - primitive-only arrays → comma-joined, truncated
//   - objects and mixed/object arrays → compact JSON, bracket-balanced truncation
//
// maxWidth <= 0 disables truncation.
func RenderCell(v any, maxWidth int) string {
	v = Normalize(v)
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return maybeTruncatePlain(typed, maxWidth)
	case bool:
		return strconvBool(typed)
	case json.Number:
		return maybeTruncatePlain(typed.String(), maxWidth)
	case float64:
		return maybeTruncatePlain(formatFloat(typed), maxWidth)
	case float32:
		return maybeTruncatePlain(formatFloat(float64(typed)), maxWidth)
	case []any:
		if isPrimitiveArray(typed) {
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, fmt.Sprint(item))
			}
			return maybeTruncatePlain(strings.Join(parts, ", "), maxWidth)
		}
		enc, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return TruncateBalanced(string(enc), maxWidth)
	case map[string]any:
		enc, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return TruncateBalanced(string(enc), maxWidth)
	default:
		// numeric types (int/float/...) fall here via fmt.Sprint
		return maybeTruncatePlain(fmt.Sprint(typed), maxWidth)
	}
}

// formatFloat avoids scientific notation for integer-valued floats so JSON
// numbers (which unmarshal to float64) render as expected in table cells.
// work_item_id 1234567 should show as "1234567", not "1.234567e+06".
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func isPrimitiveArray(arr []any) bool {
	for _, item := range arr {
		switch item.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

func maybeTruncatePlain(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	return truncatePlain(s, maxWidth)
}

func strconvBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
