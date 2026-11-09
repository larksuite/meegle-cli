// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type productTransport func(*http.Request) (*http.Response, error)

func (f productTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type productBody struct {
	io.ReadCloser
	closed *atomic.Int64
}

func (b productBody) Close() error { b.closed.Add(1); return b.ReadCloser.Close() }

func productClient(server *httptest.Server, lane string, closed *atomic.Int64) *http.Client {
	return &http.Client{Transport: productTransport(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("X-Test-Client", lane)
		response, err := server.Client().Transport.RoundTrip(r)
		if err == nil {
			response.Body = productBody{response.Body, closed}
		}
		return response, err
	})}
}

func productRPC(w http.ResponseWriter, data any) {
	payload, _ := json.Marshal(data)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(payload)}}}})
}

func productRegistry(t *testing.T, commands []CommandDefinition, shortcuts []ShortcutBinding) *CommandRegistrySetup {
	t.Helper()
	setup, err := NewCommandRegistry(commands, shortcuts)
	if err != nil {
		t.Fatal(err)
	}
	return setup
}

func TestProductAssemblyExecutionRoutes(t *testing.T) {
	t.Setenv("MEEGLE_HOST", "unused.invalid")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "unused-default-identity")
	var businessCalls, fileCalls, closed atomic.Int64
	var mu sync.Mutex
	var called []string
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileCalls.Add(1)
		if r.Header.Get("X-Test-Client") != "files" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Identity") != "" {
			t.Errorf("wrong file route or credential headers: %v", r.Header)
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "sample bytes") {
				t.Errorf("upload body = %q", body)
			}
			_, _ = io.WriteString(w, `{"file_token":"uploaded","file_url":"file://sample","un_upload_index":[]}`)
			return
		}
		if r.Header.Get("X-Lane") != "original" {
			t.Errorf("identity snapshot changed: %v", r.Header)
		}
		w.Header().Set("X-Meego-File-Sign", "signature")
		_, _ = io.WriteString(w, "sample bytes")
	}))
	defer files.Close()
	business := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		businessCalls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Test-Client") != "business" || r.Header.Get("Authorization") != "Bearer sample-identity" {
			t.Errorf("wrong business route/identity: %s %v", r.Method, r.Header)
		}
		var request struct {
			Method string
			Params struct {
				Name      string
				Arguments map[string]any
			}
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&request)
		mu.Lock()
		called = append(called, request.Params.Name)
		mu.Unlock()
		if request.Method != "tools/call" {
			t.Errorf("unexpected discovery: %s", request.Method)
		}
		switch request.Params.Name {
		case "sample_list":
			if request.Params.Arguments["page_token"] == "next" {
				productRPC(w, map[string]any{"items": []int{2}, "has_more": false})
				return
			}
			productRPC(w, map[string]any{"items": []int{1}, "has_more": true, "next_page_token": "next"})
		case "sample_user":
			if !reflect.DeepEqual(request.Params.Arguments["user_keys"], []any{"current_login_user()"}) {
				t.Errorf("user params = %#v", request.Params.Arguments)
			}
			productRPC(w, []any{map[string]any{"user_key": "sample-user", "extra": "preserved"}})
		case "sample_download":
			productRPC(w, map[string]any{"sign": "signature", "is_multipart": false, "download_url": files.URL})
		case "sample_upload":
			productRPC(w, map[string]any{"sign": "signature", "is_multipart": false, "upload_url": files.URL})
		case "sample_get":
			productRPC(w, request.Params.Arguments)
		default:
			t.Errorf("unexpected tool %q", request.Params.Name)
		}
	}))
	defer business.Close()
	businessClient := mcpclient.New(business.URL, mcpclient.WithHTTPClient(productClient(business, "business", &closed)), mcpclient.WithToken(func() (string, error) { return "sample-identity", nil }))
	fileClient := productClient(files, "files", &closed)
	newApp := func(commands []CommandDefinition, shortcuts []ShortcutBinding) *cliapp.App {
		headers := map[string]string{"X-Lane": "original", "Authorization": "secret", "X-Identity": "secret"}
		app, err := NewProductApp(context.Background(), ProductConfig{Name: "sample-cli", Registry: productRegistry(t, commands, shortcuts), BusinessClient: businessClient, FileClient: fileClient, Identity: ResolvedIdentity{Headers: headers, AccessTokenHeader: "X-Identity"}})
		if err != nil {
			t.Fatal(err)
		}
		headers["X-Lane"] = "mutated"
		return app
	}
	get := types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer", Required: true}}}
	invoke := func(app *cliapp.App, args ...string) any {
		result, err := app.Invoke(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		return result.Data
	}
	data := invoke(newApp([]CommandDefinition{{Command: get}}, nil), "record", "get", "--work-item-id", "9007199254740993")
	if data.(map[string]any)["work_item_id"] != json.Number("9007199254740993") {
		t.Fatalf("ordinary result = %#v", data)
	}
	invoke(newApp(nil, []ShortcutBinding{{Kind: ShortcutBatchGet, ToolName: "sample_get", Parameters: get.Parameters}}), "workitem", "+batch-get", "--work-item-ids", "1,2")
	list := types.MappedCommand{Resource: "record", Method: "list", ToolName: "sample_list", Parameters: []types.ToolParameter{{Name: "page_token", Type: "string"}}}
	data = invoke(newApp([]CommandDefinition{{Command: list}}, nil), "record", "list", "--auto-paginate")
	if len(data.(map[string]any)["items"].([]any)) != 2 {
		t.Fatalf("pagination = %#v", data)
	}
	for _, mode := range []ResultMode{DefaultResult, RawResult, CurrentUserResult} {
		data = invoke(newApp(nil, []ShortcutBinding{{Kind: ShortcutCurrentUser, ToolName: "sample_user", Result: mode}}), "user", "me")
		if mode == RawResult {
			if data.([]any)[0].(map[string]any)["extra"] != "preserved" {
				t.Fatal(data)
			}
		} else if data.(map[string]any)["user_key"] != "sample-user" {
			t.Fatal(data)
		}
	}
	output := filepath.Join(t.TempDir(), "download.txt")
	invoke(newApp(nil, []ShortcutBinding{{Kind: ShortcutDownload, ToolName: "sample_download"}}), "attachment", "+download", "file://sample", "--project-key", "sample", "--work-item-id", "1", "--output", output)
	body, err := os.ReadFile(output)
	if err != nil || string(body) != "sample bytes" {
		t.Fatalf("download = %q, %v", body, err)
	}
	invoke(newApp(nil, []ShortcutBinding{{Kind: ShortcutUpload, ToolName: "sample_upload"}}), "attachment", "+upload", output, "--resource-type", "13", "--project-key", "sample", "--work-item-id", "1")
	if businessCalls.Load() != 10 || fileCalls.Load() != 2 || closed.Load() != 12 {
		t.Fatalf("business/files/closed = %d/%d/%d; calls=%v", businessCalls.Load(), fileCalls.Load(), closed.Load(), called)
	}
}

type productDirectory struct {
	fail        error
	panicNow    bool
	mappedPanic bool
	mapped      []types.MappedCommand
	cancel      context.CancelFunc
}

func (d *productDirectory) Setup(ctx context.Context) (*registry.CommandTree, error) {
	if d.panicNow {
		panic("untrusted directory detail")
	}
	if d.fail != nil {
		return nil, d.fail
	}
	if d.cancel != nil {
		d.cancel()
	}
	return buildOrdinaryCommandTree(d.mapped), ctx.Err()
}
func (d *productDirectory) MappedCommands() []types.MappedCommand {
	if d.mappedPanic {
		panic("untrusted snapshot detail")
	}
	return d.mapped
}

func TestProductAssemblyDependencyFailures(t *testing.T) {
	valid := ProductConfig{Name: "sample-cli", Registry: &productDirectory{}, BusinessClient: mcpclient.New("https://unused.invalid"), FileClient: &http.Client{}}
	var typedNil *productDirectory
	for _, change := range []func(*ProductConfig){
		func(c *ProductConfig) { c.Name = "" }, func(c *ProductConfig) { c.Registry = nil }, func(c *ProductConfig) { c.Registry = typedNil },
		func(c *ProductConfig) { c.BusinessClient = nil }, func(c *ProductConfig) { c.FileClient = nil },
		func(c *ProductConfig) { c.Registry = &productDirectory{fail: errors.New("directory rejected")} }, func(c *ProductConfig) { c.Registry = &productDirectory{panicNow: true} },
	} {
		cfg := valid
		change(&cfg)
		app, err := NewProductApp(context.Background(), cfg)
		if app != nil || err == nil || strings.Contains(err.Error(), "untrusted directory detail") {
			t.Fatalf("app=%v err=%v", app, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProductApp(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestProductAssemblyRequestFailuresDoNotFallback(t *testing.T) {
	for _, failure := range []error{errors.New("business rejected"), context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			var calls atomic.Int64
			client := mcpclient.New("https://unused.invalid", mcpclient.WithHTTPClient(&http.Client{Transport: productTransport(func(r *http.Request) (*http.Response, error) { calls.Add(1); _ = r.Body.Close(); return nil, failure })}))
			setup := productRegistry(t, []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get"}}}, nil)
			app, err := NewProductApp(context.Background(), ProductConfig{Name: "sample-cli", Registry: setup, BusinessClient: client, FileClient: &http.Client{}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Invoke(context.Background(), []string{"record", "get"})
			if err == nil || calls.Load() != 1 {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestProductAssemblyIndependentInstances(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tool := fmt.Sprintf("sample_%d", i)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct{ Params struct{ Name string } }
				decoder := json.NewDecoder(r.Body)
				decoder.UseNumber()
				_ = decoder.Decode(&request)
				if request.Params.Name != tool {
					t.Errorf("tool=%s want %s", request.Params.Name, tool)
				}
				productRPC(w, map[string]any{"instance": i})
			}))
			defer server.Close()
			setup := productRegistry(t, []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get", ToolName: tool}}}, nil)
			app, err := NewProductApp(context.Background(), ProductConfig{Name: fmt.Sprintf("sample-%d", i), Registry: setup, BusinessClient: mcpclient.New(server.URL), FileClient: server.Client()})
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 3; j++ {
				result, err := app.Invoke(context.Background(), []string{"record", "get"})
				if err != nil {
					t.Error(err)
					return
				}
				if result.Data.(map[string]any)["instance"] != json.Number(fmt.Sprint(i)) {
					t.Errorf("result=%v", result.Data)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestProductAssemblyCooperativeCancellation(t *testing.T) {
	started := make(chan struct{})
	client := mcpclient.New("https://unused.invalid", mcpclient.WithHTTPClient(&http.Client{Transport: productTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		defer r.Body.Close()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}))
	setup := productRegistry(t, []CommandDefinition{{Command: types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get"}}}, nil)
	app, err := NewProductApp(context.Background(), ProductConfig{Name: "sample-cli", Registry: setup, BusinessClient: client, FileClient: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := app.Invoke(ctx, []string{"record", "get"}); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not stop")
	}
}

func TestProductAssemblyDirectorySnapshotAndCallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"sample_get rejected"}}`)
	}))
	defer server.Close()
	directory := &productDirectory{mapped: []types.MappedCommand{{Resource: "record", Method: "get", ToolName: "sample_get", Description: "Get sample record"}}}
	cfg := ProductConfig{Name: "sample-cli", Registry: directory, BusinessClient: mcpclient.New(server.URL), FileClient: server.Client()}
	app, err := NewProductApp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	directory.mapped[0].Method = "mutated"
	directory.mappedPanic = true
	_, err = app.Invoke(context.Background(), []string{"record", "get"})
	if err == nil || !strings.Contains(err.Error(), "record get") || strings.Contains(err.Error(), "mutated") {
		t.Fatalf("error snapshot=%v", err)
	}
	if _, err = NewProductApp(context.Background(), cfg); err == nil || strings.Contains(err.Error(), "untrusted snapshot detail") {
		t.Fatalf("snapshot panic=%v", err)
	}
	directory.mappedPanic = false
	cfg.CustomizeRoot = func(*cobra.Command, cliapp.RootCommandMetadata) { panic("untrusted customizer detail") }
	if _, err = NewProductApp(context.Background(), cfg); err == nil || strings.Contains(err.Error(), "untrusted customizer detail") {
		t.Fatalf("customizer panic=%v", err)
	}
	cfg.CustomizeRoot = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	directory.cancel = cancel
	if _, err = NewProductApp(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("assembly cancellation=%v", err)
	}
}

type productReadFunc func([]byte) (int, error)

func (f productReadFunc) Read(b []byte) (int, error) { return f(b) }

func TestProductAssemblyFileFailureCleanup(t *testing.T) {
	for _, mode := range []string{"transport-error", "body-error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				productRPC(w, map[string]any{"sign": "signature", "is_multipart": false, "download_url": "https://files.invalid/download"})
			}))
			defer server.Close()
			var calls, closed atomic.Int64
			started := make(chan struct{})
			fileClient := &http.Client{Transport: productTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if mode == "transport-error" {
					close(started)
					return nil, errors.New("file transport rejected")
				}
				reader := productReadFunc(func([]byte) (int, error) {
					close(started)
					if mode == "cancel" {
						<-r.Context().Done()
						return 0, r.Context().Err()
					}
					return 0, errors.New("file body rejected")
				})
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Meego-File-Sign": []string{"signature"}}, Body: productBody{io.NopCloser(reader), &closed}}, nil
			})}
			setup := productRegistry(t, nil, []ShortcutBinding{{Kind: ShortcutDownload, ToolName: "sample_download"}})
			app, err := NewProductApp(context.Background(), ProductConfig{Name: "sample-cli", Registry: setup, BusinessClient: mcpclient.New(server.URL), FileClient: fileClient})
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "failed.txt")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := app.Invoke(ctx, []string{"attachment", "+download", "file://sample", "--project-key", "sample", "--work-item-id", "1", "--output", output})
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("file request did not start")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failure became success")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("file failure did not return")
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
			if mode != "transport-error" && closed.Load() != 1 {
				t.Fatalf("closed=%d", closed.Load())
			}
			for _, path := range []string{output, output + ".partial"} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("left file %s: %v", path, err)
				}
			}
		})
	}
}

func TestProductAssemblySeparateDirectory(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string
			Params struct{ Name string }
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method != "tools/call" || request.Params.Name != "sample_directory_tool" {
			t.Errorf("business client used for discovery or wrong binding: %#v", request)
		}
		calls.Add(1)
		productRPC(w, map[string]any{"source": "business"})
	}))
	defer server.Close()
	lister := &successLister{tools: []types.ToolDefinition{{Name: "sample_directory_tool", Metadata: &types.ToolMetadata{Resource: "record", Method: "get"}}}}
	cfg := ProductConfig{Name: "sample-cli", Registry: NewDynamicRegistrySetup(lister, nil), BusinessClient: mcpclient.New(server.URL), FileClient: server.Client()}
	app, err := NewProductApp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.Invoke(context.Background(), []string{"record", "get"})
	if err != nil || result.Data.(map[string]any)["source"] != "business" || lister.calls != 1 || calls.Load() != 1 {
		t.Fatalf("result=%v err=%v directory=%d business=%d", result, err, lister.calls, calls.Load())
	}
	var typedNil *successLister
	cfg.Registry = NewDynamicRegistrySetup(typedNil, nil)
	if _, err := NewProductApp(context.Background(), cfg); err == nil {
		t.Fatal("typed nil directory was accepted")
	}
	cfg.Registry = NewDynamicRegistrySetup(&successLister{}, nil)
	if _, err := NewProductApp(context.Background(), cfg); err == nil {
		t.Fatal("empty dynamic directory silently fell back")
	}
}

func TestProductBackendErrorsCLI(t *testing.T) {
	for _, name := range []string{"meegle", "sample-cli"} {
		for _, mode := range []string{"ordinary", "paginate", "batch"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				var calls atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var request struct {
						Params struct{ Arguments map[string]any }
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if mode == "paginate" && request.Params.Arguments["page_token"] == nil {
						productRPC(w, map[string]any{"items": []int{1}, "has_more": true, "next_page_token": "next"})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32603, "message": "tool sample_get rejected"}})
				}))
				defer server.Close()
				command := types.MappedCommand{Resource: "record", Method: "get", ToolName: "sample_get", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer"}, {Name: "page_token", Type: "string"}}}
				setup := productRegistry(t, []CommandDefinition{{Command: command}}, []ShortcutBinding{{Kind: ShortcutBatchGet, ToolName: command.ToolName, Parameters: command.Parameters}})
				app, err := NewProductApp(context.Background(), ProductConfig{Name: name, Registry: setup, BusinessClient: mcpclient.New(server.URL, mcpclient.WithHTTPClient(server.Client())), FileClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"record", "get", "--format", "json"}
				wantCalls := int64(1)
				if mode == "paginate" {
					args = append(args, "--auto-paginate")
					wantCalls = 2
				}
				if mode == "batch" {
					args = []string{"workitem", "+batch-get", "--work-item-ids", "1,2", "--format", "json"}
					wantCalls = 2
				}
				var stdout, stderr bytes.Buffer
				err = app.ExecuteWithIO(context.Background(), args, &stdout, &stderr)
				want := "tool " + name + " record get rejected"
				if mode == "batch" {
					if err != nil || stderr.Len() != 0 {
						t.Fatalf("error=%v stderr=%s", err, &stderr)
					}
					var response struct {
						Errors []struct {
							Code    string
							Message string
						}
					}
					if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if len(response.Errors) != 2 {
						t.Fatalf("response=%s", &stdout)
					}
					for _, item := range response.Errors {
						if item.Code != "SERVER_CALL_FAILED" || item.Message != want {
							t.Fatalf("item=%+v", item)
						}
					}
				} else {
					var me *meerrors.MeegleError
					if !errors.As(err, &me) || me.Message != want || me.Code != "SERVER_CALL_FAILED" || me.ExitCode != 2 || me.IsRetryable() {
						t.Fatalf("error=%#v", err)
					}
					var response struct{ Error struct{ Message string } }
					if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if response.Error.Message != want || stdout.Len() != 0 {
						t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
					}
				}
				if calls.Load() != wantCalls {
					t.Fatalf("calls=%d, want %d", calls.Load(), wantCalls)
				}
			})
		}
	}
}

func TestProductBatchHelpCLI(t *testing.T) {
	for _, tool := range []string{"sample_get", "other_read"} {
		setup := productRegistry(t, nil, []ShortcutBinding{{Kind: ShortcutBatchGet, ToolName: tool, Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer"}}}})
		app, err := NewProductApp(context.Background(), ProductConfig{Name: "sample-cli", Registry: setup, BusinessClient: mcpclient.New("http://unused.invalid"), FileClient: http.DefaultClient})
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "+batch-get", "--help"}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "Client-side fan-out over the bound single-item tool, one call per ID.") || strings.Contains(stdout.String(), "workitem get") || stderr.Len() != 0 {
			t.Fatalf("help=%s stderr=%s", &stdout, &stderr)
		}
	}
}
