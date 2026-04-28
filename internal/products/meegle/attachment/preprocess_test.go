// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package attachment_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
)

// TestPreprocessUpload_BuildsMCPParamsAndParsesResponse covers the contract
// the +upload shortcut depends on: Preprocess sends the MCP tool the
// expected snake-case params (built from UploadInput), and exposes the parsed
// preprocess struct alongside the raw MCPResponse.
func TestPreprocessUpload_BuildsMCPParamsAndParsesResponse(t *testing.T) {
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "S",
		"is_multipart": true,
		"upload_url":   "https://obj/up/15/:part_number",
		"multipart": map[string]any{
			"part_count": 2, "part_size": 30,
			"need": []any{
				// Backend ranges are INCLUSIVE [start, end] — each chunk
				// covers part_size = end - start + 1 bytes.
				map[string]any{"part_index": 0, "start_byte": 0, "end_byte": 29},
				map[string]any{"part_index": 1, "start_byte": 30, "end_byte": 59},
			},
		},
	}}}
	in := attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField, ProjectKey: "P", WorkItemID: "W", FieldKey: "fk",
		File:     bytes.NewReader([]byte("xxxxxx")),
		FileSize: 6, Filename: "a.txt", ContentType: "text/plain",
	}

	raw, pre, err := attachment.PreprocessUpload(context.Background(), mcp, "", in)
	if err != nil {
		t.Fatalf("PreprocessUpload: %v", err)
	}
	if raw == nil || raw.Data == nil {
		t.Fatal("expected raw MCPResponse to be returned")
	}
	if !pre.IsMultipart || pre.Multipart == nil || pre.Multipart.PartCount != 2 {
		t.Errorf("preprocess parse incorrect: %+v", pre)
	}
	if pre.UploadURL == "" || pre.Sign != "S" {
		t.Errorf("preprocess fields wrong: %+v", pre)
	}
	if len(mcp.calls) != 1 || mcp.calls[0].name != "upload_file" {
		t.Fatalf("MCP calls=%+v", mcp.calls)
	}
	p := mcp.calls[0].params
	if p["resource_type"] != 15 {
		t.Errorf("resource_type=%v, want 15 for attachment-field", p["resource_type"])
	}
	if p["work_item_id"] != "W" {
		t.Errorf("work_item_id=%v", p["work_item_id"])
	}
	if p["field_key"] != "fk" {
		t.Errorf("field_key=%v", p["field_key"])
	}
}

func TestPreprocessUpload_ValidatesInput(t *testing.T) {
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{}}}
	_, _, err := attachment.PreprocessUpload(context.Background(), mcp, "", attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField, // missing ProjectKey + ID + file
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if len(mcp.calls) != 0 {
		t.Errorf("MCP must not be called when validation fails; got %d calls", len(mcp.calls))
	}
}

// TestPreprocessUpload_HonorsCustomToolName proves the toolName parameter
// reaches the MCP layer. This is what enables sibling-based shortcuts to
// inherit the tool name from the basic command's HandlerRef instead of
// hardcoding "upload_file" in the package.
func TestPreprocessUpload_HonorsCustomToolName(t *testing.T) {
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": "u/:part_number",
	}}}
	_, _, err := attachment.PreprocessUpload(context.Background(), mcp, "meego_upload_file_v2", attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("PreprocessUpload: %v", err)
	}
	if len(mcp.calls) != 1 || mcp.calls[0].name != "meego_upload_file_v2" {
		t.Errorf("expected tool name 'meego_upload_file_v2', got %+v", mcp.calls)
	}
}

// TestExecuteUpload_OneShot_DoesNotCallMCP proves Execute is HTTP-only.
func TestExecuteUpload_OneShot_DoesNotCallMCP(t *testing.T) {
	var gotSign string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSign = r.Header.Get("X-Meego-File-Sign")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"file_token":"TOK","file_url":"u","un_upload_index":[]}`))
	}))
	defer ts.Close()

	pre := &attachment.UploadPreprocess{
		Sign: "SIGN", IsMultipart: false,
		UploadURL: ts.URL + "/up/15/:part_number",
	}
	body := []byte("hello")
	in := attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField, ProjectKey: "P", WorkItemID: "W", FieldKey: "f",
		File:     bytes.NewReader(body),
		FileSize: int64(len(body)), Filename: "a.txt", ContentType: "text/plain",
	}
	res, err := attachment.ExecuteUpload(context.Background(), http.DefaultClient, pre, in)
	if err != nil {
		t.Fatalf("ExecuteUpload: %v", err)
	}
	if res.FileToken != "TOK" {
		t.Errorf("FileToken=%q, want TOK", res.FileToken)
	}
	if gotSign != "SIGN" {
		t.Errorf("X-Meego-File-Sign=%q, want SIGN", gotSign)
	}
}

func TestExecuteUpload_NilPreprocess(t *testing.T) {
	_, err := attachment.ExecuteUpload(context.Background(), http.DefaultClient, nil, attachment.UploadInput{})
	if err == nil || !strings.Contains(err.Error(), "preprocess result is nil") {
		t.Errorf("want nil-preprocess error, got %v", err)
	}
}

// TestPreprocessDownload_BuildsParams covers that the prepare-download basic
// command path forwards exactly the fields the MCP tool expects.
func TestPreprocessDownload_BuildsParams(t *testing.T) {
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "DS",
		"is_multipart": false,
		"download_url": "https://obj/dl/:part_number",
	}}}
	raw, pre, err := attachment.PreprocessDownload(context.Background(), mcp, "", attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "meego://abc",
	})
	if err != nil {
		t.Fatalf("PreprocessDownload: %v", err)
	}
	if raw == nil || raw.Data == nil {
		t.Fatal("expected raw MCPResponse to be returned")
	}
	if pre.DownloadURL == "" || pre.Sign != "DS" {
		t.Errorf("preprocess fields wrong: %+v", pre)
	}
	if len(mcp.calls) != 1 || mcp.calls[0].name != "get_download_url" {
		t.Fatalf("MCP calls=%+v", mcp.calls)
	}
	p := mcp.calls[0].params
	if p["file_url"] != "meego://abc" {
		t.Errorf("file_url=%v", p["file_url"])
	}
}

func TestPreprocessDownload_DoesNotRequireOutput(t *testing.T) {
	// Pure preprocess callers (the prepare-download basic command) don't have
	// a destination path — Output validation belongs in ExecuteDownload.
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "download_url": "u/:part_number",
	}}}
	_, _, err := attachment.PreprocessDownload(context.Background(), mcp, "", attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x", // Output deliberately empty
	})
	if err != nil {
		t.Errorf("preprocess should accept empty Output: %v", err)
	}
}

func TestExecuteDownload_RequiresOutput(t *testing.T) {
	pre := &attachment.DownloadPreprocess{DownloadURL: "u/:part_number", Sign: "S"}
	_, err := attachment.ExecuteDownload(context.Background(), http.DefaultClient, pre, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x", // no Output
	})
	if err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Errorf("want output-required error, got %v", err)
	}
}

// TestExecuteDownload_OneShot_NoMCP proves Execute is HTTP-only — it never
// touches the MCP client even if one were available.
func TestExecuteDownload_OneShot_NoMCP(t *testing.T) {
	payload := []byte("file contents")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	pre := &attachment.DownloadPreprocess{
		DownloadURL: ts.URL + "/:part_number",
		Sign:        "S",
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "got.bin")
	res, err := attachment.ExecuteDownload(context.Background(), http.DefaultClient, pre, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x", Output: out,
	})
	if err != nil {
		t.Fatalf("ExecuteDownload: %v", err)
	}
	if res.Size != int64(len(payload)) {
		t.Errorf("Size=%d, want %d", res.Size, len(payload))
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, payload) {
		t.Errorf("body mismatch")
	}
}

func TestPreprocessUpload_MCPErrorWrapped(t *testing.T) {
	mcp := &fakeMCP{err: errors.New("boom")}
	_, _, err := attachment.PreprocessUpload(context.Background(), mcp, "", attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err == nil || !strings.Contains(err.Error(), "upload_file preprocess failed") {
		t.Errorf("want wrapped preprocess error, got %v", err)
	}
}

// TestDefaultContentType migrated from commands/attachment_test.go since the
// helper now lives in the attachment package.
func TestDefaultContentType(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"a.pdf", "application/pdf"},
		{"x.unknown-ext-xyz", "application/octet-stream"},
		{"no-extension", "application/octet-stream"},
	}
	for _, c := range cases {
		got := attachment.DefaultContentType(c.path)
		if got != c.want {
			t.Errorf("DefaultContentType(%q) = %q, want %q", c.path, got, c.want)
		}
	}

	// mime.TypeByExtension for .txt includes "; charset=utf-8" on some
	// systems; DefaultContentType must strip it so Content-Type == mime_type
	// exactly. Don't hardcode the raw string (platform-dependent); only
	// assert the charset is gone and the prefix is text/plain.
	got := attachment.DefaultContentType("note.txt")
	if strings.Contains(got, ";") {
		t.Errorf("DefaultContentType(\"note.txt\") = %q, should have no charset suffix", got)
	}
	if !strings.HasPrefix(got, "text/plain") {
		t.Errorf("DefaultContentType(\"note.txt\") = %q, expected text/plain prefix", got)
	}
}
