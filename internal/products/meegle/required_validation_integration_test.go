// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

func TestCLIWorkflowListStateTransitionsAggregatesMissingRequiredFlags(t *testing.T) {
	for _, format := range []string{"json", "ndjson", "table"} {
		t.Run(format, func(t *testing.T) {
			app := newRequiredValidationCLIApp(t)
			var stdout, stderr bytes.Buffer
			err := app.ExecuteWithIO(context.Background(), []string{
				"workflow", "list-state-transitions",
				"--project-key", "demo",
				"--work-item-id", "1",
				"--dry-run",
				"--format", format,
			}, &stdout, &stderr)
			if err == nil {
				t.Fatal("expected missing required flags error")
			}
			var me *meerrors.MeegleError
			if !errors.As(err, &me) {
				t.Fatalf("error = %T, want *MeegleError", err)
			}
			if got, want := me.Message, "missing required parameters: --user-key, --work-item-type"; got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
			if me.Code != "CLIENT_MISSING_REQUIRED" || me.ExitCode != 1 {
				t.Fatalf("code/exit = %s/%d, want CLIENT_MISSING_REQUIRED/1", me.Code, me.ExitCode)
			}
			if got, want := me.Suggestion, "meegle workflow list-state-transitions --help"; got != want {
				t.Fatalf("suggestion = %q, want %q", got, want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, fragment := range []string{
				"CLIENT_MISSING_REQUIRED",
				"--work-item-type",
				"--user-key",
				"workflow list-state-transitions --help",
			} {
				if !strings.Contains(stderr.String(), fragment) {
					t.Fatalf("%s stderr missing %q:\n%s", format, fragment, stderr.String())
				}
			}
		})
	}
}

func TestCLIWorkflowListStateTransitionsAcceptsRequiredInputsFromAllSources(t *testing.T) {
	app := newRequiredValidationCLIApp(t)
	result, err := app.Invoke(context.Background(), []string{
		"workflow", "list-state-transitions",
		"--project-key", "demo",
		"--work-item-id", "1",
		"--params", `{"work_item_type":"story"}`,
		"--set", "user_key=user-1",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	payload, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", result.Data)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("params = %#v", payload["params"])
	}
	for key, want := range map[string]string{
		"project_key":    "demo",
		"work_item_id":   "1",
		"work_item_type": "story",
		"user_key":       "user-1",
	} {
		if got := fmt.Sprint(params[key]); got != want {
			t.Fatalf("params[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func newRequiredValidationCLIApp(t *testing.T) *cliapp.App {
	t.Helper()
	lister := &successLister{tools: []types.ToolDefinition{{
		Name:        "get_transitable_states",
		Description: "List state transitions",
		Parameters: []types.ToolParameter{
			{Name: "user_key", Type: "string", Required: true},
			{Name: "project_key", Type: "string", Required: true},
			{Name: "work_item_id", Type: "number", Required: true},
			{Name: "work_item_type", Type: "string", Required: true},
		},
	}}}
	setup := NewDynamicRegistrySetup(lister, nil, WithGlobalFlags(MeegleGlobalFlags))
	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion("test"),
		cliapp.WithSetup(setup),
		cliapp.WithPipelineFactory(newPipelineFactory(setup, nil, nil)),
	)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("discovery calls = %d, want 1", lister.calls)
	}
	return app
}

func TestProductSuggestionsCLI(t *testing.T) {
	for _, args := range [][]string{
		{"workflow", "list-state-transitions"},
		{"attachment", "+upload", "sample.txt", "--resource-type", "13", "--project-key", "sample"},
		{"workitem", "+batch-get"},
	} {
		var baselineError *meerrors.MeegleError
		var baselineStderr string
		for _, name := range []string{"meegle", "sample-cli", ""} {
			lister := &successLister{tools: []types.ToolDefinition{
				{Name: "get_transitable_states", Parameters: []types.ToolParameter{{Name: "project_key", Type: "string", Required: true}}},
				{Name: "get_workitem_brief", Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer", Required: true}}},
				{Name: "upload_file"},
			}}
			setup := NewDynamicRegistrySetup(lister, nil)
			app, err := cliapp.New(cliapp.WithAppName(name), cliapp.WithSetup(setup), cliapp.WithPipelineFactory(newPipelineFactory(setup, nil, nil)))
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			err = app.ExecuteWithIO(context.Background(), append(append([]string(nil), args...), "--dry-run", "--format", "json"), &stdout, &stderr)
			var me *meerrors.MeegleError
			if !errors.As(err, &me) || stdout.Len() != 0 {
				t.Fatalf("name=%q args=%v error=%v stdout=%s", name, args, err, stdout.String())
			}
			if name == "meegle" {
				baselineError = me
				baselineStderr = stderr.String()
				continue
			}
			wantName := name
			if wantName == "" {
				wantName = "meegle"
			}
			if me.Suggestion != strings.ReplaceAll(baselineError.Suggestion, "meegle", wantName) || me.Code != baselineError.Code || me.Message != baselineError.Message || me.ExitCode != baselineError.ExitCode {
				t.Fatalf("name=%q baseline=%#v got=%#v", name, baselineError, me)
			}
			if strings.ReplaceAll(stderr.String(), wantName, "meegle") != baselineStderr {
				t.Fatalf("JSON error changed beyond product name: %s / %s", baselineStderr, stderr.String())
			}
		}
	}
}
