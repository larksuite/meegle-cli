// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/fileio"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

func TestFileIOPublicCommandAndReuse(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input file.json"), filepath.Join(dir, "output file.json")
	os.WriteFile(input, []byte(`{"key":"from-file","priority":1,"id":9007199254740993}`), 0600)
	calls := 0
	app, err := newTestAppWithHandler(func(_ context.Context, req *executor.Request) (*executor.RawResult, error) {
		calls++
		if _, ok := req.Values["input-file"]; ok {
			t.Fatal("input option leaked")
		}
		if _, ok := req.Values["output-file"]; ok {
			t.Fatal("output option leaked")
		}
		return &executor.RawResult{Data: map[string]any{"received": req.Values, "total": 3}, Metadata: map[string]any{"logid": "test-log"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"workitem", "create", "--input-file", "@" + input, "--set", "priority=2", "--key", "direct", "--output-file", fmt.Sprintf(`{"path":%q,"fields":["received"]}`, output)}
	if err := app.ExecuteWithIO(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("%v %s", err, stderr.String())
	}
	data, _ := os.ReadFile(output)
	if !strings.Contains(string(data), `"key":"direct"`) || !strings.Contains(string(data), `"priority":2`) || !strings.Contains(string(data), `9007199254740993`) {
		t.Fatalf("file=%s", data)
	}
	if strings.Contains(stdout.String(), "received") && strings.Contains(stdout.String(), "direct") {
		t.Fatal("exported content leaked")
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["meta"].(map[string]any)["logid"] != "test-log" {
		t.Fatal(envelope)
	}
	stdout.Reset()
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "second"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "second") || strings.Contains(stdout.String(), "output_files") || calls != 2 {
		t.Fatalf("reuse=%s calls=%d", stdout.String(), calls)
	}
	stdout.Reset()
	if err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create"}, &stdout, &stderr); err == nil || calls != 2 {
		t.Fatal("old required parameter reused")
	}
}

func TestFileIORejectsBeforeBusinessCall(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	os.WriteFile(input, []byte(`{"key":"file"}`), 0600)
	output := fmt.Sprintf(`{"path":%q,"fields":["key"]}`, filepath.Join(dir, "out.json"))
	cases := [][]string{
		{"--input-file", "@" + input, "--input-file", "@" + input},
		{"--input-file", "@" + input, "--params", "{}"},
		{"--input-file", input},
		{"--input-file", "@" + input + "-missing"},
		{"--output-file", output, "--format", "ndjson"},
		{"--output-file", output, "--select", "key"},
		{"--output-file", output, "--dry-run"},
		{"--output-file", output, "--envelope=false"},
		{"--params", `{"output-file":[]}`},
		{"--set", "input-file=@file.json"},
		{"--output-file", "[]"},
	}
	for i, args := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			called := false
			app, _ := newTestAppWithHandler(func(context.Context, *executor.Request) (*executor.RawResult, error) { called = true; return nil, nil })
			var stdout, stderr bytes.Buffer
			err := app.ExecuteWithIO(context.Background(), append([]string{"workitem", "create", "--key", "x"}, args...), &stdout, &stderr)
			if err == nil || called || stdout.Len() != 0 {
				t.Fatalf("err=%v called=%v stdout=%s", err, called, stdout.String())
			}
		})
	}
}

func TestFileIOControlsCannotComeFromInput(t *testing.T) {
	for _, key := range []string{"profile", "format", "output-file", "output_file", "help", "version"} {
		t.Run(key, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.json")
			os.WriteFile(input, []byte(fmt.Sprintf(`{"key":"x",%q:"bad"}`, key)), 0600)
			tree := &registry.CommandTree{GlobalFlags: []registry.FlagDef{{Name: "profile", Type: registry.FlagTypeString}}, Nodes: []*registry.CommandNode{{Name: "run", Help: registry.HelpText{Brief: "Run"}, HandlerRef: "run", Flags: []registry.FlagDef{{Name: "key", Type: registry.FlagTypeString}}}}}
			app, err := New(WithSetup(registry.NewStaticSetup(tree)))
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Invoke(context.Background(), []string{"run", "--input-file", "@" + input})
			if err == nil || !strings.Contains(err.Error(), "control option") {
				t.Fatal(err)
			}
		})
	}
}

type failingFileStdout struct{ panicInstead bool }

func (w failingFileStdout) Write([]byte) (int, error) {
	if w.panicInstead {
		panic("broken writer")
	}
	return 0, errors.New("broken pipe")
}

type shortFileStdout struct{}

func (shortFileStdout) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestFileIOShortStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	app, _ := newTestApp()
	var stderr bytes.Buffer
	err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "x", "--output-file", fmt.Sprintf(`{"path":%q,"fields":["key"]}`, path)}, shortFileStdout{}, &stderr)
	var failure *fileio.OutputError
	if !errors.As(err, &failure) || failure.Stage != "stdout" || len(failure.Files) != 1 {
		t.Fatalf("err=%v", err)
	}
}

func TestFileIOHelpAndSliceDefaultsDoNotLeak(t *testing.T) {
	tree := &registry.CommandTree{Nodes: []*registry.CommandNode{{Name: "run", Help: registry.HelpText{Brief: "Run"}, HandlerRef: "run", Flags: []registry.FlagDef{{Name: "tags", Type: registry.FlagTypeStringArray, Default: []string{"default"}}}}}}
	app, err := New(WithSetup(registry.NewStaticSetup(tree)), WithExecutor(executor.NewDirectExecutor(map[string]executor.Handler{"run": func(_ context.Context, req *executor.Request) (*executor.RawResult, error) {
		return &executor.RawResult{Data: req.Values}, nil
	}})))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := app.ExecuteWithIO(context.Background(), []string{"run", "--help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"first", "second"} {
		stdout.Reset()
		if err := app.ExecuteWithIO(context.Background(), []string{"run", "--tags", tag}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		var got struct{ Tags []string }
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout.String())
		}
		if len(got.Tags) != 1 || got.Tags[0] != tag {
			t.Fatalf("tags=%v", got.Tags)
		}
	}
}

func TestFileIOPublishedFilesSurviveStdoutFailure(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint(panics), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.json")
			app, _ := newTestApp()
			var stderr bytes.Buffer
			err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "x", "--output-file", fmt.Sprintf(`{"path":%q,"fields":["key"]}`, path)}, failingFileStdout{panicInstead: panics}, &stderr)
			var failure *fileio.OutputError
			if !errors.As(err, &failure) || failure.Stage != "stdout" || len(failure.Files) != 1 {
				t.Fatalf("err=%v", err)
			}
			data, _ := os.ReadFile(path)
			if string(data) != "{\"key\":\"x\"}\n" {
				t.Fatalf("file=%s", data)
			}
			if !strings.Contains(stderr.String(), "FILE_OUTPUT_FAILED") || !strings.Contains(stderr.String(), "output_files") {
				t.Fatalf("stderr=%s", stderr.String())
			}
		})
	}
}

func TestFileIOPartialFailureMetadataThroughCommand(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	calls := 0
	app, _ := newTestAppWithHandler(func(context.Context, *executor.Request) (*executor.RawResult, error) {
		calls++
		os.WriteFile(b, []byte("racing writer"), 0600)
		return &executor.RawResult{Data: map[string]any{"a": 1, "b": 2}}, nil
	})
	var stdout, stderr bytes.Buffer
	err := app.ExecuteWithIO(context.Background(), []string{"workitem", "create", "--key", "x", "--output-file", fmt.Sprintf(`[{"path":%q,"fields":["a"]},{"path":%q,"fields":["b"]}]`, a, b)}, &stdout, &stderr)
	var failure *fileio.OutputError
	if !errors.As(err, &failure) || len(failure.Files) != 1 || calls != 1 || stdout.Len() != 0 {
		t.Fatalf("err=%v calls=%d stdout=%s", err, calls, stdout.String())
	}
	if !strings.Contains(stderr.String(), "completed") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestFileIOBusinessErrorsAndNilResults(t *testing.T) {
	for _, kind := range []string{"nil", "typed-nil", "error", "result-error", "missing-field"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			app, _ := newTestAppWithHandler(func(context.Context, *executor.Request) (*executor.RawResult, error) {
				switch kind {
				case "nil":
					return nil, nil
				case "typed-nil":
					return &executor.RawResult{Data: map[string]any(nil)}, nil
				case "error":
					return nil, errors.New("business failed")
				case "result-error":
					return &executor.RawResult{Data: map[string]any{"key": 1}, Err: errors.New("business failed")}, nil
				default:
					return &executor.RawResult{Data: map[string]any{"other": 1}}, nil
				}
			})
			_, err := app.Invoke(context.Background(), []string{"workitem", "create", "--key", "x", "--output-file", fmt.Sprintf(`{"path":%q,"fields":["key"]}`, filepath.Join(dir, "out.json"))})
			if err == nil {
				t.Fatal("invalid result accepted")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatalf("unexpected files: %v", entries)
			}
		})
	}
}
