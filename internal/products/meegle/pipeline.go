// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
	"github.com/larksuite/meegle-cli/pkg/integrations/formatting"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// ---------------------------------------------------------------------------
// SessionStep — Unified auth state resolution (merged from FirstRunStep + SessionAuthStep)
// Priority: pre-injected OutputConfig → local profile
// ---------------------------------------------------------------------------

type SessionStep struct{}

func (s *SessionStep) Name() string { return "session" }

func (s *SessionStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || len(state.Parsed.FullPath) == 0 {
		return nil
	}
	// --dry-run never touches the backend, so it must not require auth either:
	// the user should be able to preview the normalized payload offline.
	if isDryRunFlag(state.Parsed) {
		return nil
	}
	topCmd := state.Parsed.FullPath[0]
	skip := map[string]bool{
		"auth": true, "config": true, "help": true,
		"version": true, "inspect": true, "completion": true,
	}
	if skip[topCmd] {
		return nil
	}

	if state.OutputConfig == nil {
		state.OutputConfig = make(map[string]any)
	}

	// SDK has injected host/headers → skip local auth
	if injected, _ := state.OutputConfig["mcp.injected"].(bool); injected {
		return nil
	}

	// Local profile resolution
	profileName, _ := state.Parsed.Flags["profile"].(string)
	if profileName == "" {
		profileName, _ = GetCurrentProfileName()
	}
	cfg, err := LoadConfig(profileName)
	if err != nil {
		return err
	}

	ident, err := ResolveIdentity(cfg, profileName)
	if err != nil {
		return meerrors.NewClientError("CONFIG_ENV_UNRESOLVED", err.Error()).
			WithSuggestion("set the referenced environment variable, or run 'meegle config set' to update the profile")
	}

	if ident.Host == "" {
		if !isInteractiveTerminal() {
			return meerrors.NewClientError("HOST_NOT_CONFIGURED", "host not configured").
				WithSuggestion(meerrors.HintNonInteractiveSetup)
		}
		return CheckFirstRun(profileName)
	}

	if ident.Token == "" {
		if !isInteractiveTerminal() {
			return meerrors.NewClientError("AUTH_REQUIRED", "authentication required").
				WithSuggestion(meerrors.HintDeviceCodeRequired)
		}
		return CheckFirstRun(profileName)
	}

	state.OutputConfig["mcp.host"] = ident.Host
	state.OutputConfig["mcp.token"] = ident.Token
	state.OutputConfig["mcp.headers"] = ident.Headers
	state.OutputConfig["mcp.identity_source"] = ident.Source
	state.OutputConfig["mcp.access_token_header"] = ident.AccessTokenHeader
	if ident.UserAgentCaller != "" {
		state.OutputConfig["mcp.user_agent"] = mcpclient.BuildUserAgent(ident.UserAgentCaller)
	}
	// TokenManager / store are attached only when the token came from the
	// managed keychain — SourceEnv/SourceConfig have no local refresh path.
	if ident.Source == SourceStore {
		state.OutputConfig["mcp.store"] = ident.Store
		state.OutputConfig["mcp.token_manager"] = ident.TokenManager
	}
	return nil
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ---------------------------------------------------------------------------
// MeegleValidateStep — Validates required parameters
// ---------------------------------------------------------------------------

type MeegleValidateStep struct{}

func (s *MeegleValidateStep) Name() string { return "meegle_validate" }

func (s *MeegleValidateStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}
	for _, flag := range state.Parsed.Node.Flags {
		if flag.Required {
			if _, ok := state.Values[flag.Name]; !ok {
				return meerrors.NewClientError("CLIENT_MISSING_REQUIRED",
					fmt.Sprintf("missing required parameter: --%s", flag.Name)).
					WithSuggestion(fmt.Sprintf("meegle %s --help", state.Parsed.Node.FullPathString()))
			}
		}
	}
	// Required positional args (ArgDef.Required) — cobra defaults to
	// ArbitraryArgs which doesn't enforce arity, so commands using positional
	// args (e.g. attachment +upload-entire <source-path>) need this check
	// before the step layer opens files or hits the network.
	for index, arg := range state.Parsed.Node.Args {
		if !arg.Required {
			continue
		}
		if index >= len(state.Parsed.Args) {
			return meerrors.NewClientError("CLIENT_MISSING_REQUIRED",
				fmt.Sprintf("missing required argument <%s>", arg.Name)).
				WithSuggestion(fmt.Sprintf("meegle %s --help", state.Parsed.Node.FullPathString()))
		}
	}
	// Enum validation for persistent/inherited flags (e.g. --format).
	for _, flag := range state.Parsed.Node.InheritedFlags() {
		if len(flag.Enum) == 0 {
			continue
		}
		value, ok := state.Parsed.Flags[flag.Name]
		if !ok {
			continue
		}
		candidate := fmt.Sprint(value)
		matched := false
		for _, allowed := range flag.Enum {
			if candidate == allowed {
				matched = true
				break
			}
		}
		if !matched {
			return meerrors.NewClientError("CLIENT_INVALID_VALUE",
				fmt.Sprintf("invalid value %q for --%s; allowed: %s", candidate, flag.Name, strings.Join(flag.Enum, "|")))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// McpExecutorStep — Calls the MCP Server
// ---------------------------------------------------------------------------

type McpExecutorStep struct {
	// CommandsFunc returns the mapped commands for error message sanitization.
	// Injected by the pipeline factory to avoid package-level mutable state.
	CommandsFunc func() []types.MappedCommand
}

func (s *McpExecutorStep) Name() string { return "mcp_executor" }

func (s *McpExecutorStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	// Batch commands are handled by BatchExecutorStep.
	if state.Parsed.Node.Meta.Tags[TagMcpBatch] == "1" {
		return nil
	}
	// Attachment shortcut commands are handled by AttachmentShortcutStep.
	// They inherit their HandlerRef from the MCP-discovered prepare-* sibling
	// (see attachment_register.go::injectAttachmentCommands), so we gate on
	// TagAttachmentShortcut here to stop the shared HandlerRef from re-firing
	// the basic MCP path.
	if state.Parsed.Node.Meta.Tags[TagAttachmentShortcut] != "" {
		return nil
	}
	toolName := state.Parsed.Node.HandlerRef
	if toolName == "" {
		return nil // group node, no execution needed
	}
	if toolName == discoveryFailureHandlerRef {
		return discoveryFailureCommandError(state)
	}

	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}

	snakeParams := buildSnakeParams(state)

	unknownParams := findUnknownParams(state, snakeParams)

	// --dry-run: return the payload that would be sent to the backend without
	// performing the network call. This lets users preview the result of
	// --params/--set after type coercion, and works offline (no auth required
	// — SessionStep also short-circuits on dry-run).
	if isDryRunFlag(state.Parsed) {
		payload := map[string]any{
			"tool":    toolName,
			"params":  snakeParams,
			"dry_run": true,
		}
		if len(unknownParams) > 0 {
			payload["validation"] = map[string]any{"unknown_params": unknownParams}
		}
		state.Result = &executor.RawResult{Data: payload}
		return nil
	}

	if msg := formatUnknownParamsWarning(toolName, unknownParams); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	client := newMcpClientFromState(state)

	start := time.Now()
	resp, err := client.CallTool(ctx, toolName, snakeParams)
	duration := time.Since(start)
	if err != nil {
		if me, ok := err.(*meerrors.MeegleError); ok && s.CommandsFunc != nil {
			me.Message = sanitizeToolNames(me.Message, s.CommandsFunc())
		}
		attachMyWorkTodoSuggestion(err, state, toolName)
		return err
	}
	if resp != nil {
		data := resp.Data
		if state.Parsed != nil {
			if fn := LookupResultTransform(state.Parsed.FullPath); fn != nil {
				data = fn(data)
			}
		}
		metadata := map[string]any{
			"tool":        toolName,
			"command":     strings.Join(state.Parsed.FullPath, " "),
			"duration_ms": duration.Milliseconds(),
		}
		// Surface the backend logid to users via --envelope. The MCP server emits
		// it as a "log_id: <id>" content entry, which mcpclient strips into
		// Response.LogID (bare id, no prefix); without this hop into Metadata
		// the id is silently dropped and oncall has no way to trace a specific
		// call back to argos.
		if logid := extractLogID(resp.LogID); logid != "" {
			metadata["logid"] = logid
		}
		state.Result = &executor.RawResult{
			Data:     data,
			Metadata: metadata,
		}
	}
	return nil
}

// extractLogID returns the bare id for state.Metadata. mcpclient already
// strips the "log_id:" / "logid:" prefix into Response.LogID, but we re-check
// here as defense in depth — if a future code path forwards a raw-prefixed
// string, we still surface a clean id instead of leaking the prefix into
// --envelope's meta.
func extractLogID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for _, p := range []string{"log_id:", "logid:"} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			s = strings.TrimSpace(rest)
			break
		}
	}
	return s
}

func isMeegleRuntimeFlag(name string) bool {
	switch strings.ToLower(name) {
	case "profile", "refresh":
		return true
	default:
		return false
	}
}

const hintMyWorkTodoActionInfo = `The backend could not resolve My Work action context. Try:
  1. Refresh command metadata: meegle --refresh mywork todo --action this_week --page-num 1
  2. Confirm the active host/profile: meegle auth status
  3. If your account belongs to multiple workspaces, retry with the workspace key: meegle mywork todo --action this_week --page-num 1 --asset-key Asset_xxx`

func attachMyWorkTodoSuggestion(err error, state *pipeline.PipelineContext, toolName string) {
	me, ok := err.(*meerrors.MeegleError)
	if !ok || strings.TrimSpace(me.Suggestion) != "" {
		return
	}
	if toolName != "list_todo" {
		return
	}
	if state == nil || state.Parsed == nil || strings.Join(state.Parsed.FullPath, " ") != "mywork todo" {
		return
	}
	msg := strings.ToLower(me.Message)
	if !strings.Contains(msg, "get action info fail") {
		return
	}
	me.Suggestion = hintMyWorkTodoActionInfo
}

func discoveryFailureCommandError(state *pipeline.PipelineContext) error {
	command := ""
	if state != nil && state.Parsed != nil {
		parts := append([]string(nil), state.Parsed.FullPath...)
		for _, arg := range state.Parsed.Args {
			if strings.HasPrefix(arg, "-") {
				break
			}
			parts = append(parts, arg)
		}
		command = strings.TrimSpace(strings.Join(parts, " "))
	}
	if command == "" {
		command = "dynamic command"
	}

	detail := ""
	if state != nil && state.Parsed != nil && state.Parsed.Node != nil {
		detail = strings.TrimSpace(state.Parsed.Node.Meta.Tags[tagDiscoveryFailure])
	}
	message := fmt.Sprintf("tool discovery failed; dynamic Meegle command %q is unavailable", command)
	if detail != "" {
		message += ": " + detail
	}
	return meerrors.NewServerError("TOOL_DISCOVERY_FAILED", message).
		WithSuggestion("Check Meegle server connectivity, then retry with `meegle --refresh <command>`. Local commands such as `meegle auth status`, `meegle config`, `meegle inspect`, `meegle completion`, and `meegle url` remain available.")
}

// isDryRunFlag reports whether --dry-run was passed. Lives at package scope so
// SessionStep and McpExecutorStep can both short-circuit on it.
func isDryRunFlag(parsed *router.ParsedCommand) bool {
	if parsed == nil {
		return false
	}
	v, _ := parsed.Flags["dry-run"].(bool)
	return v
}

// ---------------------------------------------------------------------------
// batchNDJSONHook — flattens batch {results, errors, summary} payloads when
// --format=ndjson so the composite formatter emits one record per line. Gated
// on TagMcpBatch so a non-batch payload that happens to carry similar keys is
// passed through untouched.
// ---------------------------------------------------------------------------

type batchNDJSONHook struct{}

func (batchNDJSONHook) Name() string { return "meegle_batch_ndjson" }

func (batchNDJSONHook) Process(ctx *frameworkoutput.Context, data any) (any, error) {
	if ctx == nil || ctx.Options == nil || ctx.Options.Mode != "ndjson" {
		return data, nil
	}
	if ctx.Parsed == nil || ctx.Parsed.Node == nil {
		return data, nil
	}
	if ctx.Parsed.Node.Meta.Tags[TagMcpBatch] != "1" {
		return data, nil
	}
	if reshaped, ok := reshapeBatchForNDJSON(data); ok {
		return reshaped, nil
	}
	return data, nil
}

// meegleOutputProcessor wraps formatting.DefaultProcessor() and inserts the
// meegle-specific batch ndjson reshape hook before the standard hooks so the
// per-row data flows through select/envelope as a flat slice.
func meegleOutputProcessor() *frameworkoutput.Processor {
	proc := formatting.DefaultProcessor()
	proc.Hooks = append([]frameworkoutput.OutputHook{batchNDJSONHook{}}, proc.Hooks...)
	return proc
}

// ---------------------------------------------------------------------------
// buildSnakeParams — shared parameter construction for McpExecutorStep and AutoPaginateStep
// ---------------------------------------------------------------------------

// buildSnakeParams builds the snake_case parameter map from pipeline state,
// performing kebab-to-snake conversion, type coercion, and mcp_fixed_params injection.
// Extracted from McpExecutorStep so AutoPaginateStep can reuse the same logic
// when constructing follow-up requests with page_token / page_num overrides.
func buildSnakeParams(state *pipeline.PipelineContext) map[string]any {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return map[string]any{}
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}

	var paramTypes map[string]string // kebab-name -> MCP type
	var paramItems map[string]string // kebab-name -> items.type
	if state.Parsed.Node.Meta.Tags != nil {
		if raw, ok := state.Parsed.Node.Meta.Tags["mcp_param_types"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramTypes)
		}
		if raw, ok := state.Parsed.Node.Meta.Tags["mcp_param_items"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramItems)
		}
	}

	explicitKeys := make(map[string]bool)
	for k := range state.Parsed.ExplicitFlags {
		if isMeegleRuntimeFlag(k) {
			continue
		}
		explicitKeys[k] = true
	}

	snakeParams := make(map[string]any, len(state.Values))
	for k, v := range state.Values {
		if !explicitKeys[k] {
			continue
		}
		origType := paramTypes[k]
		snakeKey := strings.ReplaceAll(k, "-", "_")
		snakeParams[snakeKey] = coerceValue(v, origType, paramItems[k])
	}

	if state.Parsed.Node.Meta.Tags != nil {
		if raw, ok := state.Parsed.Node.Meta.Tags["mcp_fixed_params"]; ok {
			var fixed map[string]any
			if json.Unmarshal([]byte(raw), &fixed) == nil {
				for k, v := range fixed {
					if _, exists := snakeParams[k]; !exists {
						snakeParams[k] = v
					}
				}
			}
		}
	}

	return snakeParams
}

// ---------------------------------------------------------------------------
// AutoPaginateStep — fetches and merges all pages when --auto-paginate is set
// ---------------------------------------------------------------------------

const maxAutoPaginatePages = 200

// maxEmptyPageStreak is the number of consecutive pages that add zero new
// items before auto-paginate gives up, even if the backend keeps returning a
// non-empty page_token. Protects against a stuck cursor burning the budget.
//
// Value rationale: 3 strikes a balance — 1 would be too aggressive (a single
// legitimately empty page from a sparse filter would abort early), while 5+
// wastes budget on a genuinely stuck cursor. At 3, we tolerate one transient
// empty page (streak resets on the next non-empty page) but still stop quickly
// when no progress is being made. The budget saved by stopping early (up to
// ~197 follow-up calls) far outweighs the cost of 2 extra empty-page fetches.
const maxEmptyPageStreak = 3

type AutoPaginateStep struct {
	// CommandsFunc returns the mapped commands for error message sanitization.
	// Injected by the pipeline factory to avoid package-level mutable state.
	// Same pattern as McpExecutorStep — used to sanitize tool names in
	// follow-up CallTool errors so users see clean command paths, not raw
	// MCP tool names like "feishu-project_workitem_data_skill.get_workitem_op_records".
	CommandsFunc func() []types.MappedCommand
}

func (s *AutoPaginateStep) Name() string { return "auto_paginate" }

func (s *AutoPaginateStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Result == nil {
		return nil
	}
	// Only active when --auto-paginate is explicitly set.
	if !isAutoPaginateFlag(state.Parsed) {
		return nil
	}
	// Batch and attachment shortcut commands have their own execution paths.
	if state.Parsed.Node != nil {
		if state.Parsed.Node.Meta.Tags[TagMcpBatch] == "1" {
			return nil
		}
		if state.Parsed.Node.Meta.Tags[TagAttachmentShortcut] != "" {
			return nil
		}
	}
	// The result must be a map (the typical MCP tool response shape).
	data, ok := state.Result.Data.(map[string]any)
	if !ok {
		return nil
	}

	toolName := state.Parsed.Node.HandlerRef
	originalParams := buildSnakeParams(state)
	client := newMcpClientFromState(state)

	// Try page_token-based pagination first, then page_num-based.
	merged, meta, err := paginateByToken(ctx, client, toolName, originalParams, data, state.Parsed, s, state)
	if err != nil {
		return err
	}
	if meta == nil {
		// page_token path did not trigger; try page_num-based pagination.
		merged, meta, err = paginateByPageNum(ctx, client, toolName, originalParams, data, state.Parsed, s, state)
		if err != nil {
			return err
		}
	}
	if meta == nil {
		return nil // no pagination signals found
	}

	// Replace result data with merged payload.
	state.Result.Data = merged

	// Enrich metadata.
	if state.Result.Metadata == nil {
		state.Result.Metadata = map[string]any{}
	}
	for k, v := range meta {
		state.Result.Metadata[k] = v
	}
	return nil
}

func isAutoPaginateFlag(parsed *router.ParsedCommand) bool {
	if parsed == nil {
		return false
	}
	v, _ := parsed.Flags["auto-paginate"].(bool)
	return v
}

// paginateByToken handles next_page_token-based pagination.
// If the payload contains a non-empty next_page_token, it loops: calls the
// MCP tool with the token, merges the list arrays, and repeats until the
// token is empty or the 200-page limit is reached.
//
// The token parameter name sent to the backend is resolved from the command's
// mcp_param_types metadata: if the tool declares "next_page_token" (the
// catalog convention, e.g. get-op-records), that key is used; otherwise it
// falls back to "page_token". This avoids sending a wrong cursor parameter
// name that the backend would silently ignore, causing it to return page 1
// repeatedly and producing 200 pages of duplicate data (the empty-page guard
// does not trigger because the array length keeps growing).
//
// Returns (mergedPayload, paginationMeta, error). If the payload has no
// next_page_token, returns (originalData, nil, nil) so the caller can try
// page_num-based pagination instead.
func paginateByToken(ctx context.Context, client *mcpclient.Client, toolName string, baseParams map[string]any, firstData map[string]any, parsed *router.ParsedCommand, step *AutoPaginateStep, state *pipeline.PipelineContext) (map[string]any, map[string]any, error) {
	firstToken := getString(firstData, "next_page_token")
	if firstToken == "" {
		return firstData, nil, nil
	}

	tokenParam := resolveTokenParamName(parsed)
	transform := LookupResultTransform(parsed.FullPath)

	merged := shallowCopyMap(firstData)
	pageCount := 1
	currentToken := firstToken
	var lastToken string
	emptyStreak := 0 // consecutive pages that added zero new items
	stoppedReason := ""

	for currentToken != "" {
		if pageCount >= maxAutoPaginatePages {
			lastToken = currentToken
			break
		}
		pageCount++

		params := withParam(baseParams, tokenParam, currentToken)
		resp, err := client.CallTool(ctx, toolName, params)
		if err != nil {
			return nil, nil, step.sanitizeError(err, state, toolName)
		}

		rawData := resp.Data
		if transform != nil {
			rawData = transform(rawData)
		}
		pageData, ok := rawData.(map[string]any)
		if !ok {
			break
		}
		beforeLen := getListLength(merged)
		merged = mergePayloads(merged, pageData)
		afterLen := getListLength(merged)
		currentToken = getString(pageData, "next_page_token")

		// Guard against backends that keep returning a non-empty token while
		// yielding no new items: stop after a few consecutive empty pages so
		// we don't burn through the full 200-page budget on a stuck cursor.
		if afterLen == beforeLen {
			emptyStreak++
			if emptyStreak >= maxEmptyPageStreak {
				stoppedReason = "consecutive_empty_pages"
				break
			}
		} else {
			emptyStreak = 0
		}
	}

	delete(merged, "next_page_token")

	meta := map[string]any{
		"auto_paginated": true,
		"pages_merged":   pageCount,
		"total_items":    getListLength(merged),
	}
	if lastToken != "" {
		meta["truncated"] = true
		meta["next_page_token"] = lastToken
		fmt.Fprintf(os.Stderr,
			"[meegle] auto-paginate reached %d-page limit; merged %v items across %d pages.\nNext page token: %s. Re-run with --%s %s to continue.\n",
			maxAutoPaginatePages, meta["total_items"], pageCount, lastToken, strings.ReplaceAll(tokenParam, "_", "-"), lastToken)
	}
	if stoppedReason != "" {
		meta["stopped_reason"] = stoppedReason
		fmt.Fprintf(os.Stderr,
			"[meegle] auto-paginate stopped early after %d consecutive empty pages; merged %v items across %d pages.\nIf you confirmed there is still data, check the backend pagination logic or re-run with a smaller page size.\n",
			maxEmptyPageStreak, meta["total_items"], pageCount)
	}

	return merged, meta, nil
}

// paginateByPageNum handles page_num + pagination.has_more based pagination.
// If the payload contains a pagination.has_more=true, it loops: increments
// page_num, calls the MCP tool, merges the list arrays, and repeats until
// has_more is false, the merged count reaches total, or the 200-page limit
// is reached.
// Returns (mergedPayload, paginationMeta, error). If no page_num pagination
// signals are found, returns (originalData, nil, nil).
func paginateByPageNum(ctx context.Context, client *mcpclient.Client, toolName string, baseParams map[string]any, firstData map[string]any, parsed *router.ParsedCommand, step *AutoPaginateStep, state *pipeline.PipelineContext) (map[string]any, map[string]any, error) {
	pagination, ok := firstData["pagination"].(map[string]any)
	if !ok {
		return firstData, nil, nil
	}
	hasMore, _ := pagination["has_more"].(bool)
	if !hasMore {
		return firstData, nil, nil
	}
	// Only activate page_num auto-pagination if the tool actually declares a
	// page_num parameter in its mcp_param_types metadata. Some commands use
	// offset/cursor/group-pagination-list instead; blindly sending page_num
	// to those would either be ignored (backend returns page 1 repeatedly,
	// producing duplicate data) or rejected as an unknown parameter.
	if !hasParamInMetadata(parsed, "page_num") {
		return firstData, nil, nil
	}
	// If baseParams already has page_num, use that as the starting page;
	// otherwise default to 1.
	startPage := 1
	if raw, exists := baseParams["page_num"]; exists {
		if n, ok := toInt(raw); ok {
			startPage = n
		}
	}

	transform := LookupResultTransform(parsed.FullPath)

	merged := shallowCopyMap(firstData)
	total := toInt64(pagination["total"])
	if total == 0 {
		// Try top-level total.
		total = toInt64(firstData["total"])
	}

	pageCount := 1
	currentPage := startPage
	truncated := false
	emptyStreak := 0 // consecutive pages that added zero new items
	stoppedReason := ""

	for {
		if pageCount >= maxAutoPaginatePages {
			truncated = true
			break
		}
		if total > 0 && int64(getListLength(merged)) >= total {
			break
		}
		currentPage++

		params := withParam(baseParams, "page_num", currentPage)
		resp, err := client.CallTool(ctx, toolName, params)
		if err != nil {
			return nil, nil, step.sanitizeError(err, state, toolName)
		}

		rawData := resp.Data
		if transform != nil {
			rawData = transform(rawData)
		}
		pageData, ok := rawData.(map[string]any)
		if !ok {
			break
		}
		pageCount++

		// Check pagination signal before the streak guard so that "normal
		// end" (has_more=false) and "stuck cursor" (empty streak) are
		// clearly separated — the streak guard only runs when the backend
		// says there are more pages.
		hasMore := false
		if pg, ok := pageData["pagination"].(map[string]any); ok {
			hasMore, _ = pg["has_more"].(bool)
		}
		if !hasMore {
			merged = mergePayloads(merged, pageData)
			break
		}

		beforeLen := getListLength(merged)
		merged = mergePayloads(merged, pageData)
		afterLen := getListLength(merged)

		// Guard against backends that keep returning has_more=true while
		// yielding no new items: stop after a few consecutive empty pages.
		if afterLen == beforeLen {
			emptyStreak++
			if emptyStreak >= maxEmptyPageStreak {
				stoppedReason = "consecutive_empty_pages"
				break
			}
		} else {
			emptyStreak = 0
		}
	}

	// Flatten: move total to top-level, remove pagination wrapper.
	if pg, ok := merged["pagination"].(map[string]any); ok {
		if _, exists := merged["total"]; !exists {
			if t := toInt64(pg["total"]); t > 0 {
				merged["total"] = t
			}
		}
		delete(merged, "pagination")
	}

	meta := map[string]any{
		"auto_paginated": true,
		"pages_merged":   pageCount,
		"total_items":    getListLength(merged),
	}
	if truncated {
		meta["truncated"] = true
		meta["next_page_num"] = currentPage + 1
		fmt.Fprintf(os.Stderr,
			"[meegle] auto-paginate reached %d-page limit; merged %v items across %d pages.\nRe-run with --page-num %d to continue.\n",
			maxAutoPaginatePages, meta["total_items"], pageCount, currentPage+1)
	}
	if stoppedReason != "" {
		meta["stopped_reason"] = stoppedReason
		fmt.Fprintf(os.Stderr,
			"[meegle] auto-paginate stopped early after %d consecutive empty pages; merged %v items across %d pages.\nIf you confirmed there is still data, check the backend pagination logic or re-run with a smaller page size.\n",
			maxEmptyPageStreak, meta["total_items"], pageCount)
	}

	return merged, meta, nil
}

// mergePayloads merges two MCP response payloads.
// - Array fields are concatenated (e.g. "list").
// - Map fields are shallow-merged (e.g. "pagination").
// - Scalar fields: the newer value wins.
//
// Note: array concatenation uses append, which is O(n) per merge where n is
// the current merged array length. Across P pages each with k items this sums
// to O(P*k * P) = O(P^2 * k). Under the maxAutoPaginatePages=200 cap this is
// acceptable (200^2 * 50 = 2M element copies). If the page cap is raised
// significantly, switch to collecting page arrays in a [][]any and flattening
// once at the end.
func mergePayloads(base, next map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range next {
		existing, exists := result[k]
		if !exists {
			result[k] = v
			continue
		}
		if existingArr, ok := existing.([]any); ok {
			if nextArr, ok := v.([]any); ok {
				result[k] = append(existingArr, nextArr...)
				continue
			}
		}
		if existingMap, ok := existing.(map[string]any); ok {
			if nextMap, ok := v.(map[string]any); ok {
				merged := make(map[string]any, len(existingMap)+len(nextMap))
				for mk, mv := range existingMap {
					merged[mk] = mv
				}
				for mk, mv := range nextMap {
					merged[mk] = mv
				}
				result[k] = merged
				continue
			}
		}
		result[k] = v
	}
	return result
}

// shallowCopyMap creates a shallow copy of a map[string]any. Only the top-level
// keys are copied; nested values (arrays, maps) are shared. This is safe for
// mergePayloads because it always creates new arrays/maps during merge rather
// than mutating the originals in place.
func shallowCopyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// withParam returns a copy of base with the given key set to value.
func withParam(base map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

// getString returns the string value of key in m, or "" if absent/not a string.
func getString(m map[string]any, key string) string {
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// toInt attempts to convert v to an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// toInt64 attempts to convert v to an int64.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// listFieldPriority is the priority order for detecting the primary list field
// in a response payload. The first matching key is used; if none match, the
// first []any value found is used as fallback.
var listFieldPriority = []string{"list", "items", "data"}

// getListLength returns the length of the primary list field in m. It prefers
// well-known field names (list, items, data) in priority order to avoid
// depending on Go's randomized map iteration order. If none of the priority
// keys exist, falls back to the first []any value found.
func getListLength(m map[string]any) int {
	for _, key := range listFieldPriority {
		if arr, ok := m[key].([]any); ok {
			return len(arr)
		}
	}
	// Fallback: first []any value found (order non-deterministic but only
	// affects payloads without any of the priority field names).
	for _, v := range m {
		if arr, ok := v.([]any); ok {
			return len(arr)
		}
	}
	return 0
}

// hasParamInMetadata reports whether the command declares the given snake_case
// parameter in its mcp_param_types metadata tag. The metadata keys are kebab-case
// (e.g. "page-num"), so we convert the input to kebab before looking up.
func hasParamInMetadata(parsed *router.ParsedCommand, snakeParam string) bool {
	if parsed == nil || parsed.Node == nil || parsed.Node.Meta.Tags == nil {
		return false
	}
	raw, ok := parsed.Node.Meta.Tags["mcp_param_types"]
	if !ok {
		return false
	}
	var paramTypes map[string]string // kebab-name -> MCP type
	if json.Unmarshal([]byte(raw), &paramTypes) != nil {
		return false
	}
	kebab := strings.ReplaceAll(snakeParam, "_", "-")
	_, exists := paramTypes[kebab]
	return exists
}

// resolveTokenParamName returns the snake_case token parameter name that the
// command declares. It inspects mcp_param_types for known token parameter
// names (next_page_token, page_token). If the command declares one of them,
// that name is used; otherwise "page_token" is the fallback. This ensures we
// send the cursor under the correct parameter name — e.g. get-op-records
// declares "next_page_token", so we must send next_page_token (not page_token)
// or the backend silently ignores the cursor and returns page 1 repeatedly.
func resolveTokenParamName(parsed *router.ParsedCommand) string {
	if parsed == nil || parsed.Node == nil || parsed.Node.Meta.Tags == nil {
		return "page_token"
	}
	raw, ok := parsed.Node.Meta.Tags["mcp_param_types"]
	if !ok {
		return "page_token"
	}
	var paramTypes map[string]string // kebab-name -> MCP type
	if json.Unmarshal([]byte(raw), &paramTypes) != nil {
		return "page_token"
	}
	// Prefer "next_page_token" (the catalog convention) over "page_token".
	for _, candidate := range []string{"next_page_token", "page_token"} {
		kebab := strings.ReplaceAll(candidate, "_", "-")
		if _, exists := paramTypes[kebab]; exists {
			return candidate
		}
	}
	return "page_token"
}

// sanitizeError applies the same error enrichment as McpExecutorStep:
// sanitizeToolNames (clean MCP tool names → user-facing command paths) and
// attachMyWorkTodoSuggestion (troubleshooting hint for mywork todo). Shared
// so that follow-up CallTool errors during auto-paginate get identical
// treatment as the first-page error.
func (s *AutoPaginateStep) sanitizeError(err error, state *pipeline.PipelineContext, toolName string) error {
	if err == nil {
		return nil
	}
	if me, ok := err.(*meerrors.MeegleError); ok && s.CommandsFunc != nil {
		me.Message = sanitizeToolNames(me.Message, s.CommandsFunc())
	}
	attachMyWorkTodoSuggestion(err, state, toolName)
	return err
}

// ---------------------------------------------------------------------------
// newPipelineFactory — Assembles the step chain for the meegle product
// ---------------------------------------------------------------------------

func newPipelineFactory(setup *DynamicRegistrySetup) cliapp.PipelineFactory {
	return func(cfg cliapp.Config) (*pipeline.Pipeline, error) {
		return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
			&pipeline.ParamMergeStep{},
			&MeegleValidateStep{},
			&SessionStep{},
			&McpExecutorStep{CommandsFunc: setup.MappedCommands},
			&AutoPaginateStep{CommandsFunc: setup.MappedCommands},
			&BatchExecutorStep{CommandsFunc: setup.MappedCommands},
			&AttachmentShortcutStep{},
			&pipeline.OutputStep{Processor: meegleOutputProcessor()},
		}}, nil
	}
}

// newMcpClientFromState builds an MCP client from the shared session state
// written by SessionStep. Used by both McpExecutorStep (single-tool call) and
// BatchExecutorStep (client-side fan-out over one underlying tool).
func newMcpClientFromState(state *pipeline.PipelineContext) *mcpclient.Client {
	host, _ := state.OutputConfig["mcp.host"].(string)
	token, _ := state.OutputConfig["mcp.token"].(string)
	tm, _ := state.OutputConfig["mcp.token_manager"].(*auth.TokenManager)
	hdrs, _ := state.OutputConfig["mcp.headers"].(map[string]string)
	authHeader, _ := state.OutputConfig["mcp.access_token_header"].(string)

	// SDK mode injects a pre-built server URL (with query params); CLI mode falls back to host.
	baseURL, _ := state.OutputConfig["mcp.server_url"].(string)
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s/mcp_server/v1", host)
	}

	// Build http.Header from config headers. Authorization is handled by
	// tokenFunc; when a custom auth header is in use, drop any matching key
	// from the static headers too so tokenFunc remains the single writer.
	httpHeaders := make(http.Header)
	for k, v := range hdrs {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		if authHeader != "" && strings.EqualFold(k, authHeader) {
			continue
		}
		httpHeaders.Set(k, v)
	}

	tokenFunc := func() (string, error) { return token, nil }
	if tm != nil {
		// Store-backed credentials must be read again for the retry after a
		// successful refresh; retaining the session snapshot would resend the
		// rejected token and turn a successful refresh into another 401.
		tokenFunc = tm.GetToken
	}
	opts := []mcpclient.Option{
		mcpclient.WithToken(tokenFunc),
		mcpclient.WithHeaders(httpHeaders),
	}
	if authHeader != "" {
		opts = append(opts, mcpclient.WithAuthHeader(authHeader))
	}
	if ua, _ := state.OutputConfig["mcp.user_agent"].(string); ua != "" {
		opts = append(opts, mcpclient.WithUserAgent(ua))
	}
	if tm != nil {
		opts = append(opts, mcpclient.WithRefreshFunc(tm.TryRefresh))
	}
	return mcpclient.New(baseURL, opts...)
}

// ---------------------------------------------------------------------------
// sanitizeToolNames — Replaces MCP tool names in error messages with CLI command names
// ---------------------------------------------------------------------------

func sanitizeToolNames(msg string, commands []types.MappedCommand) string {
	if len(commands) == 0 {
		return msg
	}
	sorted := make([]types.MappedCommand, len(commands))
	copy(sorted, commands)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].ToolName) > len(sorted[j].ToolName) // Replace longer names first
	})
	for _, cmd := range sorted {
		if strings.Contains(msg, cmd.ToolName) {
			cliName := fmt.Sprintf("meegle %s %s", cmd.Resource, cmd.Method)
			msg = strings.ReplaceAll(msg, cmd.ToolName, cliName)
		}
	}
	return msg
}

// ---------------------------------------------------------------------------
// coerceValue — Converts parameter values based on the original MCP type
// ---------------------------------------------------------------------------

// coerceValue converts string values collected by the framework to the correct type based on the original MCP type.
func coerceValue(v any, mcpType string, itemsType string) any {
	switch mcpType {
	case "number":
		if s, ok := v.(string); ok && s != "" {
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return n
			}
		}
		return v
	case "array":
		return coerceArray(v, itemsType)
	case "object":
		// Object-typed MCP params are surfaced as `--flag string` (see
		// registry.mapParamType) and must be JSON-decoded before reaching
		// the backend; otherwise the server receives a string literal and
		// silently drops the field (e.g. issue#14 `subtask update --schedule`).
		// On parse failure keep the raw string so the backend can surface
		// a clearer error than a no-op.
		if s, ok := v.(string); ok && s != "" {
			trimmed := strings.TrimSpace(s)
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
				var parsed any
				if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
					return parsed
				}
			}
		}
		return v
	default:
		return v
	}
}

// coerceArray implements array parsing logic equivalent to master's splitAndCoerce.
// Input may be []string (cobra StringArray/StringSlice) or string (single value).
//
// Element handling: if an element looks like JSON (starts with '[' or '{'), it
// is JSON-parsed; otherwise left as a plain string. A single JSON-array value
// is returned unwrapped, and a single JSON-object value is wrapped into a
// one-element array so object-item schemas always receive an array.
func coerceArray(v any, itemsType string) any {
	numericItems := itemsType == "number"

	switch val := v.(type) {
	case []string:
		if len(val) == 1 {
			return coerceArraySingleValue(val[0], numericItems)
		}
		// Multiple values: parse each element. If an element is JSON (object
		// or array), decode it; otherwise keep the raw string.
		if numericItems {
			nums := make([]float64, 0, len(val))
			allNums := true
			for _, s := range val {
				var n float64
				if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &n); err == nil {
					nums = append(nums, n)
				} else {
					allNums = false
					break
				}
			}
			if allNums && len(nums) > 0 {
				return nums
			}
		}
		result := make([]any, len(val))
		for i, s := range val {
			result[i] = decodeArrayElement(s)
		}
		return result
	case string:
		return coerceArraySingleValue(val, numericItems)
	default:
		return v
	}
}

// decodeArrayElement JSON-parses a string element if it looks like JSON,
// otherwise returns it verbatim.
func decodeArrayElement(s string) any {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}
	return s
}

// coerceArraySingleValue handles array parsing for a single string value.
// A JSON-object value is wrapped into a one-element array so object-shaped
// items (e.g. fields[]) can be passed via a single `--flag '{...}'` form.
func coerceArraySingleValue(val string, numericItems bool) any {
	trimmed := strings.TrimSpace(val)
	// Starts with [ or { -> try JSON parsing
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			if _, isArray := parsed.([]any); !isArray {
				return []any{parsed}
			}
			return parsed
		}
	}
	// Comma split + optional number coerce
	return splitAndCoerce(val, numericItems)
}

// splitAndCoerce splits a comma-delimited string into an array, optionally coercing elements to float64.
// Ported from master internal/cli/dynamic.go.
func splitAndCoerce(val string, numeric bool) any {
	parts := strings.Split(val, ",")
	trimmed := make([]string, len(parts))
	for i, p := range parts {
		trimmed[i] = strings.TrimSpace(p)
	}
	if numeric {
		nums := make([]float64, 0, len(trimmed))
		allNums := true
		for _, s := range trimmed {
			var n float64
			if _, err := fmt.Sscanf(s, "%f", &n); err == nil {
				nums = append(nums, n)
			} else {
				allNums = false
				break
			}
		}
		if allNums && len(nums) > 0 {
			return nums
		}
	}
	result := make([]any, len(trimmed))
	for i, s := range trimmed {
		result[i] = s
	}
	return result
}
