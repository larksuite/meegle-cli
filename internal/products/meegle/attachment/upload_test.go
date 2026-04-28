// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package attachment_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
)

// fakeMCP records every tool call and returns a canned response.
type fakeMCP struct {
	mu       sync.Mutex
	calls    []fakeMCPCall
	response *attachment.MCPResponse
	err      error
}

type fakeMCPCall struct {
	name   string
	params map[string]any
}

func (f *fakeMCP) CallTool(_ context.Context, name string, params map[string]any) (*attachment.MCPResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeMCPCall{name: name, params: params})
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

// uploadFull runs PreprocessUpload+ExecuteUpload back-to-back the way the
// shortcut command does in production. Test-only convenience; mirrors what the
// previous UploadFlow wrapper did so test bodies don't have to re-stitch the
// pair on every call site.
func uploadFull(t *testing.T, mcp attachment.MCPClient, doer attachment.HTTPDoer, in attachment.UploadInput) (*attachment.UploadResult, error) {
	t.Helper()
	_, pre, err := attachment.PreprocessUpload(context.Background(), mcp, "", in)
	if err != nil {
		return nil, err
	}
	return attachment.ExecuteUpload(context.Background(), doer, pre, in)
}

func TestValidateResourceType(t *testing.T) {
	for _, rt := range []int{
		attachment.ResourceTypeAttachmentField,
		attachment.ResourceTypeRichTextFieldImage,
		attachment.ResourceTypeCommentAttachment,
		attachment.ResourceTypeCommentImage,
	} {
		if err := attachment.ValidateResourceType(rt); err != nil {
			t.Errorf("rt=%d: unexpected error %v", rt, err)
		}
	}
	if err := attachment.ValidateResourceType(99); err == nil {
		t.Error("expected error for unknown resource_type")
	}
}

func TestRequiresFieldKey(t *testing.T) {
	if !attachment.RequiresFieldKey(attachment.ResourceTypeAttachmentField) {
		t.Error("resource_type=15 should require --field-key")
	}
	if !attachment.RequiresFieldKey(attachment.ResourceTypeRichTextFieldImage) {
		t.Error("resource_type=16 should require --field-key")
	}
	if attachment.RequiresFieldKey(attachment.ResourceTypeCommentAttachment) {
		t.Error("resource_type=13 should NOT require --field-key")
	}
	if attachment.RequiresFieldKey(attachment.ResourceTypeCommentImage) {
		t.Error("resource_type=14 should NOT require --field-key")
	}
}

func TestUploadFlow_OneShot_SendsSignAndBody(t *testing.T) {
	var (
		gotSign        string
		gotContentType string
		gotBody        []byte
		gotPath        string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSign = r.Header.Get("X-Meego-File-Sign")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"file_token":"TOK123","file_url":"https://cdn/abc","un_upload_index":[]}`))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "SIGN-XYZ",
		"is_multipart": false,
		"upload_url":   ts.URL + "/goapi/v5/platform/file/stream/mcp/upload/15/:part_number",
	}}}

	body := []byte("hello meegle")
	result, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField,
		ProjectKey:   "P1",
		WorkItemID:   "W1",
		FieldKey:     "files_field",
		File:         bytes.NewReader(body),
		FileSize:     int64(len(body)),
		Filename:     "a.txt",
		ContentType:  "text/plain",
	})
	if err != nil {
		t.Fatalf("uploadFull: %v", err)
	}
	if result.FileToken != "TOK123" {
		t.Errorf("FileToken=%q, want TOK123", result.FileToken)
	}
	if result.FileURL != "https://cdn/abc" {
		t.Errorf("FileURL=%q", result.FileURL)
	}
	if gotSign != "SIGN-XYZ" {
		t.Errorf("X-Meego-File-Sign=%q, want SIGN-XYZ", gotSign)
	}
	if gotContentType != "text/plain" {
		t.Errorf("Content-Type=%q, want text/plain", gotContentType)
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("body=%q, want %q", gotBody, body)
	}
	if !strings.HasSuffix(gotPath, "/upload/15/0") {
		t.Errorf("path=%q, want suffix /upload/15/0", gotPath)
	}
	// Verify MCP received the right call
	if len(mcp.calls) != 1 || mcp.calls[0].name != "upload_file" {
		t.Fatalf("MCP calls=%+v", mcp.calls)
	}
	p := mcp.calls[0].params
	if p["resource_type"] != 15 {
		t.Errorf("resource_type=%v", p["resource_type"])
	}
	if p["field_key"] != "files_field" {
		t.Errorf("field_key=%v", p["field_key"])
	}
	if p["file_name"] != "a.txt" {
		t.Errorf("file_name=%v", p["file_name"])
	}
}

func TestUploadFlow_Multipart_LastChunkUsesFileSizeNotEndByte(t *testing.T) {
	// File is 100 bytes. Backend ranges are INCLUSIVE [start, end]; the last
	// chunk's end_byte is intentionally too small (79 instead of 99) to
	// exercise the fileSize fallback. ExecuteUpload must upload the full 40
	// bytes [60..100) for the last chunk, not the 20 bytes that end_byte
	// claims.
	const fileSize = 100
	fileBytes := bytes.Repeat([]byte("x"), fileSize)

	type recorded struct {
		path string
		body []byte
	}
	var (
		mu   sync.Mutex
		seen []recorded
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, recorded{path: r.URL.Path, body: body})
		parts := strings.Split(r.URL.Path, "/")
		lastPart := parts[len(parts)-1]
		mu.Unlock()

		// Only the LAST part (part_number=2) returns file_token per backend contract.
		w.WriteHeader(200)
		if lastPart == "2" {
			_, _ = w.Write([]byte(`{"file_token":"FINAL-TOK","file_url":"https://cdn/zz","un_upload_index":[]}`))
		} else {
			_, _ = w.Write([]byte(`{"file_token":"","file_url":"","un_upload_index":[]}`))
		}
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "S",
		"is_multipart": true,
		"upload_url":   ts.URL + "/up/15/:part_number",
		"multipart": map[string]any{
			"part_count": 3,
			"part_size":  30,
			"need": []any{
				map[string]any{"part_index": 0, "start_byte": 0, "end_byte": 29},
				map[string]any{"part_index": 1, "start_byte": 30, "end_byte": 59},
				// Deliberately wrong end_byte on the last chunk (should be 99):
				map[string]any{"part_index": 2, "start_byte": 60, "end_byte": 79},
			},
		},
	}}}

	result, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment,
		ProjectKey:   "P", WorkItemID: "W",
		File:        bytes.NewReader(fileBytes),
		FileSize:    fileSize,
		Filename:    "big.bin",
		ContentType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("uploadFull: %v", err)
	}
	if result.FileToken != "FINAL-TOK" {
		t.Errorf("FileToken=%q", result.FileToken)
	}
	if len(seen) != 3 {
		t.Fatalf("chunks uploaded=%d, want 3", len(seen))
	}
	// Chunks 0 and 1: 30 bytes each per end_byte - start_byte
	if len(seen[0].body) != 30 {
		t.Errorf("chunk 0 size=%d, want 30", len(seen[0].body))
	}
	if len(seen[1].body) != 30 {
		t.Errorf("chunk 1 size=%d, want 30", len(seen[1].body))
	}
	// Chunk 2: must be 40 bytes (file_size - start_byte), NOT 20 bytes
	if len(seen[2].body) != 40 {
		t.Errorf("chunk 2 size=%d, want 40 (last chunk should read to file end, ignoring end_byte=80)", len(seen[2].body))
	}
}

func TestUploadFlow_MissingUploadURL(t *testing.T) {
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "S",
		"is_multipart": false,
		// upload_url missing
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "application/octet-stream",
	})
	if err == nil || !strings.Contains(err.Error(), "missing upload_url") {
		t.Errorf("want missing upload_url error, got %v", err)
	}
}

func TestUploadFlow_MultipartIncompleteAfterFinalChunk(t *testing.T) {
	// Multipart final chunk returns non-empty un_upload_index — must surface an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every chunk including last returns un_upload_index=[5]
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"file_token":"tok","file_url":"u","un_upload_index":[5]}`))
	}))
	defer ts.Close()

	fileBytes := bytes.Repeat([]byte("x"), 60)
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": true, "upload_url": ts.URL + "/:part_number",
		"multipart": map[string]any{
			"part_count": 2, "part_size": 30,
			"need": []any{
				map[string]any{"part_index": 0, "start_byte": 0, "end_byte": 30},
				map[string]any{"part_index": 1, "start_byte": 30, "end_byte": 60},
			},
		},
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader(fileBytes),
		FileSize: int64(len(fileBytes)), Filename: "x", ContentType: "text/plain",
	})
	if err == nil || !strings.Contains(err.Error(), "un_upload_index") {
		t.Errorf("want un_upload_index error, got %v", err)
	}
}

func TestUploadFlow_ValidationErrors(t *testing.T) {
	baseInput := attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField, ProjectKey: "P", WorkItemID: "W",
		FieldKey: "f",
		File:     bytes.NewReader([]byte("x")), FileSize: 1, Filename: "a", ContentType: "text/plain",
	}
	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{}}}
	mutate := func(f func(*attachment.UploadInput)) attachment.UploadInput {
		in := baseInput
		f(&in)
		return in
	}

	cases := []struct {
		name string
		in   attachment.UploadInput
		want string
	}{
		{"missing field-key for resource-type=15", mutate(func(in *attachment.UploadInput) { in.FieldKey = "" }), "field-key"},
		{"bogus resource-type", mutate(func(in *attachment.UploadInput) { in.ResourceType = 99 }), "unknown --resource-type"},
		{"empty project-key", mutate(func(in *attachment.UploadInput) { in.ProjectKey = "" }), "project-key"},
		{
			"neither work-item-id nor work-item-type",
			mutate(func(in *attachment.UploadInput) { in.WorkItemID = ""; in.WorkItemType = "" }),
			"work-item-id or --work-item-type",
		},
		{"zero file size", mutate(func(in *attachment.UploadInput) { in.FileSize = 0 }), "FileSize"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := uploadFull(t, mcp, http.DefaultClient, c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestUploadFlow_WorkItemIDTakesPrecedenceOverType(t *testing.T) {
	// When both are provided, only work_item_id should go on the wire —
	// this keeps the agent-facing contract unambiguous ("always pass id
	// if you have one") and avoids the backend having to arbitrate.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"file_token":"T","file_url":"u","un_upload_index":[]}`))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField,
		ProjectKey:   "P",
		WorkItemID:   "W123", // both set
		WorkItemType: "story",
		FieldKey:     "fk",
		File:         bytes.NewReader([]byte("x")),
		FileSize:     1, Filename: "a.txt", ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("uploadFull: %v", err)
	}
	p := mcp.calls[0].params
	if p["work_item_id"] != "W123" {
		t.Errorf("work_item_id=%v, want W123", p["work_item_id"])
	}
	if _, present := p["work_item_type"]; present {
		t.Errorf("work_item_type should be dropped when work_item_id is present, got %v", p["work_item_type"])
	}
}

func TestUploadFlow_WorkItemType_SentInsteadOfID(t *testing.T) {
	// Create-workitem-with-attachment path: no work_item_id, only work_item_type.
	// Verify the MCP call carries work_item_type and NOT work_item_id.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"file_token":"T","file_url":"u","un_upload_index":[]}`))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	res, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeAttachmentField,
		ProjectKey:   "P",
		WorkItemType: "story",
		FieldKey:     "fk",
		File:         bytes.NewReader([]byte("x")),
		FileSize:     1, Filename: "a.txt", ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("uploadFull: %v", err)
	}
	if res.FileToken != "T" {
		t.Errorf("FileToken=%q", res.FileToken)
	}
	if len(mcp.calls) != 1 {
		t.Fatalf("MCP calls=%d, want 1", len(mcp.calls))
	}
	p := mcp.calls[0].params
	if _, present := p["work_item_id"]; present {
		t.Errorf("work_item_id should be omitted when empty, got %v", p["work_item_id"])
	}
	if p["work_item_type"] != "story" {
		t.Errorf("work_item_type=%v, want story", p["work_item_type"])
	}
}

func TestUploadFlow_MCPError(t *testing.T) {
	mcp := &fakeMCP{err: errors.New("boom")}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err == nil || !strings.Contains(err.Error(), "upload_file preprocess failed") {
		t.Errorf("want wrapped preprocess error, got %v", err)
	}
}

func TestUploadFlow_OneShot_EnvelopeResponse(t *testing.T) {
	// Backend wraps UploadFileByBinaryOutput inside {"code","data","msg"}.
	// Flow must unwrap transparently and still return file_token.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"file_token":"WRAPPED","file_url":"u","un_upload_index":[]}}`))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	res, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("uploadFull: %v", err)
	}
	if res.FileToken != "WRAPPED" {
		t.Errorf("FileToken=%q, want WRAPPED", res.FileToken)
	}
}

func TestUploadFlow_EnvelopeServerErrorSurfaces(t *testing.T) {
	// HTTP 200 + envelope code!=0 must return a descriptive error (not a
	// silent success with empty file_token).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":400,"msg":"sign expired","data":null}`))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err == nil {
		t.Fatal("expected error on code=400, got nil")
	}
	for _, want := range []string{"code=400", "sign expired"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q, got: %v", want, err)
		}
	}
}

func TestUploadFlow_EmptyTokenErrorIncludesRawBody(t *testing.T) {
	// Shape the parser can't recognize (e.g. totally unexpected wrapper):
	// the final error must include the raw body so users / future-us can
	// diagnose without adding new logging.
	rawBody := `{"unexpected":{"nested":{"file_token":"hidden-by-bad-shape"}},"status":"ok"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(rawBody))
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err == nil {
		t.Fatal("expected empty-token error, got nil")
	}
	if !strings.Contains(err.Error(), "file_token was empty") {
		t.Errorf("missing canonical error text: %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("error should include raw body for diagnosis; got: %v", err)
	}
}

func TestUploadFlow_ObjectStoreHTTP500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false, "upload_url": ts.URL + "/:part_number",
	}}}
	_, err := uploadFull(t, mcp, http.DefaultClient, attachment.UploadInput{
		ResourceType: attachment.ResourceTypeCommentAttachment, ProjectKey: "P", WorkItemID: "W",
		File:     bytes.NewReader([]byte("x")),
		FileSize: 1, Filename: "a", ContentType: "text/plain",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("want HTTP 500 error, got %v", err)
	}
}
