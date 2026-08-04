// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package encoders

import (
	"bytes"
	"encoding/json"
)

// EncodeJSON returns pretty-printed JSON with 2-space indent.
// json.RawMessage inputs are re-indented if valid, otherwise returned as-is.
func EncodeJSON(data any) ([]byte, error) {
	if raw, ok := data.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return append([]byte(nil), raw...), nil
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			return append([]byte(nil), raw...), nil
		}
		return pretty.Bytes(), nil
	}
	encoded, err := marshalJSON(data)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		return encoded, nil
	}
	return pretty.Bytes(), nil
}

// marshalJSON encodes CLI output without HTML escaping. CLI JSON is written
// to a terminal or pipe rather than embedded in HTML, so keeping characters
// such as '&' literal makes URLs directly copyable while remaining valid JSON.
// Encoder.Encode appends a newline, which is removed so callers control framing.
func marshalJSON(data any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
