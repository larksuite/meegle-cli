// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

const maxToolsListPages = 256

// ListToolDirectory fetches a complete tools/list result through Call. Single
// pages are returned unchanged; paginated results contain the ordered raw tools
// only, so callers retain ownership of schema and optional metadata decoding.
// Each response keeps Call's size limit. The sum of result bytes is limited to
// 8 MiB, pagination to 256 pages, and paginated directories to 2048 entries.
// Errors never return a partial directory. Pagination diagnostics omit cursors
// and response contents; Call retains its existing request error handling.
func (c *Client) ListToolDirectory(ctx context.Context) (json.RawMessage, error) {
	var params any
	var tools []json.RawMessage
	seen := make(map[string]struct{})
	totalBytes := 0
	for page := 0; page < maxToolsListPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := c.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		totalBytes += len(raw)
		if totalBytes > maxToolsListResponseBytes {
			return nil, fmt.Errorf("tools/list cumulative result exceeds %d-byte limit", maxToolsListResponseBytes)
		}
		var result struct {
			Tools      []json.RawMessage `json:"tools"`
			NextCursor json.RawMessage   `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("parse tools/list result: %w", err)
		}
		cursor, err := toolListCursor(result.NextCursor)
		if err != nil {
			return nil, err
		}
		// Retain the existing single-page decoder's count-limit diagnostics and
		// treatment of missing/null tools, as well as the original raw bytes.
		if page == 0 && cursor == "" {
			return raw, nil
		}
		if result.Tools == nil {
			return nil, fmt.Errorf("paginated tools/list result must contain a tools array")
		}
		if len(tools)+len(result.Tools) > maxDiscoveredTools {
			return nil, fmt.Errorf("paginated tools/list exceeds %d-tool limit", maxDiscoveredTools)
		}
		tools = append(tools, result.Tools...)
		if cursor == "" {
			return mergeToolPages(tools), nil
		}
		if _, duplicate := seen[cursor]; duplicate {
			return nil, fmt.Errorf("tools/list cursor repeated")
		}
		seen[cursor] = struct{}{}
		params = map[string]string{"cursor": cursor}
	}
	return nil, fmt.Errorf("tools/list exceeds %d-page limit", maxToolsListPages)
}

func toolListCursor(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var cursor *string
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor == nil {
		return "", fmt.Errorf("tools/list nextCursor must be a string")
	}
	return *cursor, nil
}

func mergeToolPages(tools []json.RawMessage) json.RawMessage {
	var result bytes.Buffer
	result.WriteString(`{"tools":[`)
	for index, tool := range tools {
		if index > 0 {
			result.WriteByte(',')
		}
		// Copy raw definitions without JSON re-encoding: escaping or numeric
		// normalization would change decoder policies and per-tool byte limits.
		result.Write(tool)
	}
	result.WriteString(`]}`)
	return result.Bytes()
}
