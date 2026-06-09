// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"encoding/json"
	"log/slog"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

// logIDPrefixes is the set of prefixes the MCP server has historically used
// to mark the trace id text entry. The current server emits "log_id:"; older
// notes/comments referenced "logid:". Match both so a server-side rename does
// not silently drop the id again (which is what happened before — the code
// matched "logid:" but the server emitted "log_id:", so Response.LogID was
// always empty and the slog.Debug call hid the breakage).
var logIDPrefixes = []string{"log_id:", "logid:"}

func stripLogIDPrefix(text string) (id string, matched bool) {
	for _, p := range logIDPrefixes {
		if rest, ok := strings.CutPrefix(text, p); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

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
		if id, ok := stripLogIDPrefix(text); ok {
			result.LogID = id
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
