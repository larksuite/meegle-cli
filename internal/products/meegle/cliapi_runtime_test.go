// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

type fakeHandoffAPI struct {
	createResponse     *cliapiclient.CreateLinkResponse
	preference         *cliapiclient.PreferenceUpdateResult
	err                error
	createRequest      cliapiclient.CreateLinkRequest
	updateMode         string
	createCalls        int
	preferenceSetCalls int
}

type fakeRuntimeCLIConfigSource struct {
	config *cliapiclient.CLIConfig
	err    error
	calls  int
}

func (source *fakeRuntimeCLIConfigSource) Config(context.Context) (*cliapiclient.CLIConfig, error) {
	source.calls++
	return source.config, source.err
}

func (api *fakeHandoffAPI) CreateLink(_ context.Context, request cliapiclient.CreateLinkRequest) (*cliapiclient.CreateLinkResponse, error) {
	api.createCalls++
	api.createRequest = request
	return api.createResponse, api.err
}

func (api *fakeHandoffAPI) UpdatePreference(_ context.Context, mode string) (*cliapiclient.PreferenceUpdateResult, error) {
	api.preferenceSetCalls++
	api.updateMode = mode
	return api.preference, api.err
}

// fakeCLIConfigCache is an in-memory CLIConfigStore for tests, so commands
// never touch the real ~/.meegle/cache directory.
type fakeCLIConfigCache struct {
	stored     *cliapiclient.CLIConfig
	stale      bool
	getCalls   int
	setCalls   int
	clearCalls int
	setLast    *cliapiclient.CLIConfig
}

func (c *fakeCLIConfigCache) Get() (*CLIConfigCacheResult, error) {
	c.getCalls++
	if c.stored == nil {
		return nil, nil
	}
	return &CLIConfigCacheResult{Config: c.stored, Stale: c.stale}, nil
}

func (c *fakeCLIConfigCache) Set(a *cliapiclient.CLIConfig) error {
	c.setCalls++
	c.setLast = a
	c.stored = a
	c.stale = false
	return nil
}

func (c *fakeCLIConfigCache) Clear() error {
	c.clearCalls++
	c.stored = nil
	return nil
}

func TestCLIAPIRuntimeAvailabilityReturnsStructuredUnavailableResult(t *testing.T) {
	availability := &cliapiclient.Availability{
		Available: false, RejectCode: "USER_DISABLED", RejectMsg: "AI handoff recommendations are disabled",
	}
	source := &fakeRuntimeCLIConfigSource{config: &cliapiclient.CLIConfig{HandoffSuggestion: *availability}}
	cache := &fakeCLIConfigCache{}
	state := localPipelineState(t, "ai-handoff availability", nil)
	step := &CLIAPIRuntime{
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(source, cache)
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got, _ := state.Result.Data.(*cliapiclient.Availability)
	if state.Result == nil || got == nil || *got != *availability {
		t.Fatalf("Result = %#v", state.Result)
	}
	if source.calls != 1 {
		t.Fatalf("config source calls = %d", source.calls)
	}
	// A successful business decision is cached for the next call.
	if cache.setCalls != 1 || cache.setLast == nil || cache.setLast.HandoffSuggestion != *availability {
		t.Fatalf("expected decision cached, setCalls = %d", cache.setCalls)
	}
}

func TestCLIAPIRuntimeAvailabilityServesFreshCacheWithoutAPICall(t *testing.T) {
	cached := &cliapiclient.Availability{Available: true}
	source := &fakeRuntimeCLIConfigSource{config: &cliapiclient.CLIConfig{
		HandoffSuggestion: cliapiclient.Availability{Available: false},
	}}
	cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{HandoffSuggestion: *cached}, stale: false}
	state := localPipelineState(t, "ai-handoff availability", nil)
	step := &CLIAPIRuntime{
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(source, cache)
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if source.calls != 0 {
		t.Fatalf("fresh cache should skip the config source, calls = %d", source.calls)
	}
	got, _ := state.Result.Data.(*cliapiclient.Availability)
	if state.Result == nil || got == nil || *got != *cached {
		t.Fatalf("expected cached decision served, Result = %#v", state.Result)
	}
}

func TestCLIAPIRuntimeAvailabilityStaleCacheRefetches(t *testing.T) {
	fresh := &cliapiclient.Availability{Available: true}
	source := &fakeRuntimeCLIConfigSource{config: &cliapiclient.CLIConfig{HandoffSuggestion: *fresh}}
	cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: false}}, stale: true}
	state := localPipelineState(t, "ai-handoff availability", nil)
	step := &CLIAPIRuntime{
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(source, cache)
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if source.calls != 1 {
		t.Fatalf("stale cache should refetch, config source calls = %d", source.calls)
	}
	got, _ := state.Result.Data.(*cliapiclient.Availability)
	if state.Result == nil || got == nil || *got != *fresh {
		t.Fatalf("expected fresh decision served, Result = %#v", state.Result)
	}
	if cache.setLast == nil || cache.setLast.HandoffSuggestion != *fresh {
		t.Fatalf("expected refreshed decision cached, setLast = %#v", cache.setLast)
	}
}

func TestCLIAPIRuntimeLocalDisableShortCircuits(t *testing.T) {
	t.Run("availability", func(t *testing.T) {
		source := &fakeRuntimeCLIConfigSource{config: &cliapiclient.CLIConfig{
			HandoffSuggestion: cliapiclient.Availability{Available: true},
		}}
		cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{
			HandoffSuggestion: cliapiclient.Availability{Available: true},
		}}
		state := localPipelineState(t, "ai-handoff availability", nil)
		t.Setenv("MEEGLE_AI_HANDOFF", "disabled")
		step := &CLIAPIRuntime{
			ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
				return NewCLIConfigProvider(source, cache)
			},
		}

		if err := step.Execute(context.Background(), state); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got, _ := state.Result.Data.(*cliapiclient.Availability)
		if got == nil || got.Available || got.RejectCode != "LOCAL_DISABLED" || got.RejectMsg == "" {
			t.Fatalf("Result = %#v, want local-disabled availability", state.Result)
		}
		if source.calls != 0 || cache.getCalls != 0 {
			t.Fatalf("local disable must bypass cache and config source, cache gets = %d, source calls = %d",
				cache.getCalls, source.calls)
		}
	})

	t.Run("create-link", func(t *testing.T) {
		api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
			Available: true, URL: "https://example.com/agent?ctx_token=opaque",
		}}
		state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
		t.Setenv("MEEGLE_AI_HANDOFF", "disabled")
		step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

		if err := step.Execute(context.Background(), state); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got, _ := state.Result.Data.(*cliapiclient.CreateLinkResponse)
		if got == nil || got.Available || got.RejectCode != "LOCAL_DISABLED" || got.RejectMsg == "" {
			t.Fatalf("Result = %#v, want local-disabled create-link response", state.Result)
		}
		if api.createCalls != 0 {
			t.Fatalf("local disable must bypass API, create calls = %d", api.createCalls)
		}
	})
}

func TestCLIAPIRuntimeCreateLinkParsesTypedRelatedContext(t *testing.T) {
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: true, URL: "https://example.com/agent?ctx_token=opaque", LogID: "trace-create",
	}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{
		"query": "summarize",
		"related-context": []string{
			`{"type":3,"work_item":{"work_item_id":"42","project_key":"p","work_item_type_key":"story"}}`,
		},
	})
	state.OutputConfig = map[string]any{"session.host": "meegle.com"}
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if api.createCalls != 1 || len(api.createRequest.RelatedContext) != 1 {
		t.Fatalf("request = %#v", api.createRequest)
	}
	if state.Result.Metadata["logid"] != "trace-create" {
		t.Fatalf("result metadata = %#v, want success logid", state.Result.Metadata)
	}
	contextItem := api.createRequest.RelatedContext[0]
	if contextItem.Type != cliapiclient.ContextTypeWorkItem || contextItem.WorkItem == nil || contextItem.WorkItem.WorkItemID != "42" {
		t.Fatalf("related context = %#v", contextItem)
	}
}

func TestCLIAPIRuntimeCreateLinkUsesActiveLoginHost(t *testing.T) {
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: true,
		URL:       "https://handoff.example.com/agent/session?ctx_token=a%2Fb&source=cli#result",
	}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
	state.OutputConfig = map[string]any{"session.host": "project.feishu.cn:8443"}
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got, _ := state.Result.Data.(*cliapiclient.CreateLinkResponse)
	const want = "https://project.feishu.cn:8443/agent/session?ctx_token=a%2Fb&source=cli#result"
	if got == nil || got.URL != want {
		t.Fatalf("URL = %q, want %q", got.URL, want)
	}
}

func TestCLIAPIRuntimeCreateLinkRejectsInvalidURL(t *testing.T) {
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: true,
		URL:       "not-an-absolute-url",
	}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
	state.OutputConfig = map[string]any{"session.host": "meegle.com"}
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	err := step.Execute(context.Background(), state)
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) || meegleErr.Code != "HANDOFF_API_INVALID_RESPONSE" {
		t.Fatalf("Execute() error = %#v", err)
	}
}

func TestCLIAPIRuntimeCreateLinkRequiresActiveLoginHost(t *testing.T) {
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: true,
		URL:       "https://handoff.example.com/agent/session",
	}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	err := step.Execute(context.Background(), state)
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) || meegleErr.Code != "CLIENT_MISCONFIGURED" {
		t.Fatalf("Execute() error = %#v", err)
	}
}

func TestCLIAPIRuntimeCreateLinkRejectsUnavailableResponse(t *testing.T) {
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: false, RejectCode: "USER_DISABLED", RejectMsg: "disabled by preference",
	}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	err := step.Execute(context.Background(), state)
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) || meegleErr.Code != "HANDOFF_REJECTED" {
		t.Fatalf("Execute() error = %#v", err)
	}
	if meegleErr.Suggestion == "" {
		t.Fatal("USER_DISABLED error should include the enable command")
	}
}

func TestParseRelatedContextAcceptsEverySupportedTypedPayload(t *testing.T) {
	contexts, err := parseRelatedContext(`[
		{"type":1,"project":{"project_key":"p"}},
		{"type":3,"work_item":{"project_key":"p","work_item_type_key":"story","work_item_id":"42"}},
		{"type":4,"view":{"project_key":"p","view_id":"view-1"}},
		{"type":5,"measure_chart":{"project_key":"p","chart_id":"chart-1"}}
	]`)
	if err != nil {
		t.Fatalf("parseRelatedContext() error = %v", err)
	}
	if len(contexts) != 4 || contexts[2].View == nil || contexts[2].View.ViewID != "view-1" || contexts[2].View.WorkItemTypeKey != "" {
		t.Fatalf("related context = %#v", contexts)
	}
}

func TestParseRelatedContextRejectsReservedWorkItemType(t *testing.T) {
	_, err := parseRelatedContext(`{"type":2,"work_item_type":{"project_key":"p","work_item_type_key":"story"}}`)
	if err == nil {
		t.Fatal("parseRelatedContext() error = nil, want reserved WorkItemType rejection")
	}
}

func TestParseRelatedContextRejectsUnknownFields(t *testing.T) {
	_, err := parseRelatedContext(`{"type":1,"project":{"project_key":"p","key":"p"}}`)
	if err == nil {
		t.Fatal("parseRelatedContext() error = nil, want unknown-field rejection")
	}
}

func TestParseRelatedContextRejectsTrailingJSON(t *testing.T) {
	_, err := parseRelatedContext(`{"type":1,"project":{"project_key":"p"}}{"type":1,"project":{"project_key":"q"}}`)
	if err == nil {
		t.Fatal("parseRelatedContext() error = nil, want trailing-JSON rejection")
	}
}

func TestParseRelatedContextRejectsMismatchedPayloadAndMissingRequiredFields(t *testing.T) {
	tests := []string{
		`{"type":3,"view":{"view_id":"view-1","project_key":"p"}}`,
		`{"type":3,"work_item":{"work_item_id":"42","project_key":"p"}}`,
		`{"type":1,"project":{"project_key":"p"},"view":{"view_id":"v","project_key":"p"}}`,
	}
	for _, input := range tests {
		if _, err := parseRelatedContext(input); err == nil {
			t.Fatalf("parseRelatedContext(%s) error = nil", input)
		}
	}
}

func TestCLIAPIRuntimeAutoUsesDedicatedPreferenceAPI(t *testing.T) {
	api := &fakeHandoffAPI{preference: &cliapiclient.PreferenceUpdateResult{Success: true, LogID: "trace-preference"}}
	cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: false}}}
	state := localPipelineState(t, "preference handoff auto", nil)
	step := &CLIAPIRuntime{
		ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api },
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(nil, cache)
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if api.preferenceSetCalls != 1 || api.updateMode != "auto" {
		t.Fatalf("preference calls = %d, mode = %q", api.preferenceSetCalls, api.updateMode)
	}
	if state.Result.Metadata["logid"] != "trace-preference" {
		t.Fatalf("result metadata = %#v, want success logid", state.Result.Metadata)
	}
	// A successful write must invalidate the local CLI configuration cache.
	if cache.clearCalls != 1 {
		t.Fatalf("mode update should clear the CLI config cache, clearCalls = %d", cache.clearCalls)
	}
}

func TestCLIAPIRuntimeCreateLinkBypassesCache(t *testing.T) {
	// An available create-link is a write with mandatory server-side
	// re-validation (§3); it must never be served from — nor write to — the
	// CLI configuration cache. (A rejected create-link does invalidate the cache;
	// see TestCLIAPIRuntimeCreateLinkRejectionClearsCache.)
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: true, URL: "https://example.com/agent?ctx_token=opaque",
	}}
	cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: false}}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "q"})
	state.OutputConfig = map[string]any{"session.host": "meegle.com"}
	step := &CLIAPIRuntime{
		ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api },
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(nil, cache)
		},
	}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if api.createCalls != 1 {
		t.Fatalf("create-link should always hit the server, createCalls = %d", api.createCalls)
	}
	if cache.getCalls != 0 || cache.setCalls != 0 || cache.clearCalls != 0 {
		t.Fatalf("available create-link must not touch the cache, get=%d set=%d clear=%d",
			cache.getCalls, cache.setCalls, cache.clearCalls)
	}
}

func TestCLIAPIRuntimeCreateLinkRejectionClearsCache(t *testing.T) {
	// A server rejection is authoritative: any cached availability=true is now
	// stale (e.g. disabled on another device), so the entry must be dropped.
	api := &fakeHandoffAPI{createResponse: &cliapiclient.CreateLinkResponse{
		Available: false, RejectCode: "USER_DISABLED", RejectMsg: "disabled by preference",
	}}
	cache := &fakeCLIConfigCache{stored: &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "q"})
	step := &CLIAPIRuntime{
		ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api },
		ConfigProviderFactory: func(*pipeline.PipelineContext) *CLIConfigProvider {
			return NewCLIConfigProvider(nil, cache)
		},
	}

	err := step.Execute(context.Background(), state)
	var meegleErr *meerrors.MeegleError
	if !errors.As(err, &meegleErr) || meegleErr.Code != "HANDOFF_REJECTED" {
		t.Fatalf("Execute() error = %#v", err)
	}
	if cache.clearCalls != 1 {
		t.Fatalf("rejected create-link should clear the stale cache, clearCalls = %d", cache.clearCalls)
	}
}

func TestResolveCacheProfilePrefersResolvedSessionProfile(t *testing.T) {
	// SessionStep records the resolved profile; the cache must reuse it rather
	// than re-resolving (which could disagree, e.g. after a profile switch).
	state := localPipelineState(t, "ai-handoff availability", nil)
	state.Parsed.Flags["profile"] = "flag-profile"
	state.OutputConfig = map[string]any{"session.profile": "resolved-profile"}

	if got := resolveCacheProfile(state); got != "resolved-profile" {
		t.Fatalf("resolveCacheProfile() = %q, want resolved-profile", got)
	}
}

func TestResolveCacheProfileFallsBackToFlag(t *testing.T) {
	// SDK-injected paths bypass SessionStep, so session.profile is absent; the
	// explicit --profile flag is the next authority.
	state := localPipelineState(t, "ai-handoff availability", nil)
	state.Parsed.Flags["profile"] = "flag-profile"

	if got := resolveCacheProfile(state); got != "flag-profile" {
		t.Fatalf("resolveCacheProfile() = %q, want flag-profile", got)
	}
}

func TestCLIAPIRuntimeDryRunDoesNotCallAPI(t *testing.T) {
	api := &fakeHandoffAPI{}
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": "query"})
	state.Parsed.Flags["dry-run"] = true
	step := &CLIAPIRuntime{ClientFactory: func(*pipeline.PipelineContext) cliCommandAPI { return api }}

	if err := step.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if api.createCalls != 0 {
		t.Fatalf("create calls = %d", api.createCalls)
	}
}

func TestCommandRuntimeResolverRoutesLocalCommandsToCLIAPI(t *testing.T) {
	state := localPipelineState(t, "ai-handoff availability", nil)
	cliRuntime := &CLIAPIRuntime{}
	resolver := NewCommandRuntimeResolver(&MCPRuntime{}, cliRuntime)
	runtime, err := resolver.Resolve(state)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if runtime != cliRuntime {
		t.Fatalf("Resolve() runtime = %T, want CLIAPIRuntime", runtime)
	}
}

func localPipelineState(t *testing.T, path string, values map[string]any) *pipeline.PipelineContext {
	t.Helper()
	t.Setenv("MEEGLE_AI_HANDOFF", "")
	tree, err := NewMeegleLocalSetup().Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	reg, err := registry.New(tree)
	if err != nil {
		t.Fatalf("registry.New() error = %v", err)
	}
	node := reg.GetByPath(path)
	if node == nil {
		t.Fatalf("command %q not found", path)
	}
	flags := map[string]any{}
	for key, value := range values {
		flags[key] = value
	}
	return &pipeline.PipelineContext{
		Parsed: &router.ParsedCommand{
			Node:     node,
			FullPath: node.FullPath(),
			Flags:    flags,
		},
		Values: values,
	}
}
