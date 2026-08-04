// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package encoders

import (
	"bytes"
	"encoding/json"
)

// EncodeNDJSON emits one JSON record per line.
//   - Array input: one line per element.
//   - Non-array input: single line.
//   - Empty array: zero bytes.
//
// Strings with embedded newlines are escaped by encoding/json.
func EncodeNDJSON(data any) ([]byte, error) {
	normalized := normalize(data)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if arr, ok := normalized.([]any); ok {
		if len(arr) == 0 {
			return nil, nil
		}
		// Reuse one encoder so array inputs do not allocate an encoder and
		// buffer for every record.
		for _, item := range arr {
			if err := enc.Encode(item); err != nil {
				return nil, err
			}
		}
		return buf.Bytes(), nil
	}
	if err := enc.Encode(normalized); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// normalize decodes json.RawMessage so array detection works uniformly.
func normalize(data any) any {
	if raw, ok := data.(json.RawMessage); ok && json.Valid(raw) {
		var decoded any
		if json.Unmarshal(raw, &decoded) == nil {
			return decoded
		}
	}
	return data
}
