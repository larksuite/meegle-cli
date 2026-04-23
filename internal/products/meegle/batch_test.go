// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

// ---------------------------------------------------------------------------
// injectBatchCommands
// ---------------------------------------------------------------------------

// siblingGetNode returns a node that stands in for the MCP-discovered
// `workitem get` command: one required per-item flag plus the real
// optional shared flags that the current server actually wants.
func siblingGetNode() *registry.CommandNode {
	return &registry.CommandNode{
		Name:       "get",
		Help:       registry.HelpText{Brief: "get a work item"},
		HandlerRef: "feishu-project_workitem_data_skill.get_workitem_brief",
		Flags: []registry.FlagDef{
			{Name: "work-item-id", Type: registry.FlagTypeString, Required: true},
			{Name: "fields", Type: registry.FlagTypeStringSlice},
			{Name: "project-key", Type: registry.FlagTypeString},
		},
		Meta: registry.NodeMeta{Tags: map[string]string{
			"mcp_param_types": `{"work-item-id":"number","fields":"array","project-key":"string"}`,
			"mcp_param_items": `{"fields":"string"}`,
		}},
	}
}

func TestInjectBatchCommands_InheritsFromSibling(t *testing.T) {
	nodes := []*registry.CommandNode{
		{Name: "workitem", Children: []*registry.CommandNode{siblingGetNode()}},
		{Name: "view"},
	}
	injectBatchCommands(nodes)

	wi := nodes[0]
	if len(wi.Children) != 2 {
		t.Fatalf("expected 2 children after inject, got %d", len(wi.Children))
	}
	batch := findChild(wi, "+batch-get")
	if batch == nil {
		t.Fatal("+batch-get was not injected")
	}
	if batch.Meta.Tags[TagMcpBatch] != "1" {
		t.Errorf("expected TagMcpBatch=1, got %q", batch.Meta.Tags[TagMcpBatch])
	}
	if batch.Meta.Tags[TagMcpBatchPerItem] != "work_item_id" {
		t.Errorf("expected per-item param 'work_item_id', got %q", batch.Meta.Tags[TagMcpBatchPerItem])
	}
	if batch.HandlerRef != "feishu-project_workitem_data_skill.get_workitem_brief" {
		t.Errorf("expected HandlerRef inherited from sibling, got %q", batch.HandlerRef)
	}
	if !containsStr(batch.Aliases, "+get-batch") {
		t.Errorf("expected alias '+get-batch', got %v", batch.Aliases)
	}
	// Flags must inherit project-key and fields, drop work-item-id (replaced
	// by --work-item-ids), and add --ids-file.
	got := flagNames(batch.Flags)
	sort.Strings(got)
	want := []string{"fields", "ids-file", "project-key", "work-item-ids"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected flags: got %v, want %v", got, want)
	}
	// Shared flags keep sibling's Required semantics so MeegleValidateStep
	// can fail-fast before the fan-out. The IDs flags are never required
	// (mutual-exclusion validated by collectBatchIDs).
	getSibling := findChild(wi, "get")
	for _, f := range batch.Flags {
		if f.Name == "work-item-ids" || f.Name == "ids-file" {
			if f.Required {
				t.Errorf("IDs flag --%s should not be marked required", f.Name)
			}
			continue
		}
		// Look up the same flag in sibling to compare.
		for _, sf := range getSibling.Flags {
			if sf.Name == f.Name && sf.Required != f.Required {
				t.Errorf("flag --%s: Required=%v, want %v (inherited from sibling)", f.Name, f.Required, sf.Required)
			}
		}
	}
	// Param type metadata copied from sibling for value coercion.
	if batch.Meta.Tags["mcp_param_types"] == "" {
		t.Error("expected mcp_param_types to be copied from sibling")
	}
}

func TestInjectBatchCommands_NoParentGroupIsNoop(t *testing.T) {
	nodes := []*registry.CommandNode{{Name: "view"}}
	injectBatchCommands(nodes)
	if len(nodes[0].Children) != 0 {
		t.Errorf("expected view group untouched, got %d children", len(nodes[0].Children))
	}
}

func TestInjectBatchCommands_NoSiblingIsNoop(t *testing.T) {
	nodes := []*registry.CommandNode{{Name: "workitem"}}
	injectBatchCommands(nodes)
	if len(nodes[0].Children) != 0 {
		t.Errorf("expected workitem untouched when sibling 'get' is missing, got %d children", len(nodes[0].Children))
	}
}

func TestInjectBatchCommands_DoesNotOverwriteExistingChild(t *testing.T) {
	existing := &registry.CommandNode{Name: "+batch-get", HandlerRef: "keep_me"}
	nodes := []*registry.CommandNode{
		{Name: "workitem", Children: []*registry.CommandNode{existing, siblingGetNode()}},
	}
	injectBatchCommands(nodes)
	if len(nodes[0].Children) != 2 {
		t.Fatalf("expected 2 children (unchanged), got %d", len(nodes[0].Children))
	}
	batch := findChild(nodes[0], "+batch-get")
	if batch.HandlerRef != "keep_me" {
		t.Errorf("pre-existing child was overwritten, HandlerRef=%q", batch.HandlerRef)
	}
}

// ---------------------------------------------------------------------------
// flattenStringSliceFlag / splitCSV
// ---------------------------------------------------------------------------

func TestFlattenStringSliceFlag(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"nil", nil, nil},
		{"single csv string", "1,2,3", []string{"1", "2", "3"}},
		{"string slice single csv", []string{"1,2,3"}, []string{"1", "2", "3"}},
		{"string slice multi", []string{"1", "2", "3"}, []string{"1", "2", "3"}},
		{"mixed repeated+csv", []string{"1,2", "3"}, []string{"1", "2", "3"}},
		{"any slice", []any{"1", "2,3"}, []string{"1", "2", "3"}},
		{"trim whitespace", []string{" 1 , 2 "}, []string{"1", "2"}},
		{"empty entries skipped", []string{",1,,2,"}, []string{"1", "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenStringSliceFlag(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseIDsFromFile
// ---------------------------------------------------------------------------

func TestParseIDsFromFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"newline separated", "1\n2\n3\n", []string{"1", "2", "3"}},
		{"comma separated", "1,2,3", []string{"1", "2", "3"}},
		{"mixed", "1\n2,3\n\n4", []string{"1", "2", "3", "4"}},
		{"with hash comments", "1\n# comment line\n2 # trailing\n3", []string{"1", "2", "3"}},
		{"utf8 bom stripped", "\ufeff1\n2", []string{"1", "2"}},
		{"whitespace tolerated", "  1  \n\t2\t\n", []string{"1", "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, tc.content)
			got, err := parseIDsFromFile(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseIDsFromFile_MissingFile(t *testing.T) {
	_, err := parseIDsFromFile(filepath.Join(t.TempDir(), "nope.txt"))
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "CLIENT_INVALID_PARAM" {
		t.Fatalf("expected CLIENT_INVALID_PARAM, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// collectBatchIDs
// ---------------------------------------------------------------------------

func TestCollectBatchIDs_FromFlag(t *testing.T) {
	state := batchState(map[string]any{"work-item-ids": []string{"10", "20,30"}})
	got, err := collectBatchIDs(state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []int64{10, 20, 30}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectBatchIDs_FromFile(t *testing.T) {
	path := writeTempFile(t, "100\n200,300\n")
	state := batchState(map[string]any{"ids-file": path})
	got, err := collectBatchIDs(state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !reflect.DeepEqual(got, []int64{100, 200, 300}) {
		t.Errorf("got %v, want [100 200 300]", got)
	}
}

func TestCollectBatchIDs_MergeAndDedupePreservesOrder(t *testing.T) {
	path := writeTempFile(t, "20\n40\n")
	state := batchState(map[string]any{
		"work-item-ids": []string{"10", "20"},
		"ids-file":      path,
	})
	got, err := collectBatchIDs(state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []int64{10, 20, 40}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (flag comes first, dupes dropped)", got, want)
	}
}

func TestCollectBatchIDs_MissingIsClientError(t *testing.T) {
	state := batchState(map[string]any{})
	_, err := collectBatchIDs(state)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "CLIENT_MISSING_REQUIRED" {
		t.Fatalf("expected CLIENT_MISSING_REQUIRED, got %v", err)
	}
}

func TestCollectBatchIDs_NonNumericIsClientError(t *testing.T) {
	state := batchState(map[string]any{"work-item-ids": []string{"abc"}})
	_, err := collectBatchIDs(state)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "CLIENT_INVALID_PARAM" {
		t.Fatalf("expected CLIENT_INVALID_PARAM, got %v", err)
	}
}

func TestCollectBatchIDs_OverCapIsClientError(t *testing.T) {
	var raw []string
	for i := 0; i < maxBatchIDs+1; i++ {
		raw = append(raw, fmt.Sprintf("%d", i+1))
	}
	state := batchState(map[string]any{"work-item-ids": raw})
	_, err := collectBatchIDs(state)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "CLIENT_INVALID_PARAM" {
		t.Fatalf("expected CLIENT_INVALID_PARAM for over-cap, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// BatchExecutorStep.Execute
// ---------------------------------------------------------------------------

// fakeCaller satisfies mcpToolCaller in-process.
type fakeCaller struct {
	mu       sync.Mutex
	handler  func(params map[string]any) (*mcpResponse, error)
	calls    []map[string]any
	inflight atomic.Int32
	peak     atomic.Int32
}

func (f *fakeCaller) CallTool(ctx context.Context, name string, params map[string]any) (*mcpResponse, error) {
	f.inflight.Add(1)
	if cur := f.inflight.Load(); cur > f.peak.Load() {
		f.peak.Store(cur)
	}
	defer f.inflight.Add(-1)

	f.mu.Lock()
	cp := make(map[string]any, len(params))
	for k, v := range params {
		cp[k] = v
	}
	f.calls = append(f.calls, cp)
	f.mu.Unlock()

	return f.handler(params)
}

func TestBatchExecutorStep_AllSuccess(t *testing.T) {
	state := batchState(map[string]any{
		"work-item-ids": []string{"1,2,3"},
	})
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		id := int64(params["work_item_id"].(float64))
		return &mcpResponse{Data: map[string]any{"id": id, "name": fmt.Sprintf("WI-%d", id)}}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	resp := state.Result.Data.(map[string]any)
	results := resp["results"].([]map[string]any)
	errs := resp["errors"].([]map[string]any)
	summary := resp["summary"].(map[string]any)

	if len(results) != 3 || len(errs) != 0 {
		t.Errorf("expected 3 results / 0 errors, got %d / %d", len(results), len(errs))
	}
	if summary["succeeded"] != 3 || summary["failed"] != 0 || summary["total"] != 3 {
		t.Errorf("unexpected summary: %v", summary)
	}
	// Order must match input (after dedupe): 1, 2, 3
	for i, want := range []int64{1, 2, 3} {
		got, _ := results[i]["work_item_id"].(int64)
		if got != want {
			t.Errorf("result[%d]: got id=%d, want %d", i, got, want)
		}
	}
}

func TestBatchExecutorStep_PartialFailure(t *testing.T) {
	state := batchState(map[string]any{
		"work-item-ids": []string{"1,2,3"},
	})
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		id := int64(params["work_item_id"].(float64))
		if id == 2 {
			return nil, meerrors.NewServerError("NOT_FOUND", "work item not found")
		}
		return &mcpResponse{Data: map[string]any{"id": id}}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	resp := state.Result.Data.(map[string]any)
	results := resp["results"].([]map[string]any)
	errs := resp["errors"].([]map[string]any)
	summary := resp["summary"].(map[string]any)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0]["work_item_id"].(int64) != 2 || errs[0]["code"] != "NOT_FOUND" {
		t.Errorf("unexpected error entry: %v", errs[0])
	}
	if summary["succeeded"] != 2 || summary["failed"] != 1 {
		t.Errorf("unexpected summary: %v", summary)
	}
}

func TestBatchExecutorStep_AllFail_ReturnsPayloadNotError(t *testing.T) {
	state := batchState(map[string]any{"work-item-ids": []string{"1,2"}})
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		return nil, meerrors.NewServerError("BOOM", "server exploded")
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("non-auth total failure should not return err from Execute (got %v)", err)
	}
	resp := state.Result.Data.(map[string]any)
	if len(resp["errors"].([]map[string]any)) != 2 {
		t.Errorf("expected 2 per-item errors, got %v", resp["errors"])
	}
	if len(resp["results"].([]map[string]any)) != 0 {
		t.Errorf("expected 0 results, got %v", resp["results"])
	}
}

func TestBatchExecutorStep_AuthErrorIsGlobalTerminate(t *testing.T) {
	state := batchState(map[string]any{"work-item-ids": []string{"1,2,3"}})
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		id := int64(params["work_item_id"].(float64))
		if id == 2 {
			return nil, meerrors.NewClientError("AUTH_EXPIRED", "authentication expired, please log in again")
		}
		return &mcpResponse{Data: map[string]any{"id": id}}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	err := step.Execute(context.Background(), state)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "AUTH_EXPIRED" {
		t.Fatalf("expected AUTH_EXPIRED to be returned, got %v", err)
	}
	if state.Result != nil {
		t.Errorf("Result should not be set on auth failure, got %+v", state.Result)
	}
}

func TestBatchExecutorStep_ConcurrencyCapped(t *testing.T) {
	ids := []string{"1,2,3,4,5,6,7,8,9,10"}
	state := batchState(map[string]any{"work-item-ids": ids})
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		// Hold briefly so concurrency can be observed.
		time.Sleep(25 * time.Millisecond)
		return &mcpResponse{Data: params["work_item_id"]}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if peak := caller.peak.Load(); peak > int32(batchConcurrency) {
		t.Errorf("peak concurrency %d exceeded cap %d", peak, batchConcurrency)
	}
}

// TestValidateStep_FailsFastOnMissingRequiredSharedParam verifies that when
// a shared flag is Required in the sibling (and therefore in the batch node),
// MeegleValidateStep catches it before the fan-out runs — one error, not N.
func TestValidateStep_FailsFastOnMissingRequiredSharedParam(t *testing.T) {
	// Build a sibling where project-key is required.
	sibling := &registry.CommandNode{
		Name: "get", Help: registry.HelpText{Brief: "get"},
		HandlerRef: "tool_get",
		Flags: []registry.FlagDef{
			{Name: "work-item-id", Type: registry.FlagTypeString, Required: true},
			{Name: "project-key", Type: registry.FlagTypeString, Required: true},
		},
	}
	nodes := []*registry.CommandNode{
		{Name: "workitem", Help: registry.HelpText{Brief: "workitem group"}, Children: []*registry.CommandNode{sibling}},
	}
	injectBatchCommands(nodes)
	reg, err := registry.New(&registry.CommandTree{Version: "test", Nodes: nodes})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	node, _ := reg.Resolve([]string{"workitem", "+batch-get"})

	// User provides IDs but omits the required --project-key.
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:          node,
			FullPath:      node.FullPath(),
			Flags:         map[string]any{"work-item-ids": []string{"1"}},
			ExplicitFlags: map[string]any{"work-item-ids": []string{"1"}},
		},
		OutputConfig: map[string]any{},
	}

	step := &MeegleValidateStep{}
	err = step.Execute(context.Background(), state)
	var me *meerrors.MeegleError
	if !errors.As(err, &me) || me.Code != "CLIENT_MISSING_REQUIRED" {
		t.Fatalf("expected CLIENT_MISSING_REQUIRED for --project-key, got %v", err)
	}
	if !strings.Contains(me.Message, "project-key") {
		t.Errorf("error should mention the missing flag, got: %s", me.Message)
	}
}

// siblingWithFixedParams simulates a sugar/discovered command that carries
// mcp_fixed_params — used to verify batch inherits and injects them.
func siblingWithFixedParams() *registry.CommandNode {
	return &registry.CommandNode{
		Name: "get", Help: registry.HelpText{Brief: "get"},
		HandlerRef: "tool_with_fixed",
		Flags: []registry.FlagDef{
			{Name: "work-item-id", Type: registry.FlagTypeString, Required: true},
			{Name: "project-key", Type: registry.FlagTypeString},
		},
		Meta: registry.NodeMeta{Tags: map[string]string{
			"mcp_param_types":  `{"work-item-id":"number","project-key":"string"}`,
			"mcp_fixed_params": `{"internal_flag":"always_on","project_key":"sibling_default"}`,
		}},
	}
}

// TestInjectBatchCommands_InheritsFixedParams verifies that mcp_fixed_params
// on the sibling is copied to the batch node's Meta.Tags. Without this the
// batch would silently skip fixed params that the single-command path sends.
func TestInjectBatchCommands_InheritsFixedParams(t *testing.T) {
	nodes := []*registry.CommandNode{
		{Name: "workitem", Help: registry.HelpText{Brief: "wi"}, Children: []*registry.CommandNode{siblingWithFixedParams()}},
	}
	injectBatchCommands(nodes)
	batch := findChild(nodes[0], "+batch-get")
	if batch == nil {
		t.Fatal("+batch-get not injected")
	}
	if raw := batch.Meta.Tags["mcp_fixed_params"]; raw == "" {
		t.Error("expected mcp_fixed_params to be inherited from sibling, got empty")
	}
}

// TestBatchExecutorStep_ForwardsFixedParamsUserWins asserts the runtime
// contract: fixed params flow through to every per-item call, and a
// user-explicit flag with the same name overrides the fixed default
// (mirroring McpExecutorStep's precedence).
func TestBatchExecutorStep_ForwardsFixedParamsUserWins(t *testing.T) {
	nodes := []*registry.CommandNode{
		{Name: "workitem", Help: registry.HelpText{Brief: "wi"}, Children: []*registry.CommandNode{siblingWithFixedParams()}},
	}
	injectBatchCommands(nodes)
	reg, err := registry.New(&registry.CommandTree{Version: "test", Nodes: nodes})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	node, _ := reg.Resolve([]string{"workitem", "+batch-get"})

	// Case 1: user omits project-key. Fixed param's "sibling_default" fills in.
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:          node,
			FullPath:      node.FullPath(),
			Flags:         map[string]any{"work-item-ids": []string{"1"}},
			ExplicitFlags: map[string]any{"work-item-ids": []string{"1"}},
		},
		OutputConfig: map[string]any{},
	}
	var captured map[string]any
	caller := &fakeCaller{handler: func(p map[string]any) (*mcpResponse, error) {
		captured = p
		return &mcpResponse{Data: nil}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if captured["internal_flag"] != "always_on" {
		t.Errorf("expected fixed param internal_flag forwarded, got keys: %v", keysOf(captured))
	}
	if captured["project_key"] != "sibling_default" {
		t.Errorf("expected fixed param project_key=sibling_default (user omitted), got %v", captured["project_key"])
	}

	// Case 2: user explicitly sets --project-key — must win over the fixed default.
	stateOverride := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:          node,
			FullPath:      node.FullPath(),
			Flags:         map[string]any{"work-item-ids": []string{"1"}, "project-key": "user_chosen"},
			ExplicitFlags: map[string]any{"work-item-ids": []string{"1"}, "project-key": "user_chosen"},
		},
		OutputConfig: map[string]any{},
	}
	captured = nil
	if err := step.Execute(context.Background(), stateOverride); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if captured["project_key"] != "user_chosen" {
		t.Errorf("expected user-explicit project_key to override fixed default, got %v", captured["project_key"])
	}
	if captured["internal_flag"] != "always_on" {
		t.Errorf("user override should not drop other fixed params, internal_flag=%v", captured["internal_flag"])
	}
}

// TestBatchExecutorStep_ParentCtxCancelReturnsCtxErr plugs a correctness
// hole: if the parent context is cancelled while workers are mid-fan-out,
// some results[idx] slots stay zero-valued. Without the ctx.Err() check
// we would assemble a response with {work_item_id: 0} rows and claim
// success. Instead, Execute must surface the cancellation.
func TestBatchExecutorStep_ParentCtxCancelReturnsCtxErr(t *testing.T) {
	state := batchState(map[string]any{"work-item-ids": []string{"1,2,3,4,5,6,7,8"}})

	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		t.Fatal("handler should not be called when ctx is cancelled before Execute")
		return nil, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // parent cancelled before any worker can run

	err := step.Execute(ctx, state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if state.Result != nil {
		t.Errorf("expected Result to remain nil on ctx cancel, got %+v", state.Result)
	}
}

func TestBatchExecutorStep_NonBatchNodeIsNoop(t *testing.T) {
	// Node without the TagMcpBatch tag should be skipped entirely.
	node := &registry.CommandNode{Name: "get-brief"}
	state := &pipeline.PipelineContext{Parsed: &router.ParsedCommand{Node: node, Flags: map[string]any{}, ExplicitFlags: map[string]any{}}}
	step := &BatchExecutorStep{}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if state.Result != nil {
		t.Errorf("expected Result to remain nil, got %+v", state.Result)
	}
}

// TestBatchExecutorStep_PerItemCoercedToString asserts that when the MCP
// schema declares the per-item param as "string", the fan-out sends a
// string (not a number). This mirrors the real server contract which
// rejects `{"work_item_id": 7110160001}` with "not single string value"
// but accepts `{"work_item_id": "7110160001"}`.
func TestBatchExecutorStep_PerItemCoercedToString(t *testing.T) {
	// Build a sibling whose work-item-id MCP type is "string".
	sibling := &registry.CommandNode{
		Name: "get", Help: registry.HelpText{Brief: "get"},
		HandlerRef: "tool_get_string",
		Flags: []registry.FlagDef{
			{Name: "work-item-id", Type: registry.FlagTypeString, Required: true},
		},
		Meta: registry.NodeMeta{Tags: map[string]string{
			"mcp_param_types": `{"work-item-id":"string"}`,
		}},
	}
	nodes := []*registry.CommandNode{
		{Name: "workitem", Help: registry.HelpText{Brief: "workitem"}, Children: []*registry.CommandNode{sibling}},
	}
	injectBatchCommands(nodes)
	reg, err := registry.New(&registry.CommandTree{Version: "test", Nodes: nodes})
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	node, _ := reg.Resolve([]string{"workitem", "+batch-get"})
	state := &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:          node,
			FullPath:      node.FullPath(),
			Flags:         map[string]any{"work-item-ids": []string{"7110160001"}},
			ExplicitFlags: map[string]any{"work-item-ids": []string{"7110160001"}},
		},
		OutputConfig: map[string]any{},
	}

	var captured map[string]any
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		captured = params
		return &mcpResponse{Data: nil}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}
	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got, ok := captured["work_item_id"].(string)
	if !ok {
		t.Fatalf("expected work_item_id to be a string, got %T=%v", captured["work_item_id"], captured["work_item_id"])
	}
	if got != "7110160001" {
		t.Errorf("expected work_item_id=\"7110160001\", got %q", got)
	}
}

func TestBatchExecutorStep_SharedParamsForwarded(t *testing.T) {
	state := batchState(map[string]any{
		"work-item-ids": []string{"1"},
		"fields":        []string{"title,status"},
		"project-key":   "my_project",
	})
	// Only the shared flags are ExplicitFlags; the IDs flag is CLI-only.
	state.Parsed.ExplicitFlags = map[string]any{
		"work-item-ids": state.Parsed.Flags["work-item-ids"],
		"fields":        []string{"title,status"},
		"project-key":   "my_project",
	}

	var captured map[string]any
	caller := &fakeCaller{handler: func(params map[string]any) (*mcpResponse, error) {
		captured = params
		return &mcpResponse{Data: nil}, nil
	}}
	step := &BatchExecutorStep{clientFactory: func(*pipeline.PipelineContext) mcpToolCaller { return caller }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Sibling flags are snake-cased before being sent to MCP.
	if _, ok := captured["fields"]; !ok {
		t.Errorf("expected 'fields' forwarded, got keys: %v", keysOf(captured))
	}
	if captured["project_key"] != "my_project" {
		t.Errorf("expected project_key='my_project', got %v (keys: %v)", captured["project_key"], keysOf(captured))
	}
	// Sibling fixture declares work-item-id as MCP type "number" — so the
	// per-item value goes through coerceValue and lands as float64.
	if v, ok := captured["work_item_id"].(float64); !ok || v != 1 {
		t.Errorf("expected per-item work_item_id=1 (float64), got %v (%T)", captured["work_item_id"], captured["work_item_id"])
	}
	// CLI-only flags and the kebab-cased per-item flag must not be sent.
	for _, leak := range []string{"work-item-ids", "ids-file", "work-item-id", "project-key"} {
		if _, found := captured[leak]; found {
			t.Errorf("%q leaked into MCP params: %v", leak, captured)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// batchState builds a PipelineContext whose Parsed.Node is a freshly-
// registered workitem/+batch-get (with a realistic sibling `get` providing
// flags + HandlerRef) so that FullPath() returns [workitem, +batch-get].
func batchState(flags map[string]any) *pipeline.PipelineContext {
	nodes := []*registry.CommandNode{
		{Name: "workitem", Help: registry.HelpText{Brief: "workitem group"}, Children: []*registry.CommandNode{
			siblingGetNode(),
		}},
	}
	injectBatchCommands(nodes)
	reg, err := registry.New(&registry.CommandTree{Version: "test", Nodes: nodes})
	if err != nil {
		panic(err)
	}
	node, _ := reg.Resolve([]string{"workitem", "+batch-get"})
	if node == nil {
		panic("+batch-get node not found after inject")
	}
	explicit := map[string]any{}
	for k, v := range flags {
		explicit[k] = v
	}
	return &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:          node,
			FullPath:      node.FullPath(),
			Flags:         flags,
			ExplicitFlags: explicit,
		},
		OutputConfig: map[string]any{},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ids.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func flagNames(defs []registry.FlagDef) []string {
	out := make([]string, 0, len(defs))
	for _, f := range defs {
		out = append(out, f.Name)
	}
	return out
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Regression guard: if we ever double-inject the same batch command,
// hasChildNamed should treat it as idempotent.
func TestInjectBatchCommands_Idempotent(t *testing.T) {
	nodes := []*registry.CommandNode{
		{Name: "workitem", Children: []*registry.CommandNode{siblingGetNode()}},
	}
	injectBatchCommands(nodes)
	injectBatchCommands(nodes)
	// sibling(get) + +batch-get -> 2 children; +batch-get must not be doubled.
	if got := len(nodes[0].Children); got != 2 {
		t.Errorf("expected 2 children after double inject (get + +batch-get), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// reshapeBatchForFormat
// ---------------------------------------------------------------------------

func sampleBatchPayload() map[string]any {
	return map[string]any{
		"results": []map[string]any{
			{"work_item_id": int64(1), "data": map[string]any{"name": "A", "status": "doing"}},
			{"work_item_id": int64(2), "data": map[string]any{"name": "B", "status": "done"}},
		},
		"errors": []map[string]any{
			{"work_item_id": int64(3), "code": "NOT_FOUND", "message": "gone"},
		},
		"summary": map[string]any{"total": 3, "succeeded": 2, "failed": 1},
	}
}

func TestReshapeBatchForNDJSON_FlatArrayWithSummaryLast(t *testing.T) {
	out, ok := reshapeBatchForNDJSON(sampleBatchPayload())
	if !ok {
		t.Fatal("expected reshape to recognise batch payload")
	}
	arr, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	// 2 successes + 1 error + 1 summary = 4 entries
	if len(arr) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(arr))
	}
	// First two are successes
	first := arr[0].(map[string]any)
	if _, ok := first["data"]; !ok {
		t.Errorf("expected success entry to keep 'data', got %v", first)
	}
	// Third is the error
	errRow := arr[2].(map[string]any)
	if errRow["code"] != "NOT_FOUND" {
		t.Errorf("expected error row code=NOT_FOUND, got %v", errRow)
	}
	// Last is summary wrapper
	last := arr[3].(map[string]any)
	if _, ok := last["summary"]; !ok {
		t.Errorf("expected last entry to be {summary: ...}, got %v", last)
	}
}

func TestReshapeBatchForNDJSON_NonBatchPayload_Passthrough(t *testing.T) {
	// Single get output: plain map without results/errors shape.
	data := map[string]any{"id": 123, "name": "solo"}
	if _, ok := reshapeBatchForNDJSON(data); ok {
		t.Error("expected non-batch map to be left alone")
	}
	// Slice payload should also pass through.
	if _, ok := reshapeBatchForNDJSON([]any{1, 2}); ok {
		t.Error("expected slice payload to pass through")
	}
}

// TestParseIDsFromFile_HashAtLineStart ensures a '#'-only line is ignored
// cleanly (regression against premature tokenization before comment-strip).
func TestParseIDsFromFile_HashAtLineStart(t *testing.T) {
	path := writeTempFile(t, "# only a comment\n\n42")
	got, err := parseIDsFromFile(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0] != "42" {
		t.Errorf("got %v, want [42]", got)
	}
}
