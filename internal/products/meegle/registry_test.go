// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// unauthorizedLister is a ToolLister stub that always returns a 401 MeegleError,
// mirroring what mcpclient.Client.Call returns when refresh is not possible.
type unauthorizedLister struct{ calls int }

func (u *unauthorizedLister) ListTools(_ context.Context) ([]types.ToolDefinition, error) {
	u.calls++
	return nil, meerrors.NewServerError("SERVER_HTTP_ERROR", "server returned error (401)").
		WithHTTPStatus(401)
}

func TestBuildCommandTree_AnnotatesDerivedCommandSources(t *testing.T) {
	tree := buildCommandTree([]types.MappedCommand{
		{Resource: "workitem", Method: "get", ToolName: "get_workitem_brief"},
		{Resource: "user", Method: "search", ToolName: "search_user_info"},
		{Resource: "attachment", Method: "prepare-upload", ToolName: "upload_file"},
		{Resource: "attachment", Method: "prepare-download", ToolName: "get_download_url"},
	})
	tests := []struct {
		group, command, source, risk string
	}{
		{group: "workitem", command: "+batch-get", source: "batch", risk: "read"},
		{group: "user", command: "me", source: "sugar", risk: "read"},
		{group: "attachment", command: "+upload", source: "attachment", risk: "write"},
		{group: "attachment", command: "+download", source: "attachment", risk: "read"},
	}
	for _, test := range tests {
		group := findNodeByName(tree.Nodes, test.group)
		command := findChild(group, test.command)
		if command == nil {
			t.Fatalf("command %s/%s missing", test.group, test.command)
		}
		if command.Meta.Source != test.source || command.Meta.Risk != test.risk || command.Meta.CommandID == "" || command.Meta.ToolName == "" {
			t.Errorf("%s/%s metadata = %+v", test.group, test.command, command.Meta)
		}
	}
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

type mutableLister struct {
	mu    sync.RWMutex
	tools []types.ToolDefinition
	err   error
}

type blockingLister struct {
	started chan struct{}
	release chan struct{}
	tools   []types.ToolDefinition
}

func (l *blockingLister) ListTools(context.Context) ([]types.ToolDefinition, error) {
	close(l.started)
	<-l.release
	return l.tools, nil
}

func (l *mutableLister) ListTools(context.Context) ([]types.ToolDefinition, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]types.ToolDefinition(nil), l.tools...), l.err
}

func (l *mutableLister) set(tools []types.ToolDefinition, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tools = append([]types.ToolDefinition(nil), tools...)
	l.err = err
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

func TestDynamicRegistrySetup_DiscoveryErrorWithoutDegradationHardFails(t *testing.T) {
	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, nil, WithGlobalFlags(MeegleGlobalFlags))

	_, err := setup.Setup(context.Background())
	if err == nil {
		t.Fatal("expected discovery error without CLI degradation")
	}
	if !strings.Contains(err.Error(), "tool discovery failed") {
		t.Fatalf("expected tool discovery failure, got: %v", err)
	}
}

func TestDynamicRegistrySetup_DegradesDiscoveryErrorToPlaceholderTree(t *testing.T) {
	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, nil,
		WithGlobalFlags(MeegleGlobalFlags),
		WithDiscoveryFailureDegradation(true),
	)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected CLI degradation, got error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil placeholder tree")
	}
	if tree.Version != discoveryFailureFallbackVersion {
		t.Fatalf("expected fallback tree version, got %q", tree.Version)
	}
	if len(tree.GlobalFlags) == 0 {
		t.Error("expected global flags to stay registered")
	}
	workitem := findNodeByName(tree.Nodes, "workitem")
	if workitem == nil {
		t.Fatal("expected workitem placeholder node")
	}
	if workitem.HandlerRef != discoveryFailureHandlerRef {
		t.Errorf("expected discovery failure handler, got %q", workitem.HandlerRef)
	}
	if workitem.Meta.Tags[tagRouterAllowUnknownFlags] != "1" {
		t.Error("expected placeholder to allow unknown business flags")
	}
	if !strings.Contains(workitem.Meta.Tags[tagDiscoveryFailure], "network unreachable") {
		t.Errorf("expected original error in placeholder tag, got %q", workitem.Meta.Tags[tagDiscoveryFailure])
	}
}

// The placeholder tree must cover every business domain the CLI knows about,
// otherwise an unlisted domain falls through to cobra's "unknown command"
// instead of the clean TOOL_DISCOVERY_FAILED error. Deriving the domains from
// dynamic.FallbackResources keeps this complete as new tools are added; this
// test guards against regressing to a hand-maintained subset.
func TestDegradesDiscoveryError_CoversEveryFallbackDomain(t *testing.T) {
	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, nil,
		WithDiscoveryFailureDegradation(true),
	)

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("expected CLI degradation, got error: %v", err)
	}

	domains := dynamic.FallbackResources()
	if len(tree.Nodes) != len(domains) {
		t.Fatalf("placeholder node count = %d, want %d (one per fallback domain)", len(tree.Nodes), len(domains))
	}
	for _, name := range domains {
		node := findNodeByName(tree.Nodes, name)
		if node == nil {
			t.Errorf("missing placeholder node for domain %q", name)
			continue
		}
		if node.HandlerRef != discoveryFailureHandlerRef {
			t.Errorf("domain %q: handler = %q, want discovery-failure handler", name, node.HandlerRef)
		}
	}
	// "resource" specifically regressed before this guard existed.
	if findNodeByName(tree.Nodes, "resource") == nil {
		t.Error("expected resource domain to have a placeholder node")
	}
}

func TestCLIApp_DiscoveryErrorStillAllowsStaticCommand(t *testing.T) {
	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, nil,
		WithGlobalFlags(MeegleGlobalFlags),
		WithDiscoveryFailureDegradation(true),
	)
	ran := false
	authCmd := &cobra.Command{Use: "auth"}
	authCmd.AddCommand(&cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			_, _ = cmd.OutOrStdout().Write([]byte("static ok\n"))
			return nil
		},
	})

	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion("test"),
		cliapp.WithSetup(setup),
		cliapp.WithPipelineFactory(newPipelineFactory(setup, nil, nil)),
		cliapp.WithRootCommandCustomizer(rootCustomizer(&StaticCommands{Auth: authCmd}, nil, setup, platformruntime.Diagnostics{}, ResolvedIdentity{})),
	)
	if err != nil {
		t.Fatalf("expected app bootstrap to succeed, got: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = app.ExecuteWithIO(context.Background(), []string{"auth", "status"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected static command to run, got: %v\nstderr=%s", err, stderr.String())
	}
	if !ran {
		t.Fatal("expected static command handler to run")
	}
	if !strings.Contains(stdout.String(), "static ok") {
		t.Fatalf("expected static command output, got: %q", stdout.String())
	}
}

func TestCLIApp_DynamicMetadataCannotShadowStaticCommand(t *testing.T) {
	lister := &successLister{tools: []types.ToolDefinition{
		{
			Name:        "remote_auth_status",
			Description: "Remote command that must not shadow local authentication status",
			Metadata:    &types.ToolMetadata{Resource: "auth", Method: "status"},
		},
		{
			Name:        "enterprise_ping",
			Description: "Ping the enterprise extension",
			Metadata:    &types.ToolMetadata{Resource: "enterprise", Method: "ping"},
		},
	}}
	setup := NewDynamicRegistrySetup(lister, nil, WithGlobalFlags(MeegleGlobalFlags))
	ran := false
	authCmd := &cobra.Command{Use: "auth", Short: "Manage authentication"}
	authCmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show local authentication status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ran = true
			_, _ = cmd.OutOrStdout().Write([]byte("local auth status\n"))
			return nil
		},
	})

	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion("test"),
		cliapp.WithSetup(setup),
		cliapp.WithPipelineFactory(newPipelineFactory(setup, nil, nil)),
		cliapp.WithRootCommandCustomizer(rootCustomizer(&StaticCommands{Auth: authCmd}, nil, setup, platformruntime.Diagnostics{}, ResolvedIdentity{})),
	)
	if err != nil {
		t.Fatalf("build CLI app: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := app.ExecuteWithIO(context.Background(), []string{"auth", "status"}, &stdout, &stderr); err != nil {
		t.Fatalf("execute static command: %v\nstderr=%s", err, stderr.String())
	}
	if !ran || !strings.Contains(stdout.String(), "local auth status") {
		t.Fatalf("remote metadata shadowed static command: ran=%v stdout=%q", ran, stdout.String())
	}
	if findNodeByName(app.Registry().Tree().Nodes, "enterprise") == nil {
		t.Fatal("valid dynamic tool was lost while rejecting reserved path")
	}
	issues := setup.MappingIssues()
	if len(issues) != 1 || issues[0].Code != "reserved_path" || issues[0].ToolName != "remote_auth_status" {
		t.Fatalf("mapping issues = %+v, want deterministic reserved_path diagnostic", issues)
	}
}

func TestCompositeSetup_RemoteCatalogCannotShadowLocalRoots(t *testing.T) {
	localTree := newMeegleLocalCommandTree()
	tools := make([]types.ToolDefinition, 0, len(localTree.Nodes)+1)
	for _, node := range localTree.Nodes {
		tools = append(tools, types.ToolDefinition{
			Name:        "remote_" + strings.ReplaceAll(node.Name, "-", "_"),
			Description: "Remote command that must not shadow a local root",
			Metadata:    &types.ToolMetadata{Resource: node.Name, Method: "remote"},
		})
	}
	tools = append(tools, types.ToolDefinition{
		Name:        "enterprise_ping",
		Description: "Unrelated dynamic command",
		Metadata:    &types.ToolMetadata{Resource: "enterprise", Method: "ping"},
	})

	dynamicSetup := NewDynamicRegistrySetup(&successLister{tools: tools}, nil, WithGlobalFlags(MeegleGlobalFlags))
	setup := registry.NewCompositeSetup(dynamicSetup, NewMeegleLocalSetup())
	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("remote catalog collision must not reject the complete App setup: %v", err)
	}
	reg, err := registry.New(tree)
	if err != nil {
		t.Fatalf("registry.New() error = %v", err)
	}
	for _, node := range localTree.Nodes {
		if reg.GetByPath(node.Name) == nil {
			t.Errorf("local root %q was lost", node.Name)
		}
		if reg.GetByPath(node.Name+" remote") != nil {
			t.Errorf("remote catalog shadowed local root %q", node.Name)
		}
	}
	if reg.GetByPath("enterprise ping") == nil {
		t.Fatal("unrelated dynamic command was lost while isolating collisions")
	}

	issues := dynamicSetup.MappingIssues()
	if len(issues) != len(localTree.Nodes) {
		t.Fatalf("MappingIssues() = %+v, want one reserved_path issue per local root", issues)
	}
	for index, node := range localTree.Nodes {
		if issues[index].Code != "reserved_path" || issues[index].Path != node.Name+"/remote" {
			t.Errorf("MappingIssues()[%d] = %+v, want reserved local root %q", index, issues[index], node.Name)
		}
	}
}

func TestDynamicRegistrySetup_IsolatesInvalidAndDuplicateTools(t *testing.T) {
	lister := &successLister{tools: []types.ToolDefinition{
		{Name: "valid_without_description", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "ping"}},
		{Name: "invalid_resource", Description: "invalid", Metadata: &types.ToolMetadata{Resource: "corp_ops", Method: "run"}},
		{Name: "duplicate_path", Description: "duplicate", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "ping"}},
		{Name: "builtin_flag_conflict", Description: "invalid parameter", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "bad-flags"}, Parameters: []types.ToolParameter{{Name: "format", Type: "string"}}},
		{Name: "empty_flag_name", Description: "invalid parameter", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "empty-flag"}, Parameters: []types.ToolParameter{{Name: "", Type: "string"}}},
		{Name: "global_flag_conflict", Description: "invalid parameter", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "bad-global-flag"}, Parameters: []types.ToolParameter{{Name: "profile", Type: "string"}}},
		{Name: "implicit_flag_conflict", Description: "invalid parameter", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "bad-help-flag"}, Parameters: []types.ToolParameter{{Name: "help", Type: "boolean"}}},
		{Name: "partial_metadata", Description: "invalid metadata", Metadata: &types.ToolMetadata{Resource: "enterprise"}},
		{Name: "unknown_without_metadata", Description: "cannot be mapped"},
	}}
	setup := NewDynamicRegistrySetup(lister, nil, WithGlobalFlags(MeegleGlobalFlags))

	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("one invalid tool must not reject the complete tools/list response: %v", err)
	}
	enterprise := findNodeByName(tree.Nodes, "enterprise")
	if enterprise == nil {
		t.Fatal("valid dynamic resource missing")
	}
	ping := findChild(enterprise, "ping")
	if ping == nil || ping.HandlerRef != "valid_without_description" {
		t.Fatalf("enterprise/ping = %+v, want first valid tool", ping)
	}
	if strings.TrimSpace(ping.Help.Brief) == "" {
		t.Fatal("missing optional MCP description must receive a safe help fallback")
	}
	if findChild(enterprise, "bad-flags") != nil || findChild(enterprise, "empty-flag") != nil || findChild(enterprise, "bad-global-flag") != nil || findChild(enterprise, "bad-help-flag") != nil || findNodeByName(tree.Nodes, "corp_ops") != nil {
		t.Fatalf("invalid tools leaked into command tree: %+v", tree.Nodes)
	}

	issues := setup.MappingIssues()
	if len(issues) != 8 {
		t.Fatalf("mapping issues = %+v, want one issue per rejected tool", issues)
	}
	wantCodes := []string{"invalid_tool_definition", "missing_mapping", "invalid_command", "duplicate_path", "invalid_command", "invalid_command", "invalid_command", "invalid_command"}
	for i, want := range wantCodes {
		if issues[i].Code != want {
			t.Fatalf("issue[%d] = %+v, want code %q", i, issues[i], want)
		}
	}
	if validation := registry.ValidateTree(tree); validation.HasErrors() {
		t.Fatalf("isolated tree must remain valid: %v", validation)
	}
}

func TestDynamicRegistrySetup_SanitizesAcceptedHelpText(t *testing.T) {
	setup := NewDynamicRegistrySetup(&successLister{tools: []types.ToolDefinition{{
		Name: "enterprise_help", Description: "Safe\nFORGED\x1b[31m red\u009b31m hidden",
		Metadata:   &types.ToolMetadata{Resource: "enterprise", Method: "help-text"},
		Parameters: []types.ToolParameter{{Name: "value", Type: "string", Description: "Value\r\nFAKE\x1b[32m green\u0085next"}},
	}}}, nil)
	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	command := findChild(findNodeByName(tree.Nodes, "enterprise"), "help-text")
	if command == nil || strings.ContainsAny(command.Help.Brief, "\r\n\x1b\u009b") {
		t.Fatalf("unsafe command help = %q", command.Help.Brief)
	}
	if len(command.Flags) != 1 || strings.ContainsAny(command.Flags[0].Description, "\r\n\x1b\u0085") {
		t.Fatalf("unsafe flag help = %+v", command.Flags)
	}
}

func TestDynamicRegistrySetup_MappedCommandsReturnsDeepCopy(t *testing.T) {
	setup := NewDynamicRegistrySetup(&successLister{tools: []types.ToolDefinition{{
		Name: "enterprise_tags", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "tags"},
		Parameters: []types.ToolParameter{{Name: "tags", Type: "array", Items: &types.ParameterItems{Type: "string"}}},
	}}}, nil)
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	commands := setup.MappedCommands()
	commands[0].Parameters[0].Name = "mutated"
	commands[0].Parameters[0].Items.Type = "number"

	snapshot := setup.MappedCommands()
	if snapshot[0].Parameters[0].Name != "tags" || snapshot[0].Parameters[0].Items.Type != "string" {
		t.Fatalf("MappedCommands leaked mutable snapshot state: %+v", snapshot)
	}
}

func TestDynamicRegistrySetup_SlowRebuildDoesNotBlockSnapshotReads(t *testing.T) {
	setup := NewDynamicRegistrySetup(&successLister{tools: []types.ToolDefinition{{
		Name: "enterprise_ping", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "ping"},
	}}}, nil)
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("initial Setup() error = %v", err)
	}

	blocked := &blockingLister{
		started: make(chan struct{}),
		release: make(chan struct{}),
		tools: []types.ToolDefinition{{
			Name: "enterprise_pong", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "pong"},
		}},
	}
	setup.client = blocked
	setupDone := make(chan error, 1)
	go func() {
		_, err := setup.Setup(context.Background())
		setupDone <- err
	}()
	<-blocked.started

	readDone := make(chan []types.MappedCommand, 1)
	go func() { readDone <- setup.MappedCommands() }()
	select {
	case commands := <-readDone:
		if len(commands) != 1 || commands[0].Method != "ping" {
			t.Fatalf("snapshot during rebuild = %+v, want previous coherent snapshot", commands)
		}
	case <-time.After(500 * time.Millisecond):
		close(blocked.release)
		<-setupDone
		t.Fatal("MappedCommands blocked on in-flight discovery")
	}
	close(blocked.release)
	if err := <-setupDone; err != nil {
		t.Fatalf("rebuild Setup() error = %v", err)
	}
}

func TestDynamicRegistrySetup_RebuildPublishesCoherentState(t *testing.T) {
	lister := &mutableLister{err: meerrors.NewServerError("SERVER_HTTP_ERROR", "unauthorized").WithHTTPStatus(401)}
	setup := NewDynamicRegistrySetup(lister, nil,
		WithIdentitySource(SourceEnv), WithDiscoveryFailureDegradation(true), WithGlobalFlags(MeegleGlobalFlags))
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("401 setup: %v", err)
	}
	if !setup.AuthFailed() {
		t.Fatal("401 setup did not publish authFailed")
	}

	lister.set([]types.ToolDefinition{{Name: "enterprise_ping", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "ping"}}}, nil)
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("successful rebuild: %v", err)
	}
	if setup.AuthFailed() || len(setup.MappedCommands()) != 1 {
		t.Fatalf("successful rebuild state: authFailed=%v commands=%+v", setup.AuthFailed(), setup.MappedCommands())
	}

	lister.set([]types.ToolDefinition{{Name: "bad", Metadata: &types.ToolMetadata{Resource: "corp_ops", Method: "run"}}}, nil)
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("invalid mapping rebuild: %v", err)
	}
	if len(setup.MappingIssues()) != 1 {
		t.Fatalf("invalid rebuild issues = %+v", setup.MappingIssues())
	}
	lister.set(nil, stderrors.New("network unavailable"))
	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("degraded rebuild: %v", err)
	}
	if setup.AuthFailed() || len(setup.MappedCommands()) != 0 || len(setup.MappingIssues()) != 0 {
		t.Fatalf("degraded snapshot retained stale state: auth=%v commands=%+v issues=%+v",
			setup.AuthFailed(), setup.MappedCommands(), setup.MappingIssues())
	}
}

func TestDynamicRegistrySetup_ConcurrentRebuildAndSnapshotReads(t *testing.T) {
	lister := &mutableLister{tools: []types.ToolDefinition{{Name: "enterprise_ping", Metadata: &types.ToolMetadata{Resource: "enterprise", Method: "ping"}}}}
	setup := NewDynamicRegistrySetup(lister, nil)
	var wait sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 50; iteration++ {
				_, _ = setup.Setup(context.Background())
			}
		}()
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 200; iteration++ {
				_ = setup.MappedCommands()
				_ = setup.MappingIssues()
				_ = setup.AuthFailed()
			}
		}()
	}
	wait.Wait()
}

func TestDynamicRegistrySetup_RejectsFallbackPathAliasWhenCanonicalToolIsAbsent(t *testing.T) {
	setup := NewDynamicRegistrySetup(&successLister{tools: []types.ToolDefinition{{
		Name: "server_alias", Description: "attempted alias",
		Metadata: &types.ToolMetadata{Resource: "workitem", Method: "get"},
	}}}, nil)
	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if get := findChild(findNodeByName(tree.Nodes, "workitem"), "get"); get != nil {
		t.Fatalf("server alias occupied immutable fallback path: %+v", get)
	}
	issues := setup.MappingIssues()
	if len(issues) != 1 || issues[0].Code != "reserved_path" || issues[0].ToolName != "server_alias" {
		t.Fatalf("MappingIssues() = %+v", issues)
	}
}

func TestDynamicRegistrySetup_KnownFallbackWinsDuplicatePathRegardlessOfServerOrder(t *testing.T) {
	lister := &successLister{tools: []types.ToolDefinition{
		{Name: "server_alias", Description: "attempted alias", Metadata: &types.ToolMetadata{Resource: "workitem", Method: "get"}},
		{Name: "get_workitem_brief", Description: "known tool"},
	}}
	setup := NewDynamicRegistrySetup(lister, nil)
	tree, err := setup.Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	get := findChild(findNodeByName(tree.Nodes, "workitem"), "get")
	if get == nil || get.HandlerRef != "get_workitem_brief" {
		t.Fatalf("workitem/get = %+v, want built-in compatibility mapping", get)
	}
	issues := setup.MappingIssues()
	if len(issues) != 1 || issues[0].ToolName != "server_alias" || issues[0].Code != "reserved_path" {
		t.Fatalf("MappingIssues() = %+v", issues)
	}
}

func TestCLIApp_DiscoveryErrorDynamicCommandReturnsClearError(t *testing.T) {
	lister := &errorLister{err: stderrors.New("network unreachable")}
	setup := NewDynamicRegistrySetup(lister, nil,
		WithGlobalFlags(MeegleGlobalFlags),
		WithDiscoveryFailureDegradation(true),
	)
	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion("test"),
		cliapp.WithSetup(setup),
		cliapp.WithPipelineFactory(newPipelineFactory(setup, nil, nil)),
	)
	if err != nil {
		t.Fatalf("expected app bootstrap to succeed, got: %v", err)
	}

	_, err = app.Invoke(context.Background(), []string{"workitem", "create", "--project-key", "P", "--dry-run"})
	if err == nil {
		t.Fatal("expected dynamic command discovery error")
	}
	var me *meerrors.MeegleError
	if !stderrors.As(err, &me) {
		t.Fatalf("expected MeegleError, got %T: %v", err, err)
	}
	if me.Code != "TOOL_DISCOVERY_FAILED" {
		t.Fatalf("expected TOOL_DISCOVERY_FAILED, got %q", me.Code)
	}
	if me.ExitCode != 2 {
		t.Fatalf("expected server-unreachable exit code 2, got %d", me.ExitCode)
	}
	if !strings.Contains(me.Message, "workitem create") || !strings.Contains(me.Message, "network unreachable") {
		t.Fatalf("expected command and original error in message, got: %s", me.Message)
	}
	if !strings.Contains(me.Suggestion, "auth status") {
		t.Fatalf("expected suggestion to mention local commands, got: %s", me.Suggestion)
	}
}

func TestDynamicRegistrySetup_Graceful401_ClearsTokenAndFlagsAuth(t *testing.T) {
	lister := &unauthorizedLister{}
	store := &memTokenStore{data: &auth.TokenData{AccessToken: "stale"}}
	tm := auth.NewTokenManager(store, "example.com")

	setup := NewDynamicRegistrySetup(lister, nil,
		WithTokenManager(tm),
		WithActiveToken("stale"),
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

func TestDynamicRegistrySetup_Graceful401_PreservesNewerStoredToken(t *testing.T) {
	lister := &unauthorizedLister{}
	store := &memTokenStore{data: &auth.TokenData{AccessToken: "fresh"}}
	tm := auth.NewTokenManager(store, "example.com")

	setup := NewDynamicRegistrySetup(lister, nil,
		WithTokenManager(tm),
		WithActiveToken("stale"),
		WithIdentitySource(SourceStore),
	)

	if _, err := setup.Setup(context.Background()); err != nil {
		t.Fatalf("expected graceful degradation on 401, got error: %v", err)
	}
	if store.cleared {
		t.Fatal("stale process cleared a token refreshed by another process")
	}
	if store.data == nil || store.data.AccessToken != "fresh" {
		t.Fatalf("newer token was not preserved: %+v", store.data)
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
