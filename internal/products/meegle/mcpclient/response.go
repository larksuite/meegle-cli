// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"encoding/json"
	"log/slog"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

type Response struct {
	Data  any
	Raw   string
	LogID string
}

type mcpContentEntry struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResponse struct {
	Content []mcpContentEntry `json:"content"`
	IsError bool              `json:"isError"`
}

func unwrapResponse(raw json.RawMessage) (*Response, error) {
	if len(raw) == 0 {
		return &Response{}, nil
	}

	var resp mcpToolResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return &Response{Raw: string(raw)}, nil
	}

	result := &Response{}
	var allTexts []string

	// Collect ALL text entries first (before filtering logid)
	for _, entry := range resp.Content {
		if entry.Type != "text" || entry.Text == "" {
			continue
		}
		allTexts = append(allTexts, entry.Text)
	}

	// Check isError BEFORE filtering logid, so error messages include logid text
	if resp.IsError {
		msg := strings.Join(allTexts, "\n")
		if msg == "" {
			msg = "server returned error"
		}
		return nil, meerrors.NewServerError("SERVER_CALL_FAILED", msg)
	}

	// Filter logid only for non-error responses
	var dataTexts []string
	for _, text := range allTexts {
		if strings.HasPrefix(text, "logid:") {
			result.LogID = text
			slog.Debug(text)
		} else {
			dataTexts = append(dataTexts, text)
		}
	}

	if len(dataTexts) == 0 {
		return result, nil
	}

	text := dataTexts[0]
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		result.Data = parsed
	} else {
		result.Data = text
		result.Raw = text
	}
	return result, nil
}
