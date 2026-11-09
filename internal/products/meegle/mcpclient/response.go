// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"encoding/json"
	"log/slog"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/jsonvalue"
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

// toolErrorRetryable reads the backend's tools/call error contract from the
// first content entry: error=<category>,message=<text>,retriable=<true|false>.
// Message text may contain commas and newlines, so only its terminal retriable
// field is authoritative. Ordinary messages and malformed contracts default false.
func toolErrorRetryable(content []mcpContentEntry) bool {
	if len(content) == 0 || content[0].Type != "text" {
		return false
	}
	fields, ok := strings.CutPrefix(content[0].Text, "error=")
	if !ok {
		return false
	}
	category, message, ok := strings.Cut(fields, ",message=")
	if !ok || category == "" || strings.TrimSpace(category) != category || strings.ContainsAny(category, ",=\r\n") {
		return false
	}
	const marker = ",retriable="
	index := strings.LastIndex(message, marker)
	if index < 0 {
		return false
	}
	return message[index+len(marker):] == "true"
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

	var dataTexts []string
	for _, text := range allTexts {
		if id, ok := stripLogIDPrefix(text); ok {
			result.LogID = id
			slog.Debug(text)
			continue
		}
		dataTexts = append(dataTexts, text)
	}

	if resp.IsError {
		msg := strings.Join(dataTexts, "\n")
		if msg == "" {
			msg = "server returned error"
		}
		return nil, meerrors.NewServerError("SERVER_CALL_FAILED", msg).
			WithRetryable(toolErrorRetryable(resp.Content)).WithLogID(result.LogID)
	}

	if len(dataTexts) == 0 {
		return result, nil
	}

	text := dataTexts[0]
	var parsed any
	if err := jsonvalue.Unmarshal([]byte(text), &parsed); err == nil {
		result.Data = parsed
	} else {
		result.Data = text
		result.Raw = text
	}
	return result, nil
}
