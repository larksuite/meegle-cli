// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// MultipartInfo describes how a chunked upload/download is partitioned.
// Exported so the prepare-upload basic command and the +upload-entire
// shortcut can share the parsed shape.
type MultipartInfo struct {
	PartCount int32
	PartSize  int64
	Need      []ChunkInfo
}

// ChunkInfo identifies a single byte range to upload.
type ChunkInfo struct {
	PartIndex int32
	StartByte int64
	EndByte   int64
}

// UploadPreprocess is the parsed result of the upload_file MCP tool call —
// everything needed to execute the subsequent HTTP POST(s) to object storage.
type UploadPreprocess struct {
	Sign        string
	UploadURL   string
	IsMultipart bool
	Multipart   *MultipartInfo
}

// DefaultUploadTool is the canonical MCP tool name for the upload preprocess.
// Exposed so callers (and tests) can reference the default without importing
// magic strings; sibling-based shortcuts inherit the actual name from the
// basic prepare-upload command's HandlerRef and pass it explicitly.
const DefaultUploadTool = "upload_file"

// PreprocessUpload validates input, calls the upload preprocess MCP tool, and
// parses the response into UploadPreprocess. The raw *MCPResponse is also
// returned so callers that need the original Data (e.g. for direct emit)
// don't pay a re-parse.
//
// toolName is the MCP tool name to call. Empty string falls back to
// DefaultUploadTool. Shortcut commands inherit toolName from their sibling
// (prepare-upload) node's HandlerRef so a backend rename of the MCP tool
// doesn't require touching this package — the basic command's registration
// remains the single source of truth.
func PreprocessUpload(ctx context.Context, mcp MCPClient, toolName string, in UploadInput) (*MCPResponse, *UploadPreprocess, error) {
	if err := validateUploadInput(in); err != nil {
		return nil, nil, err
	}
	if toolName == "" {
		toolName = DefaultUploadTool
	}

	params := map[string]any{
		"project_key":   in.ProjectKey,
		"file_name":     in.Filename,
		"mime_type":     in.ContentType,
		"size":          in.FileSize,
		"resource_type": in.ResourceType,
	}
	// work_item_id takes precedence over work_item_type — id is strictly
	// more specific (uniquely identifies a workitem including its type),
	// so we only fall back to type when no id is available. This also
	// keeps the wire payload clean (single scope field) and makes the
	// agent-facing contract unambiguous: always pass id if you have one.
	switch {
	case in.WorkItemID != "":
		params["work_item_id"] = in.WorkItemID
	case in.WorkItemType != "":
		params["work_item_type"] = in.WorkItemType
	}
	if in.FieldKey != "" {
		params["field_key"] = in.FieldKey
	}

	resp, err := mcp.CallTool(ctx, toolName, params)
	if err != nil {
		return nil, nil, fmt.Errorf("%s preprocess failed: %w", toolName, err)
	}
	pre, err := parsePreProcessResponse(resp)
	if err != nil {
		return resp, nil, err
	}
	return resp, pre, nil
}

// ExecuteUpload POSTs bytes to object storage using a previously-obtained
// UploadPreprocess. It does not call MCP. Input is assumed pre-validated by
// PreprocessUpload — direct callers (tests / advanced flows) must validate
// themselves if they bypass that.
func ExecuteUpload(ctx context.Context, doer HTTPDoer, pre *UploadPreprocess, in UploadInput) (*UploadResult, error) {
	if pre == nil {
		return nil, fmt.Errorf("upload preprocess result is nil")
	}

	var (
		final *uploadPartResponse
		err   error
	)
	if !pre.IsMultipart {
		final, err = uploadOneShot(ctx, doer, pre.UploadURL, pre.Sign, in)
	} else {
		final, err = uploadMultipart(ctx, doer, pre.UploadURL, pre.Sign, in, pre.Multipart)
	}
	if err != nil {
		return nil, err
	}
	if final == nil || final.FileToken == "" {
		raw := ""
		if final != nil {
			raw = truncateBody(final.raw)
		}
		return nil, fmt.Errorf("upload completed but file_token was empty (raw response: %s)", raw)
	}

	return &UploadResult{
		FileToken: final.FileToken,
		FileURL:   final.FileURL,
		Name:      in.Filename,
		Size:      in.FileSize,
		MimeType:  in.ContentType,
	}, nil
}

func validateUploadInput(in UploadInput) error {
	if err := ValidateResourceType(in.ResourceType); err != nil {
		return err
	}
	if RequiresFieldKey(in.ResourceType) && in.FieldKey == "" {
		return fmt.Errorf("--field-key is required for --resource-type %d", in.ResourceType)
	}
	if in.ProjectKey == "" {
		return fmt.Errorf("--project-key is required")
	}
	// Backend requires at least one of these: work_item_id targets an
	// existing workitem (update path); work_item_type targets a workitem
	// that doesn't exist yet (create-with-attachment path).
	if in.WorkItemID == "" && in.WorkItemType == "" {
		return fmt.Errorf("one of --work-item-id or --work-item-type is required " +
			"(use --work-item-id to attach to an existing workitem, or --work-item-type " +
			"when preparing an attachment for a workitem you're about to create)")
	}
	if in.File == nil {
		return fmt.Errorf("File reader is nil")
	}
	if in.FileSize <= 0 {
		return fmt.Errorf("FileSize must be > 0, got %d", in.FileSize)
	}
	if in.Filename == "" {
		return fmt.Errorf("Filename is empty")
	}
	if in.ContentType == "" {
		return fmt.Errorf("ContentType is empty")
	}
	return nil
}

func parsePreProcessResponse(resp *MCPResponse) (*UploadPreprocess, error) {
	if resp == nil {
		return nil, fmt.Errorf("upload_file returned nil response")
	}
	// The MCP text content is JSON-parsed into any; the simplest type-safe
	// extraction is to re-marshal and decode into a tagged struct.
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("re-marshal upload_file response: %w", err)
	}
	var aux struct {
		Sign        string `json:"sign"`
		UploadURL   string `json:"upload_url"`
		IsMultipart bool   `json:"is_multipart"`
		Multipart   *struct {
			PartCount int32 `json:"part_count"`
			PartSize  int64 `json:"part_size"`
			Need      []struct {
				PartIndex int32 `json:"part_index"`
				StartByte int64 `json:"start_byte"`
				EndByte   int64 `json:"end_byte"`
			} `json:"need"`
		} `json:"multipart"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("parse upload_file response: %w", err)
	}
	if aux.UploadURL == "" {
		return nil, fmt.Errorf("upload_file response missing upload_url")
	}
	out := &UploadPreprocess{
		Sign:        aux.Sign,
		UploadURL:   aux.UploadURL,
		IsMultipart: aux.IsMultipart,
	}
	if aux.Multipart != nil {
		mp := &MultipartInfo{PartCount: aux.Multipart.PartCount, PartSize: aux.Multipart.PartSize}
		for _, c := range aux.Multipart.Need {
			mp.Need = append(mp.Need, ChunkInfo{PartIndex: c.PartIndex, StartByte: c.StartByte, EndByte: c.EndByte})
		}
		out.Multipart = mp
	}
	if out.IsMultipart && (out.Multipart == nil || out.Multipart.PartCount <= 0) {
		return nil, fmt.Errorf("upload_file reports is_multipart=true but multipart info is missing or invalid")
	}
	return out, nil
}

// uploadPartResponse mirrors UploadFileByBinaryOutput from the backend. Also
// stores the raw HTTP body so callers can surface it in diagnostic error
// messages when the parsed token is missing.
type uploadPartResponse struct {
	FileToken     string  `json:"file_token"`
	FileURL       string  `json:"file_url"`
	UnUploadIndex []int32 `json:"un_upload_index"`
	raw           []byte  `json:"-"`
}

// parseUploadPartResponse unwraps the backend HTTP body. Meego's Gin handlers
// typically wrap business payloads in {"code","data","msg"}; the raw handler
// output may also appear flat. Trying the envelope first and falling back to
// flat keeps the parser robust to either shape without silently returning a
// zero-valued token when the backend inserts a wrapper.
//
// If the envelope carries a non-zero `code`, surface it as an error — this
// is the "HTTP 200 but business-level failure" case (e.g. expired sign)
// that would otherwise look like a successful upload with an empty token.
func parseUploadPartResponse(body []byte) (*uploadPartResponse, error) {
	// Envelope shape detection
	var env struct {
		Code *int            `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		if env.Code != nil && *env.Code != 0 {
			return nil, fmt.Errorf("server returned code=%d msg=%q body=%s",
				*env.Code, env.Msg, truncateBody(body))
		}
		if len(env.Data) > 0 && !bytes.Equal(bytes.TrimSpace(env.Data), []byte("null")) {
			var out uploadPartResponse
			if err := json.Unmarshal(env.Data, &out); err == nil &&
				(out.FileToken != "" || len(out.UnUploadIndex) > 0) {
				out.raw = body
				return &out, nil
			}
		}
	}

	// Flat shape: {file_token, file_url, un_upload_index}
	var out uploadPartResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse upload part response (body=%s): %w",
			truncateBody(body), err)
	}
	out.raw = body
	return &out, nil
}

// truncateBody keeps error messages readable if the server happens to dump a
// large HTML/JSON blob on failure paths.
func truncateBody(body []byte) string {
	const maxLen = 2048
	s := strings.TrimSpace(string(body))
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…(truncated)"
}

func uploadOneShot(ctx context.Context, doer HTTPDoer, urlTemplate, sign string, in UploadInput) (*uploadPartResponse, error) {
	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to start: %w", err)
	}
	url := strings.ReplaceAll(urlTemplate, partNumberPlaceholder, "0")
	return doUploadPart(ctx, doer, url, sign, in.ContentType, in.File, in.FileSize)
}

func uploadMultipart(ctx context.Context, doer HTTPDoer, urlTemplate, sign string, in UploadInput, mp *MultipartInfo) (*uploadPartResponse, error) {
	var final *uploadPartResponse
	for _, chunk := range mp.Need {
		if _, err := in.File.Seek(chunk.StartByte, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek chunk %d start: %w", chunk.PartIndex, err)
		}
		bodySize := chunkBodySize(chunk, mp.PartCount, in.FileSize)
		if bodySize <= 0 {
			return nil, fmt.Errorf("chunk %d computed bodySize=%d <= 0", chunk.PartIndex, bodySize)
		}
		url := strings.ReplaceAll(urlTemplate, partNumberPlaceholder, strconv.Itoa(int(chunk.PartIndex)))
		resp, err := doUploadPart(ctx, doer, url, sign, in.ContentType, in.File, bodySize)
		if err != nil {
			return nil, fmt.Errorf("upload chunk %d: %w", chunk.PartIndex, err)
		}
		// Backend contract: only the last chunk's response carries file_token.
		if chunk.PartIndex == mp.PartCount-1 {
			if len(resp.UnUploadIndex) > 0 {
				return nil, fmt.Errorf("upload incomplete after last chunk; un_upload_index=%v", resp.UnUploadIndex)
			}
			final = resp
		}
	}
	if final == nil {
		return nil, fmt.Errorf("multipart upload finished without receiving the final chunk (expected part_index=%d in need[])", mp.PartCount-1)
	}
	return final, nil
}

// chunkBodySize returns the number of bytes to POST for a given chunk.
// Backend contract: the [start_byte, end_byte] range is INCLUSIVE on both
// ends, so chunk size is (end - start + 1). The last chunk's end_byte has
// historically been unreliable — for it, read from start_byte to file end
// instead, which lines up with the inclusive math when end_byte is correct.
func chunkBodySize(c ChunkInfo, partCount int32, fileSize int64) int64 {
	if c.PartIndex == partCount-1 {
		return fileSize - c.StartByte
	}
	return c.EndByte - c.StartByte + 1
}

func doUploadPart(ctx context.Context, doer HTTPDoer, url, sign, contentType string, reader io.Reader, size int64) (*uploadPartResponse, error) {
	body := io.LimitReader(reader, size)
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	req.Header.Set(signHeader, sign)
	req.Header.Set("Content-Type", contentType)

	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read upload part response body: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upload part HTTP %d: %s", resp.StatusCode, truncateBody(rawBody))
	}
	return parseUploadPartResponse(rawBody)
}

// DefaultContentType infers the MIME type from a file path's extension,
// falling back to application/octet-stream. mime.TypeByExtension can include
// "; charset=utf-8" — we strip it so the Content-Type header matches the
// mime_type sent to MCP.
//
// Used by the +upload shortcut when --content-type isn't supplied.
func DefaultContentType(path string) string {
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		return "application/octet-stream"
	}
	if i := strings.Index(ct, ";"); i > 0 {
		return strings.TrimSpace(ct[:i])
	}
	return ct
}
