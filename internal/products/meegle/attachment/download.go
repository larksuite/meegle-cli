// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ErrFileSignMissing and ErrFileSignMismatch report failures of the file
// signature integrity guard performed during +download (see verifySign). They
// are exported so the register layer can errors.Is them — across the multipart
// wrap — and map them to a user-facing CLIENT_* error with a retry hint.
var (
	ErrFileSignMissing  = errors.New("download response missing file signature header")
	ErrFileSignMismatch = errors.New("download response file signature does not match the requested signature")
)

// DownloadPreprocess is the parsed result of the get_download_url MCP tool —
// everything needed to execute the subsequent HTTP GET(s) from object storage.
type DownloadPreprocess struct {
	DownloadURL string
	Sign        string
	IsMultipart bool
	Multipart   *MultipartInfo
}

// DefaultDownloadTool is the canonical MCP tool name for the download
// preprocess. Sibling-based shortcuts inherit the actual name from the basic
// prepare-download command's HandlerRef and pass it explicitly.
const DefaultDownloadTool = "get_download_url"

// checkOutputAvailable enforces the --overwrite gate before any bytes are
// written. Called from ExecuteDownload.
func checkOutputAvailable(in DownloadInput) error {
	if in.Output == "" {
		return nil
	}
	if _, err := os.Stat(in.Output); err == nil {
		if !in.Overwrite {
			return fmt.Errorf("output path %q already exists (pass --overwrite to replace)", in.Output)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat output path: %w", err)
	}
	return nil
}

// PreprocessDownload validates input, calls the download preprocess MCP tool,
// and parses the response into DownloadPreprocess. The raw *MCPResponse is
// also returned so callers that need the original Data don't pay a re-parse.
//
// toolName is the MCP tool name to call; empty falls back to DefaultDownloadTool.
// Sibling-based shortcuts inherit toolName from the prepare-download node's
// HandlerRef so this package doesn't have to be touched when the backend
// renames the underlying tool.
//
// Output-path checks (existing file / overwrite) live in ExecuteDownload so
// that pure preprocess callers don't need a destination path.
func PreprocessDownload(ctx context.Context, mcp MCPClient, toolName string, in DownloadInput) (*MCPResponse, *DownloadPreprocess, error) {
	if err := validateDownloadPreprocessInput(in); err != nil {
		return nil, nil, err
	}
	if toolName == "" {
		toolName = DefaultDownloadTool
	}
	resp, err := mcp.CallTool(ctx, toolName, map[string]any{
		"project_key":  in.ProjectKey,
		"work_item_id": in.WorkItemID,
		"file_url":     in.FileURL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s failed: %w", toolName, err)
	}
	pre, err := parseDownloadURLResponse(resp)
	if err != nil {
		return resp, nil, err
	}
	return resp, pre, nil
}

// ExecuteDownload streams bytes from object storage to in.Output via a
// `.partial` temp file + atomic rename, so a failed download never leaves a
// half-written file at the user-visible path. Input is assumed pre-validated
// by PreprocessDownload — direct callers must validate themselves if they
// bypass that.
func ExecuteDownload(ctx context.Context, doer HTTPDoer, pre *DownloadPreprocess, in DownloadInput) (*DownloadResult, error) {
	if pre == nil {
		return nil, fmt.Errorf("download preprocess result is nil")
	}
	if in.Output == "" {
		return nil, fmt.Errorf("--output is required")
	}
	if err := checkOutputAvailable(in); err != nil {
		return nil, err
	}

	partialPath := in.Output + ".partial"
	partial, err := os.Create(partialPath)
	if err != nil {
		return nil, fmt.Errorf("create partial file: %w", err)
	}
	cleanup := func() {
		_ = partial.Close()
		_ = os.Remove(partialPath)
	}

	var totalSize int64
	var fileName, mimeType string

	if !pre.IsMultipart {
		url := strings.ReplaceAll(pre.DownloadURL, partNumberPlaceholder, "0")
		meta, err := doDownloadPart(ctx, doer, url, pre.Sign, in.Headers, partial)
		if err != nil {
			cleanup()
			return nil, err
		}
		totalSize = meta.size
		fileName = meta.name
		mimeType = meta.mimeType
	} else {
		for i := int32(0); i < pre.Multipart.PartCount; i++ {
			url := strings.ReplaceAll(pre.DownloadURL, partNumberPlaceholder, strconv.Itoa(int(i)))
			meta, err := doDownloadPart(ctx, doer, url, pre.Sign, in.Headers, partial)
			if err != nil {
				cleanup()
				return nil, fmt.Errorf("download chunk %d: %w", i, err)
			}
			if fileName == "" {
				fileName = meta.name
			}
			if mimeType == "" {
				mimeType = meta.mimeType
			}
			totalSize += meta.size
		}
	}

	if err := partial.Close(); err != nil {
		_ = os.Remove(partialPath)
		return nil, fmt.Errorf("close partial file: %w", err)
	}
	if err := os.Rename(partialPath, in.Output); err != nil {
		_ = os.Remove(partialPath)
		return nil, fmt.Errorf("rename partial to output: %w", err)
	}

	return &DownloadResult{
		Path:     in.Output,
		Size:     totalSize,
		Name:     fileName,
		MimeType: mimeType,
	}, nil
}

// validateDownloadPreprocessInput covers everything PreprocessDownload needs.
// ExecuteDownload checks Output separately so a preprocess-only call doesn't
// require a destination path.
func validateDownloadPreprocessInput(in DownloadInput) error {
	if in.ProjectKey == "" {
		return fmt.Errorf("--project-key is required")
	}
	if in.WorkItemID == "" {
		return fmt.Errorf("--work-item-id is required")
	}
	if in.FileURL == "" {
		return fmt.Errorf("<file-url> is required")
	}
	return nil
}

func parseDownloadURLResponse(resp *MCPResponse) (*DownloadPreprocess, error) {
	if resp == nil {
		return nil, fmt.Errorf("get_download_url returned nil response")
	}
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("re-marshal get_download_url response: %w", err)
	}
	var aux struct {
		DownloadURL string `json:"download_url"`
		Sign        string `json:"sign"`
		IsMultipart bool   `json:"is_multipart"`
		Multipart   *struct {
			PartCount int32 `json:"part_count"`
			PartSize  int64 `json:"part_size"`
		} `json:"multipart"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("parse get_download_url response: %w", err)
	}
	if aux.DownloadURL == "" {
		return nil, fmt.Errorf("get_download_url response missing download_url")
	}
	out := &DownloadPreprocess{DownloadURL: aux.DownloadURL, Sign: aux.Sign, IsMultipart: aux.IsMultipart}
	if aux.Multipart != nil {
		out.Multipart = &MultipartInfo{PartCount: aux.Multipart.PartCount, PartSize: aux.Multipart.PartSize}
	}
	if out.IsMultipart && (out.Multipart == nil || out.Multipart.PartCount <= 0) {
		return nil, fmt.Errorf("get_download_url reports is_multipart=true but multipart info is missing or invalid")
	}
	return out, nil
}

type partMeta struct {
	size     int64
	name     string
	mimeType string
}

func doDownloadPart(ctx context.Context, doer HTTPDoer, url, sign string, extraHeaders map[string]string, sink io.Writer) (*partMeta, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	// Caller-supplied headers first (e.g. environment-routing headers); the
	// signature header is set last so it can never be clobbered.
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set(signHeader, sign)
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download part HTTP %d", resp.StatusCode)
	}
	// Integrity guard before reading the body: abort if the echoed signature
	// doesn't match the one we requested, so a mis-served object is never
	// written.
	if err := verifySign(resp.Header.Get(signHeader), sign); err != nil {
		return nil, err
	}
	n, err := io.Copy(sink, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("write part body: %w", err)
	}
	return &partMeta{
		size:     n,
		name:     filenameFromHeader(resp.Header.Get("Content-Disposition")),
		mimeType: resp.Header.Get("Content-Type"),
	}, nil
}

// verifySign enforces the CDN-cache integrity guard: the backend echoes the
// per-request signature on every download response, and the +download flow
// only writes bytes whose echoed signature matches the one we requested. Both
// a missing header (can't verify) and a mismatch are fail-closed — they abort
// the download so a mis-served object never reaches disk. The comparison is
// case-insensitive, matching the header's case-insensitive contract.
func verifySign(got, want string) error {
	got = strings.TrimSpace(got)
	if got == "" {
		return fmt.Errorf("%w (%s)", ErrFileSignMissing, signHeader)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w: response signature %q, requested %q", ErrFileSignMismatch, got, want)
	}
	return nil
}

// filenameFromHeader extracts the filename from a Content-Disposition header.
// Returns empty if the header is missing or malformed — name metadata is
// best-effort decoration, not a hard requirement.
func filenameFromHeader(cd string) string {
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}
	if fn := params["filename"]; fn != "" {
		return fn
	}
	if fn := params["filename*"]; fn != "" {
		// RFC 5987: "UTF-8''name.ext" — strip the "<charset>''" prefix when present.
		if i := strings.Index(fn, "''"); i >= 0 {
			return fn[i+2:]
		}
		return fn
	}
	return ""
}
