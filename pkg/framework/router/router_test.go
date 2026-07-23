// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"testing"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

func TestCommandRouterRoutesAndSeparatesExplicitFlags(t *testing.T) {
	manager := registry.NewManager(registry.NewStaticSetup(testRouterTree()))
	if err := manager.Init(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	r, err := NewCommandRouter(manager, "test")
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	parsed, err := r.Route(&frameworkadapter.RawInput{Context: context.Background(), Args: []string{"workitem", "update", "WI-1", "--query", "abc"}})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if parsed == nil || parsed.Node == nil || parsed.Node.Name != "update" {
		t.Fatalf("parsed node = %#v", parsed)
	}
	if got := parsed.Args[0]; got != "WI-1" {
		t.Fatalf("arg id = %q", got)
	}
	if got := parsed.Flags["limit"]; got != 20 {
		t.Fatalf("effective limit = %#v", got)
	}
	if got := parsed.Flags["query"]; got != "abc" {
		t.Fatalf("effective query = %#v", got)
	}
	if _, ok := parsed.ExplicitFlags["limit"]; ok {
		t.Fatalf("limit should not be explicit: %#v", parsed.ExplicitFlags)
	}
	if got := parsed.ExplicitFlags["query"]; got != "abc" {
		t.Fatalf("explicit query = %#v", got)
	}
}

func TestCommandRouterDefersRequiredFlagValidationToPipeline(t *testing.T) {
	r := newTestRouter(t)
	parsed, err := r.Route(&frameworkadapter.RawInput{Context: context.Background(), Args: []string{"workitem", "create"}})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if parsed == nil || parsed.Node == nil || parsed.Node.Name != "create" {
		t.Fatalf("parsed node = %#v", parsed)
	}
}

func TestCommandRouterWrapsUnknownCommandError(t *testing.T) {
	r := newTestRouter(t)
	_, err := r.Route(&frameworkadapter.RawInput{Context: context.Background(), Args: []string{"missing", "command"}})
	assertCLIError(t, err, frameworkerrors.CodeCommandNotFound)
}

func TestCommandRouterWrapsUnknownFlagError(t *testing.T) {
	r := newTestRouter(t)
	_, err := r.Route(&frameworkadapter.RawInput{Context: context.Background(), Args: []string{"workitem", "create", "--not-exist"}})
	assertCLIError(t, err, frameworkerrors.CodeParamInvalid)
}

// FlagTypeStringArray must bypass cobra's CSV splitting so JSON-shaped values
// survive verbatim. Each --flag invocation stores one element; commas and
// double quotes inside the value are preserved. This is the difference from
// FlagTypeStringSlice that makes --fields usable for object-shaped payloads.
func TestCommandRouterStringArrayPreservesJSONAndRepeats(t *testing.T) {
	manager := registry.NewManager(registry.NewStaticSetup(&registry.CommandTree{
		Nodes: []*registry.CommandNode{{
			Name:       "run",
			Help:       registry.HelpText{Brief: "Run with string-array flag"},
			HandlerRef: "core.run",
			Flags: []registry.FlagDef{
				{Name: "fields", Type: registry.FlagTypeStringArray},
			},
		}},
	}))
	if err := manager.Init(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	r, err := NewCommandRouter(manager, "test")
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	parsed, err := r.Route(&frameworkadapter.RawInput{Context: context.Background(), Args: []string{
		"run",
		"--fields", `{"field_key":"name","field_value":"A"}`,
		"--fields", `{"field_key":"priority","field_value":"P1"}`,
	}})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	raw, ok := parsed.Flags["fields"].([]string)
	if !ok {
		t.Fatalf("Flags[fields] type = %T, want []string", parsed.Flags["fields"])
	}
	want := []string{
		`{"field_key":"name","field_value":"A"}`,
		`{"field_key":"priority","field_value":"P1"}`,
	}
	if len(raw) != len(want) {
		t.Fatalf("got %d elements, want %d: %#v", len(raw), len(want), raw)
	}
	for i, w := range want {
		if raw[i] != w {
			t.Errorf("element %d = %q, want %q (CSV splitting must not fire for StringArray)", i, raw[i], w)
		}
	}
}

func newTestRouter(t *testing.T) *CommandRouter {
	t.Helper()
	manager := registry.NewManager(registry.NewStaticSetup(testRouterTree()))
	if err := manager.Init(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	r, err := NewCommandRouter(manager, "test")
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	return r
}

func testRouterTree() *registry.CommandTree {
	return &registry.CommandTree{Nodes: []*registry.CommandNode{{
		Name: "workitem",
		Help: registry.HelpText{Brief: "Manage work items"},
		Children: []*registry.CommandNode{{
			Name:       "update",
			Help:       registry.HelpText{Brief: "Update a work item"},
			HandlerRef: "core.workitem.update",
			Args:       []registry.ArgDef{{Name: "id", Required: true}},
			Flags: []registry.FlagDef{
				{Name: "query", Type: registry.FlagTypeString},
				{Name: "limit", Type: registry.FlagTypeInt, Default: 20},
			},
		}, {
			Name:       "create",
			Help:       registry.HelpText{Brief: "Create a work item"},
			HandlerRef: "core.workitem.create",
			Flags: []registry.FlagDef{
				{Name: "key", Type: registry.FlagTypeString, Required: true},
			},
		}},
	}}}
}

func assertCLIError(t *testing.T, err error, code string) {
	t.Helper()
	cliErr := frameworkerrors.As(err)
	if cliErr == nil {
		t.Fatalf("expected CLIError, got %T (%v)", err, err)
	}
	if cliErr.Code != code {
		t.Fatalf("error code = %q, want %q", cliErr.Code, code)
	}
}
