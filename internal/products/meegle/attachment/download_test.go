// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package attachment_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
)

// downloadFull runs PreprocessDownload+ExecuteDownload back-to-back the way
// the shortcut command does in production. Test-only convenience.
func downloadFull(t *testing.T, mcp attachment.MCPClient, doer attachment.HTTPDoer, in attachment.DownloadInput) (*attachment.DownloadResult, error) {
	t.Helper()
	_, pre, err := attachment.PreprocessDownload(context.Background(), mcp, "", in)
	if err != nil {
		return nil, err
	}
	return attachment.ExecuteDownload(context.Background(), doer, pre, in)
}

// stubDownloadMCP returns a fakeMCP whose CallTool yields a minimal valid
// preprocess response, so tests targeting ExecuteDownload-side gates (output
// existence, --overwrite) can reach Execute without setting up real MCP
// scaffolding. The download_url points at the supplied test server so callers
// can keep the multipart placeholder convention.
func stubDownloadMCP(downloadURL string) *fakeMCP {
	return &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "S",
		"is_multipart": false,
		"download_url": downloadURL,
	}}}
}

func TestDownloadFlow_OneShot(t *testing.T) {
	payload := []byte("file contents here")
	var gotSign string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSign = r.Header.Get("X-Meego-File-Sign")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="server.txt"`)
		w.WriteHeader(200)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "SIGN-DL",
		"is_multipart": false,
		"download_url": ts.URL + "/dl/15/:part_number",
	}}}

	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "out.txt")
	result, err := downloadFull(t, mcp, http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W",
		FileURL: "meego://abc",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("downloadFull: %v", err)
	}
	if result.Size != int64(len(payload)) {
		t.Errorf("Size=%d, want %d", result.Size, len(payload))
	}
	if result.Name != "server.txt" {
		t.Errorf("Name=%q, want server.txt", result.Name)
	}
	if result.MimeType != "text/plain" {
		t.Errorf("MimeType=%q", result.MimeType)
	}
	if gotSign != "SIGN-DL" {
		t.Errorf("sign=%q", gotSign)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("output bytes=%q, want %q", got, payload)
	}
	// .partial file should be gone after rename
	if _, err := os.Stat(output + ".partial"); !os.IsNotExist(err) {
		t.Errorf(".partial file still exists after success")
	}
	// MCP was called with expected params
	if len(mcp.calls) != 1 || mcp.calls[0].name != "get_download_url" {
		t.Fatalf("MCP calls=%+v", mcp.calls)
	}
	if mcp.calls[0].params["file_url"] != "meego://abc" {
		t.Errorf("file_url=%v", mcp.calls[0].params["file_url"])
	}
}

func TestDownloadFlow_Multipart_ConcatenatesParts(t *testing.T) {
	// Two chunks: "aaaa" + "BBBBBB"
	part0 := []byte("aaaa")
	part1 := []byte("BBBBBB")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		last := parts[len(parts)-1]
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		switch last {
		case "0":
			_, _ = w.Write(part0)
		case "1":
			_, _ = w.Write(part1)
		default:
			http.Error(w, "unexpected part_number", 400)
		}
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign":         "S",
		"is_multipart": true,
		"download_url": ts.URL + "/dl/:part_number",
		"multipart": map[string]any{
			"part_count": 2,
			"part_size":  4,
		},
	}}}

	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "merged.bin")
	result, err := downloadFull(t, mcp, http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W",
		FileURL: "x",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("downloadFull: %v", err)
	}
	wantSize := int64(len(part0) + len(part1))
	if result.Size != wantSize {
		t.Errorf("Size=%d, want %d", result.Size, wantSize)
	}
	got, _ := os.ReadFile(output)
	want := append(append([]byte{}, part0...), part1...)
	if !bytes.Equal(got, want) {
		t.Errorf("merged=%q, want %q", got, want)
	}
}

func TestDownloadFlow_ExistingOutput_NoOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "already.txt")
	if err := os.WriteFile(output, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	// ExecuteDownload's checkOutputAvailable runs before any HTTP, so a stub
	// preprocess response is enough — the test never reaches the download_url.
	_, err := downloadFull(t, stubDownloadMCP("http://unused/:part_number"), http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x",
		Output:    output,
		Overwrite: false,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("want already-exists error, got %v", err)
	}
}

func TestDownloadFlow_ExistingOutput_WithOverwrite(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("new contents"))
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "stale.txt")
	if err := os.WriteFile(output, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false,
		"download_url": ts.URL + "/:part_number",
	}}}
	_, err := downloadFull(t, mcp, http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x",
		Output:    output,
		Overwrite: true,
	})
	if err != nil {
		t.Fatalf("downloadFull with --overwrite: %v", err)
	}
	got, _ := os.ReadFile(output)
	if string(got) != "new contents" {
		t.Errorf("overwrite output=%q", got)
	}
}

func TestDownloadFlow_HTTP404_CleansUpPartial(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	mcp := &fakeMCP{response: &attachment.MCPResponse{Data: map[string]any{
		"sign": "S", "is_multipart": false,
		"download_url": ts.URL + "/:part_number",
	}}}
	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "wont-exist.txt")

	_, err := downloadFull(t, mcp, http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "x", Output: output,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("want HTTP 404 error, got %v", err)
	}
	// neither output nor .partial should remain
	if _, e := os.Stat(output); !os.IsNotExist(e) {
		t.Errorf("output should not exist after failed download")
	}
	if _, e := os.Stat(output + ".partial"); !os.IsNotExist(e) {
		t.Errorf(".partial should be cleaned up after failed download")
	}
}

func TestDownloadFlow_PreprocessValidationErrors(t *testing.T) {
	base := attachment.DownloadInput{ProjectKey: "P", WorkItemID: "W", FileURL: "f", Output: "/tmp/x"}
	mutate := func(f func(*attachment.DownloadInput)) attachment.DownloadInput {
		in := base
		f(&in)
		return in
	}
	// Preprocess-side gates: validateDownloadPreprocessInput runs before any
	// MCP call, so a bare fakeMCP suffices and never sees CallTool.
	cases := []struct {
		name string
		in   attachment.DownloadInput
		want string
	}{
		{"missing project-key", mutate(func(in *attachment.DownloadInput) { in.ProjectKey = "" }), "project-key"},
		{"missing work-item-id", mutate(func(in *attachment.DownloadInput) { in.WorkItemID = "" }), "work-item-id"},
		{"missing file-url", mutate(func(in *attachment.DownloadInput) { in.FileURL = "" }), "file-url"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := downloadFull(t, &fakeMCP{}, http.DefaultClient, c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

// Output is enforced by ExecuteDownload (not Preprocess), so this case needs a
// stub preprocess response so we actually reach Execute.
func TestDownloadFlow_MissingOutput_FailsAtExecute(t *testing.T) {
	_, err := downloadFull(t, stubDownloadMCP("http://unused/:part_number"), http.DefaultClient, attachment.DownloadInput{
		ProjectKey: "P", WorkItemID: "W", FileURL: "f", Output: "",
	})
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Errorf("want error mentioning output, got %v", err)
	}
}
