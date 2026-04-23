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
// Strings with embedded newlines are escaped by json.Marshal.
func EncodeNDJSON(data any) ([]byte, error) {
	normalized := normalize(data)
	if arr, ok := normalized.([]any); ok {
		if len(arr) == 0 {
			return nil, nil
		}
		var buf bytes.Buffer
		for _, item := range arr {
			enc, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			buf.Write(enc)
			buf.WriteByte('\n')
		}
		return buf.Bytes(), nil
	}
	enc, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return append(enc, '\n'), nil
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
