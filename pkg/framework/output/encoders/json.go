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
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		return encoded, nil
	}
	return pretty.Bytes(), nil
}
