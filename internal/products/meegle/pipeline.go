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

	// Read original MCP type info (written by registry.go buildCommandTree into Meta.Tags)
	var paramTypes map[string]string // kebab-name -> MCP type
	var paramItems map[string]string // kebab-name -> items.type
	if state.Parsed.Node != nil && state.Parsed.Node.Meta.Tags != nil {
		if raw, ok := state.Parsed.Node.Meta.Tags["mcp_param_types"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramTypes)
		}
		if raw, ok := state.Parsed.Node.Meta.Tags["mcp_param_items"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramItems)
		}
	}

	// Only send parameters explicitly provided by the user (skip cobra defaults like empty string/false/0)
	// master's makeRunFunc also only collects Changed flags
	explicitKeys := make(map[string]bool)
	for k := range state.Parsed.ExplicitFlags {
		if isMeegleRuntimeFlag(k) {
			continue
		}
		explicitKeys[k] = true
	}

	// Type conversion + kebab -> snake_case
	snakeParams := make(map[string]any, len(state.Values))
	for k, v := range state.Values {
		if !explicitKeys[k] {
			continue // Skip parameters not explicitly provided by the user (cobra defaults)
		}
		origType := paramTypes[k] // Look up type using kebab key
		snakeKey := strings.ReplaceAll(k, "-", "_")
		snakeParams[snakeKey] = coerceValue(v, origType, paramItems[k])
	}

	// Inject mcp_fixed_params (user-explicit parameters take priority)
	if state.Parsed.Node != nil && state.Parsed.Node.Meta.Tags != nil {
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
// newPipelineFactory — Assembles the step chain for the meegle product
// ---------------------------------------------------------------------------

func newPipelineFactory(setup *DynamicRegistrySetup) cliapp.PipelineFactory {
	return func(cfg cliapp.Config) (*pipeline.Pipeline, error) {
		return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{
			&pipeline.ParamMergeStep{},
			&MeegleValidateStep{},
			&SessionStep{},
			&McpExecutorStep{CommandsFunc: setup.MappedCommands},
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

	opts := []mcpclient.Option{
		mcpclient.WithToken(func() (string, error) { return token, nil }),
		mcpclient.WithHeaders(httpHeaders),
	}
	if authHeader != "" {
		opts = append(opts, mcpclient.WithAuthHeader(authHeader))
	}
	if ua, _ := state.OutputConfig["mcp.user_agent"].(string); ua != "" {
		opts = append(opts, mcpclient.WithUserAgent(ua))
	}
	opts = append(opts, mcpclient.WithRefreshFunc(func() error {
		if tm == nil {
			return fmt.Errorf("no token manager")
		}
		store, _ := state.OutputConfig["mcp.store"].(auth.TokenStore)
		if store == nil {
			return fmt.Errorf("no token store")
		}
		data, err := store.Load()
		if err != nil || data == nil {
			return fmt.Errorf("no token data")
		}
		_, err = tm.RefreshToken(data)
		return err
	}))
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
