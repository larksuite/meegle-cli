// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	exttransport "github.com/larksuite/meegle-cli/extension/transport"
	internaltransport "github.com/larksuite/meegle-cli/internal/extension/transport"
	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

type recordingProvider struct {
	postCalls *atomic.Int32
}

func (recordingProvider) Name() string { return "integration-transport" }
func (p recordingProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	return recordingInterceptor{postCalls: p.postCalls}
}

type recordingInterceptor struct {
	postCalls *atomic.Int32
}

func (i recordingInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.Header.Set("X-Extension-Transport", "active")
	return func(*http.Response, error) { i.postCalls.Add(1) }
}

func TestExtensionTransport_InterceptsOAuthRefreshRequestsEndToEnd(t *testing.T) {
	if runIntegrationSubprocess(t) {
		return
	}
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Extension-Transport") != "active" {
			t.Errorf("OAuth request %s did not pass through extension transport", request.URL.Path)
		}
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(writer, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q}`,
				server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/register")
		case "/token":
			_, _ = writer.Write([]byte(`{"access_token":"fresh-token","refresh_token":"fresh-refresh","expires_in":3600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	var postCalls atomic.Int32
	exttransport.Register(recordingProvider{postCalls: &postCalls})
	client := internaltransport.NewHTTPClient(context.Background(), server.Client())
	manager := auth.NewTokenManager(auth.NewFileStore(t.TempDir(), "refresh"), strings.TrimPrefix(server.URL, "https://")).WithHTTPClient(client)
	refreshed, err := manager.RefreshToken(&auth.TokenData{AccessToken: "old", RefreshToken: "refresh", ClientID: "client-1"})
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if refreshed.AccessToken != "fresh-token" {
		t.Fatalf("refreshed access token = %q", refreshed.AccessToken)
	}
	if requests.Load() != 2 || postCalls.Load() != 2 {
		t.Fatalf("refresh interception counts: requests=%d post=%d, want 2 each", requests.Load(), postCalls.Load())
	}
}

func TestExtensionTransport_PreservesStreamingMCPResponseUntilBodyClose(t *testing.T) {
	if runIntegrationSubprocess(t) {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[`))
		writer.(http.Flusher).Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"name":"streamed_tool","description":"streamed","inputSchema":{"type":"object"}}]}}`))
	}))
	defer server.Close()

	var postCalls atomic.Int32
	exttransport.Register(recordingProvider{postCalls: &postCalls})
	httpClient := internaltransport.NewHTTPClient(context.Background(), server.Client())
	client := mcpclient.New(server.URL, mcpclient.WithHTTPClient(httpClient))

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v, want complete streamed response", err)
	}
	if len(tools) != 1 || tools[0].Name != "streamed_tool" {
		t.Fatalf("ListTools() = %+v, want streamed_tool", tools)
	}
	if postCalls.Load() != 1 {
		t.Fatalf("post-hook calls = %d, want 1", postCalls.Load())
	}
}

func TestExtensionTransport_PreservesStreamingMCPToolCallResponse(t *testing.T) {
	if runIntegrationSubprocess(t) {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[`))
		writer.(http.Flusher).Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"type":"text","text":"{\"ok\":true}"}]}}`))
	}))
	defer server.Close()

	var postCalls atomic.Int32
	exttransport.Register(recordingProvider{postCalls: &postCalls})
	httpClient := internaltransport.NewHTTPClient(context.Background(), server.Client())
	client := mcpclient.New(server.URL, mcpclient.WithHTTPClient(httpClient))

	result, err := client.CallTool(context.Background(), "streamed_tool", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v, want complete streamed response", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["ok"] != true {
		t.Fatalf("CallTool() data = %#v, want ok=true", result.Data)
	}
	if postCalls.Load() != 1 {
		t.Fatalf("post-hook calls = %d, want 1", postCalls.Load())
	}
}

func TestExtensionTransport_InterceptsAttachmentUploadAndDownloadEndToEnd(t *testing.T) {
	if runIntegrationSubprocess(t) {
		return
	}
	var postCalls atomic.Int32
	exttransport.Register(recordingProvider{postCalls: &postCalls})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Extension-Transport") != "active" {
			t.Errorf("attachment request %s did not pass through extension transport", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /upload/0":
			_, _ = writer.Write([]byte(`{"file_token":"file-token","file_url":"https://files.example/file-token"}`))
		case "GET /download/0":
			writer.Header().Set("X-Meego-File-Sign", "download-sign")
			writer.Header().Set("Content-Disposition", `attachment; filename="download.txt"`)
			_, _ = writer.Write([]byte("download-body"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := internaltransport.NewHTTPClient(context.Background(), server.Client())

	uploadBody := strings.NewReader("upload-body")
	uploaded, err := attachment.ExecuteUpload(context.Background(), client,
		&attachment.UploadPreprocess{UploadURL: server.URL + "/upload/:part_number", Sign: "upload-sign"},
		attachment.UploadInput{
			ResourceType: attachment.ResourceTypeCommentAttachment,
			ProjectKey:   "PROJ", WorkItemID: "123", File: uploadBody, FileSize: int64(uploadBody.Len()),
			Filename: "upload.txt", ContentType: "text/plain",
		})
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	if uploaded.FileToken != "file-token" {
		t.Fatalf("uploaded file token = %q", uploaded.FileToken)
	}

	output := filepath.Join(t.TempDir(), "download.txt")
	downloaded, err := attachment.ExecuteDownload(context.Background(), client,
		&attachment.DownloadPreprocess{DownloadURL: server.URL + "/download/:part_number", Sign: "download-sign"},
		attachment.DownloadInput{Output: output})
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read downloaded attachment: %v", err)
	}
	if downloaded.Size != int64(len(content)) || string(content) != "download-body" {
		t.Fatalf("downloaded attachment = size %d body %q", downloaded.Size, content)
	}
	if postCalls.Load() != 2 {
		t.Fatalf("attachment post-hook calls = %d, want upload and download", postCalls.Load())
	}
}

func runIntegrationSubprocess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("TRANSPORT_INTEGRATION_HELPER") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
	command.Env = append(os.Environ(), "TRANSPORT_INTEGRATION_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("transport integration helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("transport integration helper failed: %v\n%s", err, output)
	}
	return true
}
