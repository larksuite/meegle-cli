// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"strings"
	"testing"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

// shortcutNode looks up the +upload-entire / +download-entire node from the
// attachment group so tests use the production wiring instead of a hand-built
// fixture (catches drift when registration changes). The basic prepare-*
// commands now come from MCP discovery via dynamic.fallbackTable, so we feed
// buildCommandTree the same MappedCommands MapTools would produce for them.
func shortcutNode(t *testing.T, name string) *registry.CommandNode {
	t.Helper()
	tree := buildCommandTree(attachmentDiscoveryFixture())
	group := findNodeByName(tree.Nodes, "attachment")
	if group == nil {
		t.Fatal("attachment group not injected")
	}
	n := findChild(group, name)
	if n == nil {
		t.Fatalf("shortcut %s not registered", name)
	}
	return n
}

// stateFor builds a minimal PipelineContext that mimics the post-router state
// AttachmentShortcutStep observes: parsed flags + positional args + Values
// already merged. CLI mode (TTY) so dry-run/output paths behave like users
// see them.
func stateFor(t *testing.T, node *registry.CommandNode, flags map[string]any, args []string, dryRun bool) *pipeline.PipelineContext {
	t.Helper()
	if flags == nil {
		flags = map[string]any{}
	}
	if dryRun {
		flags["dry-run"] = true
	}
	parsed := &frameworkrouter.ParsedCommand{
		Node:          node,
		FullPath:      []string{"attachment", node.Name},
		Flags:         flags,
		ExplicitFlags: copyExplicit(flags),
		Args:          args,
	}
	state := &pipeline.PipelineContext{
		Input:  &frameworkadapter.RawInput{IOMode: frameworkadapter.IOModeTTY},
		Parsed: parsed,
		Values: pipeline.BuildInputValues(parsed),
	}
	return state
}

func copyExplicit(flags map[string]any) map[string]any {
	out := make(map[string]any, len(flags))
	for k, v := range flags {
		out[k] = v
	}
	return out
}

// TestAttachmentShortcut_FailsFastBeforeDryRun is the load-bearing assertion:
// validation must run BEFORE the dry-run short-circuit so a misconfigured
// invocation surfaces the real error instead of echoing the broken values.
func TestAttachmentShortcut_FailsFastBeforeDryRun(t *testing.T) {
	uploadNode := shortcutNode(t, "+upload")
	downloadNode := shortcutNode(t, "+download")

	cases := []struct {
		name     string
		node     *registry.CommandNode
		flags    map[string]any
		args     []string
		wantCode string
		wantMsg  string
	}{
		{
			name: "upload missing field-key for resource-type=15",
			node: uploadNode,
			flags: map[string]any{
				"resource-type": "15", "project-key": "X", "work-item-id": "1",
			},
			args:     []string{"/tmp/x"},
			wantCode: "CLIENT_MISSING_REQUIRED",
			wantMsg:  "--field-key is required",
		},
		{
			name: "upload missing both work-item-id and work-item-type",
			node: uploadNode,
			flags: map[string]any{
				"resource-type": "15", "project-key": "X", "field-key": "f",
			},
			args:     []string{"/tmp/x"},
			wantCode: "CLIENT_MISSING_REQUIRED",
			wantMsg:  "--work-item-id or --work-item-type",
		},
		{
			name: "upload non-integer resource-type",
			node: uploadNode,
			flags: map[string]any{
				"resource-type": "bogus", "project-key": "X", "work-item-id": "1", "field-key": "f",
			},
			args:     []string{"/tmp/x"},
			wantCode: "CLIENT_INVALID_VALUE",
			wantMsg:  "--resource-type must be an integer",
		},
		{
			name: "upload out-of-range resource-type",
			node: uploadNode,
			flags: map[string]any{
				"resource-type": "99", "project-key": "X", "work-item-id": "1", "field-key": "f",
			},
			args:     []string{"/tmp/x"},
			wantCode: "CLIENT_INVALID_VALUE",
			wantMsg:  "unknown --resource-type",
		},
		// resource-type=16 (rich-text-field-image) also requires field-key.
		{
			name: "upload resource-type=16 without field-key",
			node: uploadNode,
			flags: map[string]any{
				"resource-type": "16", "project-key": "X", "work-item-id": "1",
			},
			args:     []string{"/tmp/x"},
			wantCode: "CLIENT_MISSING_REQUIRED",
			wantMsg:  "--field-key is required",
		},
		// resource-type=13/14 (comment scope) do NOT require field-key, so
		// supplying just work-item-id should pass pre-flight (we only test
		// validation here — actual file open is exercised separately).
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := stateFor(t, c.node, c.flags, c.args, true /* dry-run */)
			step := &AttachmentShortcutStep{}
			err := step.Execute(context.Background(), state)
			if err == nil {
				t.Fatalf("expected error %s, got nil; state.Result=%+v", c.wantCode, state.Result)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantMsg)
			}
		})
	}

	// Sanity: a fully-valid invocation does succeed (proves the validation
	// gate isn't over-rejecting when all rules are satisfied).
	t.Run("upload happy path passes validation", func(t *testing.T) {
		state := stateFor(t, uploadNode, map[string]any{
			"resource-type": "15", "project-key": "X", "work-item-id": "1",
			"field-key": "f",
		}, []string{"/tmp/x"}, true)
		if err := (&AttachmentShortcutStep{}).Execute(context.Background(), state); err != nil {
			t.Errorf("happy path should pass validation, got: %v", err)
		}
	})
	t.Run("download happy path passes validation", func(t *testing.T) {
		state := stateFor(t, downloadNode, map[string]any{
			"project-key": "X", "work-item-id": "1", "output": "/tmp/y",
		}, []string{"meego://abc"}, true)
		if err := (&AttachmentShortcutStep{}).Execute(context.Background(), state); err != nil {
			t.Errorf("download happy path should pass validation, got: %v", err)
		}
	})
}

// TestMeegleValidate_RequiredArgs locks in the framework-layer guarantee that
// ArgDef.Required is enforced for production cobra invocations (which default
// to ArbitraryArgs and would otherwise silently swallow missing positionals).
func TestMeegleValidate_RequiredArgs(t *testing.T) {
	uploadNode := shortcutNode(t, "+upload")

	// All required flags satisfied, but no positional <source-path>.
	state := stateFor(t, uploadNode, map[string]any{
		"resource-type": "15", "project-key": "X", "work-item-id": "1",
		"field-key": "f",
	}, nil, false)

	err := (&MeegleValidateStep{}).Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected missing-positional error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required argument <source-path>") {
		t.Errorf("error should mention <source-path>, got: %v", err)
	}
}
