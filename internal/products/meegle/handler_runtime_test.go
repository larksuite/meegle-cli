// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/commands"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type handlerDirectory struct {
	registry.RegistrySetup
	mapped []types.MappedCommand
}

func (d handlerDirectory) MappedCommands() []types.MappedCommand { return d.mapped }

func handlerNode() *registry.CommandNode {
	return &registry.CommandNode{
		Name: "echo", HandlerRef: "sample.echo", Help: registry.HelpText{Brief: "Echo sample input"},
		Flags: []registry.FlagDef{
			{Name: "item-name", Type: registry.FlagTypeString, Required: true},
			{Name: "count", Type: registry.FlagTypeInteger},
			{Name: "mode", Type: registry.FlagTypeString, Enum: []string{"a", "b"}, Default: "a"},
			{Name: "enabled", Type: registry.FlagTypeBool},
			{Name: "object", Type: registry.FlagTypeObject},
		},
		Args: []registry.ArgDef{{Name: "label"}},
		Meta: registry.NodeMeta{Tags: map[string]string{meegle.TagExecutorKind: meegle.ExecutorKindHandler}},
	}
}

func handlerConfig(node *registry.CommandNode, handler executor.Handler) meegle.ProductConfig {
	return meegle.ProductConfig{
		Name: "sample-cli", Version: "1.0.0",
		Registry:       handlerDirectory{RegistrySetup: registry.NewStaticSetup(&registry.CommandTree{Nodes: []*registry.CommandNode{node}})},
		Handlers:       map[string]executor.Handler{"sample.echo": handler},
		BusinessClient: mcpclient.New("https://unused.invalid"), FileClient: &http.Client{},
	}
}

func handlerApp(t *testing.T, cfg meegle.ProductConfig) *cliapp.App {
	t.Helper()
	app, err := meegle.NewProductApp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func echoHandler(_ context.Context, req *executor.Request) (*executor.RawResult, error) {
	return &executor.RawResult{Data: req.Values, Metadata: map[string]any{"sample": true}}, nil
}

func TestProductHandlerCLIParametersAndOutput(t *testing.T) {
	for _, format := range []string{"json", "ndjson", "table"} {
		t.Run(format, func(t *testing.T) {
			var calls int
			app := handlerApp(t, handlerConfig(handlerNode(), func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
				calls++
				if req.Meta.Output != format || req.Meta.Select != "item-name,count,label" || !req.Meta.Verbose || req.Source.Product != "sample-cli" || req.Source.Version != "1.0.0" || req.Source.Channel != "cli" {
					t.Fatalf("request meta/source = %+v / %+v", req.Meta, req.Source)
				}
				want := map[string]any{"item-name": "flag", "count": json.Number("9007199254740993"), "mode": "b", "enabled": false, "object": "", "label": "positional"}
				if !reflect.DeepEqual(req.Values, want) {
					t.Fatalf("values=%#v want=%#v", req.Values, want)
				}
				return echoHandler(ctx, req)
			}))
			var out, stderr bytes.Buffer
			err := app.ExecuteWithIO(context.Background(), []string{"echo", "positional", "--params", `{"item_name":"json","count":9007199254740993}`, "--set", "mode=b", "--item-name", "flag", "--format", format, "--select", "item-name,count,label", "--verbose"}, &out, &stderr)
			if err != nil || calls != 1 || stderr.Len() != 0 || !strings.Contains(out.String(), "9007199254740993") || strings.Contains(out.String(), "enabled") {
				t.Fatalf("calls=%d out=%s stderr=%s err=%v", calls, &out, &stderr, err)
			}
		})
	}
	app := handlerApp(t, handlerConfig(handlerNode(), echoHandler))
	var out, stderr bytes.Buffer
	if err := app.ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--envelope", "--format", "json"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"sample": true`) || !strings.Contains(out.String(), `"data"`) {
		t.Fatal(out.String())
	}
}

func TestProductHandlerDeclaredTypes(t *testing.T) {
	node := handlerNode()
	node.Flags = append(node.Flags, registry.FlagDef{Name: "limit", Type: registry.FlagTypeInt})
	calls := 0
	app := handlerApp(t, handlerConfig(node, func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
		calls++
		want := map[string]any{"nested": map[string]any{"arbitrary_key": json.Number("1")}}
		if !reflect.DeepEqual(req.Values["object"], want) {
			t.Fatalf("object = %#v (%T), want %#v", req.Values["object"], req.Values["object"], want)
		}
		if value, ok := req.Values["limit"].(int); !ok || value != 3 {
			t.Fatalf("limit = %#v (%T), want int(3)", req.Values["limit"], req.Values["limit"])
		}
		return echoHandler(ctx, req)
	}))
	var out, stderr bytes.Buffer
	err := app.ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--object", `{"nested":{"arbitrary_key":1}}`, "--set", "limit=3"}, &out, &stderr)
	if err != nil || calls != 1 || stderr.Len() != 0 {
		t.Fatalf("err=%v calls=%d stderr=%s", err, calls, &stderr)
	}
}

func TestProductHandlerNoExecution(t *testing.T) {
	cases := []struct {
		name string
		args []string
		fail bool
	}{
		{"help", []string{"--help"}, false},
		{"command-help", []string{"echo", "--help"}, false},
		{"inspect", []string{"inspect"}, false},
		{"dry-run", []string{"echo", "--item-name", "ok", "--dry-run"}, false},
		{"missing", []string{"echo"}, true},
		{"enum", []string{"echo", "--item-name", "ok", "--mode", "bad"}, true},
		{"number", []string{"echo", "--item-name", "ok", "--count", "1.5"}, true},
		{"json-number", []string{"echo", "--params", `{"item-name":"ok","count":1.5}`}, true},
		{"json-string", []string{"echo", "--params", `{"item-name":{}}`}, true},
		{"json-bool", []string{"echo", "--params", `{"item-name":"ok","enabled":"false"}`}, true},
		{"null", []string{"echo", "--params", `{"item-name":null}`}, true},
		{"malformed", []string{"echo", "--params", `{`}, true},
		{"object", []string{"echo", "--item-name", "ok", "--object", `{`}, true},
		{"unknown-params", []string{"echo", "--params", `{"item_name":"ok","unknown_key":1}`}, true},
		{"unknown-set", []string{"echo", "--item-name", "ok", "--set", "unknown_key=1"}, true},
		{"unknown-dry-run", []string{"echo", "--item-name", "ok", "--set", "unknown_key=1", "--dry-run"}, true},
		{"unknown-flag", []string{"echo", "--unknown"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			cfg := handlerConfig(handlerNode(), func(context.Context, *executor.Request) (*executor.RawResult, error) { calls.Add(1); return nil, nil })
			cfg.BusinessClient = mcpclient.New(server.URL)
			cfg.CustomizeRoot = func(root *cobra.Command, _ cliapp.RootCommandMetadata) {
				root.AddCommand(commands.NewInspectCmdWithProvider(func(context.Context) []types.MappedCommand { return nil }))
			}
			var out, stderr bytes.Buffer
			err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), tc.args, &out, &stderr)
			if (err != nil) != tc.fail || calls.Load() != 0 {
				t.Fatalf("err=%v calls=%d stderr=%s", err, calls.Load(), &stderr)
			}
			if strings.HasPrefix(tc.name, "unknown-") && tc.name != "unknown-flag" {
				if !strings.Contains(stderr.String(), "CLIENT_INVALID_VALUE") || !strings.Contains(stderr.String(), "unknown_key") || out.Len() != 0 {
					t.Fatalf("out=%s stderr=%s", &out, &stderr)
				}
			}
			if tc.name == "dry-run" && (!strings.Contains(out.String(), "sample.echo") || !strings.Contains(out.String(), `"dry_run": true`)) {
				t.Fatal(out.String())
			}
		})
	}
}

type handlerLister struct{}

func (handlerLister) ListTools(context.Context) ([]types.ToolDefinition, error) {
	return []types.ToolDefinition{{Name: "sample_remote", Metadata: &types.ToolMetadata{Resource: "record", Method: "get"}}}, nil
}

func TestProductHandlerCoexistsWithDynamicTools(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Params struct{ Name string } }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Params.Name != "sample_remote" {
			t.Errorf("request=%+v err=%v", request, err)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"remote\":true}"}]}}`))
	}))
	defer server.Close()
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprint(hidden), func(t *testing.T) {
			dynamic := meegle.NewDynamicRegistrySetup(handlerLister{}, nil)
			cfg := handlerConfig(handlerNode(), echoHandler)
			client := mcpclient.New(server.URL)
			cfg.BusinessClient = client
			cfg.Registry = handlerDirectory{RegistrySetup: registry.NewProgrammaticSetup(func(ctx context.Context) (*registry.CommandTree, error) {
				tree, err := dynamic.Setup(ctx)
				if err != nil {
					return nil, err
				}
				if hidden {
					tree.Nodes = nil
				}
				tree.Nodes = append(tree.Nodes, handlerNode())
				return tree, nil
			})}
			cfg.Handlers["sample.echo"] = func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
				result, err := client.CallTool(ctx, "sample_remote", map[string]any{})
				return &executor.RawResult{Data: result}, err
			}
			app := handlerApp(t, cfg)
			var out, stderr bytes.Buffer
			if err := app.ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok"}, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			app = handlerApp(t, cfg)
			err := app.ExecuteWithIO(context.Background(), []string{"record", "get"}, &out, &stderr)
			if (err != nil) != hidden {
				t.Fatalf("hidden=%v err=%v", hidden, err)
			}
		})
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestProductHandlerConfigurationFailures(t *testing.T) {
	changes := map[string]func(*meegle.ProductConfig, *registry.CommandNode){
		"absent-map":  func(c *meegle.ProductConfig, _ *registry.CommandNode) { c.Handlers = nil },
		"missing-ref": func(_ *meegle.ProductConfig, n *registry.CommandNode) { n.HandlerRef = "" },
		"unknown-ref": func(_ *meegle.ProductConfig, n *registry.CommandNode) { n.HandlerRef = "missing" },
		"nil-handler": func(c *meegle.ProductConfig, _ *registry.CommandNode) { c.Handlers["sample.echo"] = nil },
		"empty-key":   func(c *meegle.ProductConfig, _ *registry.CommandNode) { c.Handlers[""] = echoHandler },
		"unknown-kind": func(_ *meegle.ProductConfig, n *registry.CommandNode) {
			n.Meta.Tags[meegle.TagExecutorKind] = "invalid"
		},
		"duplicate-root": func(c *meegle.ProductConfig, n *registry.CommandNode) {
			c.Registry = handlerDirectory{RegistrySetup: registry.NewStaticSetup(&registry.CommandTree{Nodes: []*registry.CommandNode{n, handlerNode()}})}
		},
		"duplicate-child": func(_ *meegle.ProductConfig, n *registry.CommandNode) {
			n.Children = []*registry.CommandNode{handlerNode(), handlerNode()}
		},
		"alias-conflict": func(_ *meegle.ProductConfig, n *registry.CommandNode) {
			a, b := handlerNode(), handlerNode()
			b.Name = "other"
			b.Aliases = []string{"echo"}
			n.Children = []*registry.CommandNode{a, b}
		},
		"static-conflict": func(c *meegle.ProductConfig, _ *registry.CommandNode) {
			c.CustomizeRoot = func(root *cobra.Command, _ cliapp.RootCommandMetadata) { root.AddCommand(&cobra.Command{Use: "echo"}) }
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			node := handlerNode()
			cfg := handlerConfig(node, echoHandler)
			change(&cfg, node)
			app, err := meegle.NewProductApp(context.Background(), cfg)
			if err == nil || app != nil {
				t.Fatalf("app=%v err=%v", app, err)
			}
		})
	}
}

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("private panic detail") }

type nilHandlerError struct{}

func (*nilHandlerError) Error() string { panic("nil error detail") }

func TestProductHandlerFailures(t *testing.T) {
	var remoteCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remoteCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	var typedNil *nilHandlerError
	cases := map[string]executor.Handler{
		"error": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return nil, errors.New("sample failure")
		},
		"typed-nil":  func(context.Context, *executor.Request) (*executor.RawResult, error) { return nil, typedNil },
		"nil-result": func(context.Context, *executor.Request) (*executor.RawResult, error) { return nil, nil },
		"result-error": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return &executor.RawResult{Data: "not success", Err: errors.New("sample failure")}, nil
		},
		"result-typed-nil": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return &executor.RawResult{Err: typedNil}, nil
		},
		"panic":     func(context.Context, *executor.Request) (*executor.RawResult, error) { panic("private panic detail") },
		"panic-nil": func(context.Context, *executor.Request) (*executor.RawResult, error) { panic(nil) },
		"bad-data": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return &executor.RawResult{Data: make(chan int)}, nil
		},
		"bad-metadata": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return &executor.RawResult{Data: "ok", Metadata: map[string]any{"bad": make(chan int)}}, nil
		},
		"marshal-panic": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return &executor.RawResult{Data: panicJSON{}}, nil
		},
		"successful-exit": func(context.Context, *executor.Request) (*executor.RawResult, error) {
			return nil, cliapp.SuccessfulExit(errors.New("not success"))
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := handlerConfig(handlerNode(), handler)
			cfg.BusinessClient = mcpclient.New(server.URL)
			app := handlerApp(t, cfg)
			var out, stderr bytes.Buffer
			err := app.ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--format", "json"}, &out, &stderr)
			if err == nil || cliapp.IsSuccessfulExit(err) || out.Len() != 0 || !json.Valid(stderr.Bytes()) || strings.Contains(stderr.String(), "private panic detail") {
				t.Fatalf("err=%v out=%s stderr=%s", err, &out, &stderr)
			}
		})
	}
	if remoteCalls.Load() != 0 {
		t.Fatalf("handler failures fell back to remote execution: %d", remoteCalls.Load())
	}
}

func TestProductHandlerContext(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "pre-cancel", "ignored-cancel"} {
		t.Run(mode, func(t *testing.T) {
			type key struct{}
			ctx := context.WithValue(context.Background(), key{}, "sample")
			var cancel context.CancelFunc
			if mode == "timeout" {
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			started, cleaned := make(chan struct{}), make(chan struct{})
			var calls atomic.Int64
			app := handlerApp(t, handlerConfig(handlerNode(), func(got context.Context, _ *executor.Request) (*executor.RawResult, error) {
				calls.Add(1)
				defer close(cleaned)
				close(started)
				if got != ctx || got.Value(key{}) != "sample" {
					t.Error("invocation context changed")
				}
				<-got.Done()
				if mode == "ignored-cancel" {
					return &executor.RawResult{Data: "late"}, nil
				}
				return nil, got.Err()
			}))
			if mode == "pre-cancel" {
				cancel()
			}
			done := make(chan error, 1)
			go func() {
				var out, stderr bytes.Buffer
				done <- app.ExecuteWithIO(ctx, []string{"echo", "--item-name", "ok"}, &out, &stderr)
			}()
			if mode != "pre-cancel" {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("handler not started")
				}
				if mode != "timeout" {
					cancel()
				}
			}
			select {
			case err := <-done:
				want := context.Canceled
				if mode == "timeout" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) {
					t.Fatalf("err=%v want=%v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("handler did not stop")
			}
			if mode == "pre-cancel" {
				if calls.Load() != 0 {
					t.Fatal("called canceled handler")
				}
			} else {
				select {
				case <-cleaned:
				default:
					t.Fatal("cleanup not complete")
				}
			}
		})
	}
}

func TestProductHandlerIsolationAndReentry(t *testing.T) {
	for i := 0; i < 8; i++ {
		i := i
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 3; j++ {
				node := handlerNode()
				cfg := handlerConfig(node, func(ctx context.Context, _ *executor.Request) (*executor.RawResult, error) {
					inner := handlerApp(t, handlerConfig(handlerNode(), echoHandler))
					return inner.Invoke(ctx, []string{"echo", "--item-name", fmt.Sprint(i)})
				})
				app := handlerApp(t, cfg)
				cfg.Handlers["sample.echo"] = nil
				node.Meta.Tags[meegle.TagExecutorKind] = meegle.ExecutorKindMCP
				node.Flags[0].Required = false
				var out, stderr bytes.Buffer
				if err := app.ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "outer", "--select", "item-name"}, &out, &stderr); err != nil {
					t.Fatal(err)
				}
				var result map[string]any
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["item-name"] != fmt.Sprint(i) {
					t.Fatalf("result=%s err=%v", &out, err)
				}
			}
		})
	}
}

func TestProductHandlerProcess(t *testing.T) {
	if os.Getenv("SAMPLE_HANDLER_CHILD") == "1" {
		handler := echoHandler
		switch os.Getenv("SAMPLE_HANDLER_FAIL") {
		case "panic":
			handler = func(context.Context, *executor.Request) (*executor.RawResult, error) { panic("private panic detail") }
		case "payload", "metadata":
			handler = func(context.Context, *executor.Request) (*executor.RawResult, error) {
				return nil, badHandlerPayload{metadata: os.Getenv("SAMPLE_HANDLER_FAIL") == "metadata"}
			}
		}
		app, err := meegle.NewProductApp(context.Background(), handlerConfig(handlerNode(), handler))
		if err == nil {
			err = app.Execute(context.Background(), []string{"echo", "--item-name", "process", "--format", "json"})
		}
		os.Exit(frameworkerrors.ExitCode(err))
	}
	for _, mode := range []string{"success", "panic", "payload", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			fail := mode != "success"
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(binary, "-test.run=^TestProductHandlerProcess$")
			child.Env = append(os.Environ(), "SAMPLE_HANDLER_CHILD=1")
			if fail {
				child.Env = append(child.Env, "SAMPLE_HANDLER_FAIL="+mode)
			}
			var out, stderr bytes.Buffer
			child.Stdout = &out
			child.Stderr = &stderr
			err = child.Run()
			if fail {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "CLIENT_HANDLER_FAILED") {
					t.Fatalf("err=%v out=%s stderr=%s", err, &out, &stderr)
				}
			} else if err != nil || !strings.Contains(out.String(), "process") || stderr.Len() != 0 {
				t.Fatalf("err=%v out=%s stderr=%s", err, &out, &stderr)
			}
		})
	}
}

func TestProductHandlerPreservesStructuredError(t *testing.T) {
	failure := meerrors.NewServerError("SAMPLE_REJECTED", "sample rejected").WithRetryable(false)
	cfg := handlerConfig(handlerNode(), func(context.Context, *executor.Request) (*executor.RawResult, error) { return nil, failure })
	var out, stderr bytes.Buffer
	err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--format", "json"}, &out, &stderr)
	var got *meerrors.MeegleError
	if !errors.As(err, &got) || got != failure || !strings.Contains(stderr.String(), "SAMPLE_REJECTED") || got.ExitCode != 2 || got.IsRetryable() {
		t.Fatalf("err=%v stderr=%s", err, &stderr)
	}
}

func TestProductHandlerNestedCommands(t *testing.T) {
	var calls atomic.Int64
	for _, path := range [][]string{{"sample", "record", "echo"}, {"sample", "record", "echo", "child", "echo"}} {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			leaf := handlerNode()
			leaf.Children = []*registry.CommandNode{{Name: "child", Help: registry.HelpText{Brief: "Child commands"}, Children: []*registry.CommandNode{handlerNode()}}}
			tree := &registry.CommandTree{Nodes: []*registry.CommandNode{{Name: "sample", Aliases: []string{"s"}, Help: registry.HelpText{Brief: "Sample commands"}, Flags: []registry.FlagDef{{Name: "tenant", Type: registry.FlagTypeString, Required: true}}, Children: []*registry.CommandNode{{Name: "record", Help: registry.HelpText{Brief: "Record commands"}, Children: []*registry.CommandNode{leaf, {Name: "remote", HandlerRef: "sample_remote", Help: registry.HelpText{Brief: "Remote record"}}}}}}}}
			cfg := handlerConfig(leaf, func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
				calls.Add(1)
				if req.Values["tenant"] != "sample" {
					t.Errorf("missing ancestor flag: %+v", req.Values)
				}
				return echoHandler(ctx, req)
			})
			cfg.Registry = handlerDirectory{RegistrySetup: registry.NewStaticSetup(tree)}
			var out, stderr bytes.Buffer
			args := append(append([]string(nil), path...), "--tenant", "sample", "--item-name", "nested")
			if err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), args, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			args[0] = "s"
			if err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), args, &out, &stderr); err != nil {
				t.Fatal(err)
			}
		})
	}
	if calls.Load() != 4 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestProductHandlerGlobalFlags(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		fail   bool
	}{
		{"valid", `{"tenant_id":"sample","global-mode":"a","global-enabled":true}`, false},
		{"missing", `{}`, true},
		{"enum", `{"tenant-id":"sample","global-mode":"invalid"}`, true},
		{"type", `{"tenant-id":"sample","global-enabled":"true"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			cfg := handlerConfig(handlerNode(), func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
				calls++
				if req.Values["tenant-id"] != "sample" {
					t.Errorf("global values=%v", req.Values)
				}
				return echoHandler(ctx, req)
			})
			cfg.Registry = handlerDirectory{RegistrySetup: registry.NewStaticSetup(&registry.CommandTree{Nodes: []*registry.CommandNode{handlerNode()}, GlobalFlags: []registry.FlagDef{
				{Name: "tenant-id", Type: registry.FlagTypeString, Required: true},
				{Name: "global-mode", Type: registry.FlagTypeString, Enum: []string{"a", "b"}, Default: "a"},
				{Name: "global-enabled", Type: registry.FlagTypeBool},
			}})}
			var out, stderr bytes.Buffer
			err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--params", tc.params}, &out, &stderr)
			if (err != nil) != tc.fail || (calls == 0) != tc.fail {
				t.Fatalf("calls=%d err=%v stderr=%s", calls, err, &stderr)
			}
		})
	}
}

type cancelJSON struct{ cancel context.CancelFunc }

func (v cancelJSON) MarshalJSON() ([]byte, error) { v.cancel(); return []byte(`"late"`), nil }
func TestProductHandlerCancellationDuringResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := handlerConfig(handlerNode(), func(context.Context, *executor.Request) (*executor.RawResult, error) {
		return &executor.RawResult{Data: cancelJSON{cancel}}, nil
	})
	var out, stderr bytes.Buffer
	err := handlerApp(t, cfg).ExecuteWithIO(ctx, []string{"echo", "--item-name", "ok"}, &out, &stderr)
	if !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("err=%v out=%s", err, &out)
	}
}

type badHandlerPayload struct{ metadata bool }

func (badHandlerPayload) Error() string { return "sample failure" }
func (e badHandlerPayload) ErrorPayload() map[string]any {
	if e.metadata {
		return map[string]any{"code": "SAMPLE_FAILED", "message": "sample failure", "retryable": false}
	}
	return map[string]any{"code": "SAMPLE_FAILED", "message": panicJSON{}}
}
func (e badHandlerPayload) ErrorMetadata() map[string]any {
	if e.metadata {
		return map[string]any{"bad": panicJSON{}}
	}
	return nil
}

func TestProductHandlerRequiresExplicitKind(t *testing.T) {
	var handlerCalls, remoteCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls.Add(1)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"remote\":true}"}]}}`))
	}))
	defer server.Close()
	node := handlerNode()
	delete(node.Meta.Tags, meegle.TagExecutorKind)
	cfg := handlerConfig(node, func(context.Context, *executor.Request) (*executor.RawResult, error) {
		handlerCalls.Add(1)
		return nil, nil
	})
	cfg.BusinessClient = mcpclient.New(server.URL)
	var out, stderr bytes.Buffer
	if err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if handlerCalls.Load() != 0 || remoteCalls.Load() != 1 || !strings.Contains(out.String(), `"remote": true`) {
		t.Fatalf("handler=%d remote=%d out=%s", handlerCalls.Load(), remoteCalls.Load(), &out)
	}
}

type singleMarshal struct{ calls *atomic.Int64 }

func (s singleMarshal) MarshalJSON() ([]byte, error) {
	if s.calls.Add(1) > 1 {
		panic("unexpected repeated serialization")
	}
	return []byte(`{"count":9007199254740993}`), nil
}
func TestProductHandlerSerializesResultOnce(t *testing.T) {
	var calls atomic.Int64
	cfg := handlerConfig(handlerNode(), func(context.Context, *executor.Request) (*executor.RawResult, error) {
		return &executor.RawResult{Data: singleMarshal{&calls}}, nil
	})
	var out, stderr bytes.Buffer
	err := handlerApp(t, cfg).ExecuteWithIO(context.Background(), []string{"echo", "--item-name", "ok", "--select", "count", "--envelope", "--format", "json"}, &out, &stderr)
	if err != nil || calls.Load() != 1 || !strings.Contains(out.String(), "9007199254740993") {
		t.Fatalf("err=%v calls=%d out=%s", err, calls.Load(), &out)
	}
}
