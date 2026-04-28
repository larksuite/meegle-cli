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

// successLister returns a fixed tool list and counts ListTools calls.
type successLister struct {
	tools []types.ToolDefinition
	calls int
}

func (s *successLister) ListTools(_ context.Context) ([]types.ToolDefinition, error) {
	s.calls++
	return s.tools, nil
}

// errorLister returns a fixed non-401 error and counts ListTools calls.
type errorLister struct {
	err   error
	calls int
}

func (e *errorLister) ListTools(_ context.Context) ([]types.ToolDefinition, error) {
	e.calls++
	return nil, e.err
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

	// 2 discovered groups (view, workitem). The attachment group only exists
	// when MCP discovery surfaces upload_file / get_download_url — these
	// commands don't include them, so attachment is absent here.
	if len(tree.Nodes) != 2 {
		t.Fatalf("expected 2 groups (view + workitem), got %d", len(tree.Nodes))
	}
	view := findNodeByName(tree.Nodes, "view")
	workitem := findNodeByName(tree.Nodes, "workitem")
	if view == nil || workitem == nil {
		t.Fatalf("missing expected groups: view=%v workitem=%v", view, workitem)
	}
	// view: 1 discovered child. workitem: 2 discovered + 1 injected +batch-get.
	if len(view.Children) != 1 {
		t.Errorf("view group: expected 1 child, got %d", len(view.Children))
	}
	if len(workitem.Children) != 3 {
		t.Errorf("workitem group: expected 3 children (2 discovered + +batch-get), got %d", len(workitem.Children))
	}
	// check HandlerRef
	if view.Children[0].HandlerRef != "get_view_detail" {
		t.Errorf("expected HandlerRef 'get_view_detail', got %q", view.Children[0].HandlerRef)
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

	// 1 discovered (workitem). Attachment group is absent because no
	// upload_file / get_download_url tools were supplied.
	if len(tree.Nodes) != 1 {
		t.Fatalf("expected 1 group (workitem), got %d", len(tree.Nodes))
	}
	workitem := findNodeByName(tree.Nodes, "workitem")
	if workitem == nil {
		t.Fatal("missing workitem group")
	}
	leaf := workitem.Children[0]
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

func TestDynamicRegistrySetup_GracefulNoClient(t *testing.T) {
	// nil client + nil cache → no MCP-discovered tools. All business commands
	// (including attachment) come from MCP discovery, so the tree is empty
	// and only framework built-ins remain after RootCommandCustomizer runs.
	setup := NewDynamicRegistrySetup(nil, nil)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Nodes) != 0 {
		t.Errorf("expected 0 nodes (no MCP discovery), got %d", len(tree.Nodes))
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
	// Discovery returned no MCP tools, so the tree is empty. attachment
	// commands come from MCP discovery now and follow the same fate as other
	// business commands when auth fails.
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
		t.Errorf("expected empty tree on 401 for SourceEnv, got %d nodes", len(tree.Nodes))
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

func TestResolveTools_FreshCacheSkipsDiscovery(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewToolCache(tmpDir, "default", DefaultTTL)
	if err := cache.Set([]types.ToolDefinition{{Name: "cached_tool"}}); err != nil {
		t.Fatalf("set cache: %v", err)
	}

	lister := &successLister{tools: []types.ToolDefinition{{Name: "fresh_tool"}}}
	setup := NewDynamicRegistrySetup(lister, cache)

	tools, err := setup.resolveTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lister.calls != 0 {
		t.Errorf("fresh cache should skip discovery, got %d calls", lister.calls)
	}
	if len(tools) != 1 || tools[0].Name != "cached_tool" {
		t.Errorf("expected cached tool, got %v", tools)
	}
}

func TestResolveTools_StaleCacheRefreshesFromServer(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewToolCache(tmpDir, "default", -1) // immediately stale
	if err := cache.Set([]types.ToolDefinition{{Name: "stale_tool"}}); err != nil {
		t.Fatalf("set cache: %v", err)
	}

	lister := &successLister{tools: []types.ToolDefinition{{Name: "fresh_tool"}}}
	// cache TTL=-1 only affects Get's Stale flag; recreate Set's cache uses
	// the same instance so the freshly written entry will also report Stale.
	setup := NewDynamicRegistrySetup(lister, cache)

	tools, err := setup.resolveTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lister.calls != 1 {
		t.Errorf("stale cache should trigger discovery, got %d calls", lister.calls)
	}
	if len(tools) != 1 || tools[0].Name != "fresh_tool" {
		t.Errorf("expected fresh tool, got %v", tools)
	}
	// Cache should be overwritten with fresh tools.
	persisted, _ := cache.Get()
	if persisted == nil || len(persisted.Tools) != 1 || persisted.Tools[0].Name != "fresh_tool" {
		t.Errorf("expected cache overwritten, got %v", persisted)
	}
}

func TestResolveTools_StaleCacheFallsBackOnDiscoveryError(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewToolCache(tmpDir, "default", -1)
	if err := cache.Set([]types.ToolDefinition{{Name: "stale_tool"}}); err != nil {
		t.Fatalf("set cache: %v", err)
	}

	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, cache)

	tools, err := setup.resolveTools(context.Background())
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if lister.calls != 1 {
		t.Errorf("expected 1 discovery attempt, got %d", lister.calls)
	}
	if len(tools) != 1 || tools[0].Name != "stale_tool" {
		t.Errorf("expected stale fallback tool, got %v", tools)
	}
}

func TestResolveTools_ForceRefreshBypassesFreshCache(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewToolCache(tmpDir, "default", DefaultTTL) // fresh
	if err := cache.Set([]types.ToolDefinition{{Name: "cached_tool"}}); err != nil {
		t.Fatalf("set cache: %v", err)
	}

	lister := &successLister{tools: []types.ToolDefinition{{Name: "fresh_tool"}}}
	setup := NewDynamicRegistrySetup(lister, cache, WithForceRefresh(true))

	tools, err := setup.resolveTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lister.calls != 1 {
		t.Errorf("force refresh should hit server, got %d calls", lister.calls)
	}
	if len(tools) != 1 || tools[0].Name != "fresh_tool" {
		t.Errorf("expected fresh tool, got %v", tools)
	}
}

func TestHasRefreshFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", []string{}, false},
		{"no flag", []string{"workitem.list"}, false},
		{"bare flag", []string{"--refresh"}, true},
		{"flag before subcommand", []string{"--refresh", "workitem.list"}, true},
		{"flag after subcommand", []string{"workitem.list", "--refresh"}, true},
		{"explicit true", []string{"--refresh=true"}, true},
		{"explicit false", []string{"--refresh=false"}, false},
		{"after end-of-flags", []string{"--", "--refresh"}, false},
		{"substring match", []string{"--name=--refresh"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasRefreshFlag(c.args); got != c.want {
				t.Errorf("hasRefreshFlag(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}
