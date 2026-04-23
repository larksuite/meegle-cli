// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// unauthorizedLister is a ToolLister stub that always returns a 401 MeegleError,
// mirroring what mcpclient.Client.Call returns when refresh is not possible.
type unauthorizedLister struct{ calls int }

func (u *unauthorizedLister) ListTools(_ context.Context) ([]types.ToolDefinition, error) {
	u.calls++
	return nil, meerrors.NewServerError("SERVER_HTTP_ERROR", "server returned error (401)").
		WithHTTPStatus(401)
}

// memTokenStore is an in-memory auth.TokenStore for tests.
type memTokenStore struct {
	data    *auth.TokenData
	cleared bool
}

func (m *memTokenStore) Load() (*auth.TokenData, error) { return m.data, nil }
func (m *memTokenStore) Save(d *auth.TokenData) error   { m.data = d; return nil }
func (m *memTokenStore) Clear() error                   { m.data = nil; m.cleared = true; return nil }

func TestBuildCommandTree_GroupsByResource(t *testing.T) {
	commands := []types.MappedCommand{
		{Resource: "workitem", Method: "get", ToolName: "get_workitem_brief", Description: "Get work item brief"},
		{Resource: "workitem", Method: "create", ToolName: "create_workitem", Description: "Create work item"},
		{Resource: "view", Method: "get", ToolName: "get_view_detail", Description: "Get view detail"},
	}

	tree := buildCommandTree(commands)

	if len(tree.Nodes) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(tree.Nodes))
	}
	// sorted alphabetically: view, workitem
	if tree.Nodes[0].Name != "view" {
		t.Errorf("expected first group 'view', got %q", tree.Nodes[0].Name)
	}
	if tree.Nodes[1].Name != "workitem" {
		t.Errorf("expected second group 'workitem', got %q", tree.Nodes[1].Name)
	}
	// view: 1 discovered child. workitem: 2 discovered + 1 injected +batch-get.
	if len(tree.Nodes[0].Children) != 1 {
		t.Errorf("view group: expected 1 child, got %d", len(tree.Nodes[0].Children))
	}
	if len(tree.Nodes[1].Children) != 3 {
		t.Errorf("workitem group: expected 3 children (2 discovered + +batch-get), got %d", len(tree.Nodes[1].Children))
	}
	// check HandlerRef
	viewChild := tree.Nodes[0].Children[0]
	if viewChild.HandlerRef != "get_view_detail" {
		t.Errorf("expected HandlerRef 'get_view_detail', got %q", viewChild.HandlerRef)
	}
}

func TestBuildCommandTree_MapsParameterTypes(t *testing.T) {
	commands := []types.MappedCommand{
		{
			Resource: "workitem", Method: "test", ToolName: "test_tool",
			Parameters: []types.ToolParameter{
				{Name: "str_param", Type: "string", Description: "a string", Required: true},
				{Name: "num_param", Type: "number", Description: "a number"},
				{Name: "bool_param", Type: "boolean", Description: "a bool"},
				{Name: "arr_param", Type: "array", Description: "an array"},
				{Name: "obj_param", Type: "object", Description: "an object"},
				{Name: "url", Type: "string", Description: "should be skipped"},
			},
		},
	}

	tree := buildCommandTree(commands)

	if len(tree.Nodes) != 1 {
		t.Fatalf("expected 1 group, got %d", len(tree.Nodes))
	}
	leaf := tree.Nodes[0].Children[0]
	// url should be skipped → 5 flags
	if len(leaf.Flags) != 5 {
		t.Fatalf("expected 5 flags (url skipped), got %d", len(leaf.Flags))
	}

	expected := map[string]string{
		"str-param":  registry.FlagTypeString,
		"num-param":  registry.FlagTypeString,
		"bool-param": registry.FlagTypeBool,
		"arr-param":  registry.FlagTypeStringArray,
		"obj-param":  registry.FlagTypeString,
	}
	for _, f := range leaf.Flags {
		want, ok := expected[f.Name]
		if !ok {
			t.Errorf("unexpected flag %q", f.Name)
			continue
		}
		if f.Type != want {
			t.Errorf("flag %q: expected type %q, got %q", f.Name, want, f.Type)
		}
	}
	// check required
	for _, f := range leaf.Flags {
		if f.Name == "str-param" && !f.Required {
			t.Error("str-param should be required")
		}
	}
}

func TestDynamicRegistrySetup_FallsBackToStaticOnFailure(t *testing.T) {
	// nil client + nil cache → should return empty tree without error
	setup := NewDynamicRegistrySetup(nil, nil)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(tree.Nodes))
	}
}

func TestDynamicRegistrySetup_Graceful401_ClearsTokenAndFlagsAuth(t *testing.T) {
	lister := &unauthorizedLister{}
	store := &memTokenStore{data: &auth.TokenData{AccessToken: "stale"}}
	tm := auth.NewTokenManager(store, "example.com")

	setup := NewDynamicRegistrySetup(lister, nil,
		WithTokenManager(tm),
		WithIdentitySource(SourceStore),
	)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected graceful degradation on 401, got error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Nodes) != 0 {
		t.Errorf("expected empty tree on 401, got %d nodes", len(tree.Nodes))
	}
	if !setup.AuthFailed() {
		t.Error("expected AuthFailed() to be true after 401")
	}
	if !store.cleared {
		t.Error("expected token store to be cleared after terminal 401")
	}
}

// Env-var and config-token modes must also degrade silently so that --help,
// `auth login`, and other offline commands keep working when the server
// rejects the token. The rotation hint is surfaced separately by the root
// command's "Not logged in" note (see notLoggedInNote).
func TestDynamicRegistrySetup_Graceful401_EnvSourceDoesNotErrorOrClear(t *testing.T) {
	lister := &unauthorizedLister{}
	store := &memTokenStore{data: &auth.TokenData{AccessToken: "env_token"}}

	setup := NewDynamicRegistrySetup(lister, nil, WithIdentitySource(SourceEnv))

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected graceful degradation on 401 for SourceEnv, got error: %v", err)
	}
	if tree == nil || len(tree.Nodes) != 0 {
		t.Error("expected empty tree on 401 for SourceEnv")
	}
	if !setup.AuthFailed() {
		t.Error("expected AuthFailed() to be true after 401 for SourceEnv")
	}
	if store.cleared {
		t.Error("env-var mode must not clear a token it does not own")
	}
}

func TestDynamicRegistrySetup_Graceful401_ConfigSourceDoesNotError(t *testing.T) {
	lister := &unauthorizedLister{}

	setup := NewDynamicRegistrySetup(lister, nil, WithIdentitySource(SourceConfig))

	_, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected graceful degradation on 401 for SourceConfig, got error: %v", err)
	}
	if !setup.AuthFailed() {
		t.Error("expected AuthFailed() to be true after 401 for SourceConfig")
	}
}

// SDK path: caller injected the client without identifying the source. A 401
// must surface as an error so the SDK caller can react — there is no
// interactive shell to degrade to.
func TestDynamicRegistrySetup_401_UnsetSource_PropagatesError(t *testing.T) {
	lister := &unauthorizedLister{}

	setup := NewDynamicRegistrySetup(lister, nil)

	_, err := setup.Setup(context.Background())
	if err == nil {
		t.Fatal("expected error on 401 without IdentitySource, got nil")
	}
	var me *meerrors.MeegleError
	if !stderrors.As(err, &me) {
		t.Fatalf("expected MeegleError, got %T: %v", err, err)
	}
	if me.Code != "AUTH_REJECTED" {
		t.Errorf("expected code AUTH_REJECTED, got %q", me.Code)
	}
	if !strings.Contains(me.Message, "token rejected by server") {
		t.Errorf("expected 'token rejected by server' in message, got: %s", me.Message)
	}
	if !strings.Contains(me.Suggestion, "MEEGLE_USER_ACCESS_TOKEN") {
		t.Errorf("expected suggestion to mention MEEGLE_USER_ACCESS_TOKEN, got: %s", me.Suggestion)
	}
	if setup.AuthFailed() {
		t.Error("expected AuthFailed() to be false when error is propagated")
	}
}

func TestDynamicRegistrySetup_UsesCacheWhenAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewToolCache(tmpDir, "default", DefaultTTL)

	// Pre-fill cache with tools
	tools := []types.ToolDefinition{
		{
			Name:        "get_workitem_brief",
			Description: "Get work item brief",
			Parameters: []types.ToolParameter{
				{Name: "work_item_id", Type: "string", Required: true},
			},
		},
	}
	if err := cache.Set(tools); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// nil client → will rely on cache
	setup := NewDynamicRegistrySetup(nil, cache)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Nodes) == 0 {
		t.Error("expected nodes from cached tools, got 0")
	}
}
