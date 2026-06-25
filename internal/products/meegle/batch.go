// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// Tags written into a command node's Meta.Tags to mark it as a batch command
// and describe the wiring BatchExecutorStep needs to fan-out per-item calls.
const (
	TagMcpBatch        = "mcp_batch"          // value "1" -> handled by BatchExecutorStep
	TagMcpBatchPerItem = "mcp_batch_per_item" // snake-case MCP param name, e.g. "work_item_id"
)

// batchConcurrency is fixed on purpose (plan: no external knob).
// maxBatchIDs caps the CLI against being used as an unbounded batch gateway.
const (
	batchConcurrency = 3
	maxBatchIDs      = 200
)

// batchCommand describes a CLI command that calls one MCP tool N times with
// one parameter varying per item and the rest shared across all calls.
// Shared flags and the underlying MCP tool name are derived from a sibling
// single-item command at inject time, so +batch-get stays in sync with
// whatever the MCP server actually requires (e.g. project_key showing up
// as a required parameter is picked up automatically).
//
// The `+` prefix on the injected Name marks this as a scenario / client-side
// sugar command (no 1:1 MCP tool behind it) — see validator R2.
type batchCommand struct {
	Group             string   // parent resource (e.g. "workitem")
	Name              string   // sub-command (e.g. "+batch-get")
	Aliases           []string // optional command aliases
	Brief             string   // --help summary
	SingleCommandName string   // sibling to inherit flags + HandlerRef from (e.g. "get")
	PerItemParam      string   // snake-case MCP param that varies per item
	IDsFlag           string   // kebab-case flag carrying the list of IDs
	IDsFileFlag       string   // optional flag for a file containing IDs
}

// batchCommands is the static list of batch commands injected into the
// dynamic command tree. Adding a new batch command is a two-line change:
// append an entry here and make sure its Group + SingleCommandName match
// something produced by MCP discovery.
var batchCommands = []batchCommand{
	{
		Group:             "workitem",
		Name:              "+batch-get",
		Aliases:           []string{"+get-batch"},
		Brief:             "Batch-read work items by IDs (client-side fan-out over `workitem get`).",
		SingleCommandName: "get",
		PerItemParam:      "work_item_id",
		IDsFlag:           "work-item-ids",
		IDsFileFlag:       "ids-file",
	},
}

// injectBatchCommands attaches batch command nodes under their target group,
// inheriting flags and MCP tool wiring from the configured sibling single-
// item command so schema changes in the MCP tool flow through automatically.
//
// Idempotent: an existing child with the same name is left untouched and the
// batch entry is skipped, so a re-run never overwrites a real discovery.
// Silent no-op if either the parent group or the sibling is missing —
// keeps `meegle --help` from blowing up when MCP discovery is incomplete.
func injectBatchCommands(nodes []*registry.CommandNode) {
	for i := range batchCommands {
		bc := &batchCommands[i]
		target := findNodeByName(nodes, bc.Group)
		if target == nil {
			continue
		}
		if hasChildNamed(target, bc.Name) {
			continue
		}
		sibling := findChild(target, bc.SingleCommandName)
		if sibling == nil {
			continue
		}

		flags, tags := deriveBatchFlagsAndTags(bc, sibling)

		target.Children = append(target.Children, &registry.CommandNode{
			Name:    bc.Name,
			Aliases: bc.Aliases,
			Help: registry.HelpText{
				Brief: bc.Brief,
				Long:  buildBatchLongHelp(bc),
			},
			Flags:      flags,
			HandlerRef: sibling.HandlerRef,
			Meta:       registry.NodeMeta{Tags: tags},
		})
	}
}

// buildBatchLongHelp renders the --help Long text for a batch command,
// interpolating the fan-out caps from their constants so the text can't
// drift from the behavior.
func buildBatchLongHelp(bc *batchCommand) string {
	lines := []string{
		fmt.Sprintf("Client-side fan-out over `meegle %s %s`, one call per ID.", bc.Group, bc.SingleCommandName),
		fmt.Sprintf("Up to %d IDs per invocation, executed with %d concurrent workers.", maxBatchIDs, batchConcurrency),
		"",
		fmt.Sprintf("Provide IDs via --%s (comma-separated or repeated)", bc.IDsFlag),
	}
	if bc.IDsFileFlag != "" {
		lines[len(lines)-1] += fmt.Sprintf(" or --%s (one per line, '#' starts a comment).", bc.IDsFileFlag)
	} else {
		lines[len(lines)-1] += "."
	}
	lines = append(lines,
		"",
		"Output shape: {results, errors, summary}. Use `--format ndjson` to stream one record per line (summary on the last line).",
	)
	return strings.Join(lines, "\n")
}

// deriveBatchFlagsAndTags builds the batch node's flag list and Meta.Tags
// by inheriting from sibling. IDs flags come first (so --help surfaces them
// first), then every sibling flag except the per-item one. Shared flags
// keep their original Required semantics so MeegleValidateStep can
// fail-fast on missing required params before the fan-out starts.
func deriveBatchFlagsAndTags(bc *batchCommand, sibling *registry.CommandNode) ([]registry.FlagDef, map[string]string) {
	perItemKebab := strings.ReplaceAll(bc.PerItemParam, "_", "-")

	flags := []registry.FlagDef{
		{
			Name:        bc.IDsFlag,
			Type:        registry.FlagTypeStringSlice,
			Description: fmt.Sprintf("Target IDs (comma-separated or repeated). Required unless --%s is given.", bc.IDsFileFlag),
		},
	}
	if bc.IDsFileFlag != "" {
		flags = append(flags, registry.FlagDef{
			Name:        bc.IDsFileFlag,
			Type:        registry.FlagTypeString,
			Description: "Read IDs from file (one per line or comma-separated; lines starting with # are ignored).",
		})
	}
	for _, f := range sibling.Flags {
		if f.Name == perItemKebab {
			continue
		}
		flags = append(flags, f)
	}

	tags := map[string]string{
		TagMcpBatch:        "1",
		TagMcpBatchPerItem: bc.PerItemParam,
	}
	// Copy sibling's MCP metadata so BatchExecutorStep builds byte-identical
	// MCP payloads to the single-command path:
	//   - mcp_param_types / mcp_param_items drive value coercion
	//   - mcp_fixed_params supplies params the sibling injects automatically
	//     (e.g. sugar commands like `user me` bind fixed "user_keys").
	// Missing any of these would cause batch calls to silently differ from
	// what the single-command path sends.
	for _, key := range []string{"mcp_param_types", "mcp_param_items", "mcp_fixed_params"} {
		if raw, ok := sibling.Meta.Tags[key]; ok && raw != "" {
			tags[key] = raw
		}
	}
	return flags, tags
}

func findNodeByName(nodes []*registry.CommandNode, name string) *registry.CommandNode {
	for _, node := range nodes {
		if node != nil && node.Name == name {
			return node
		}
	}
	return nil
}

func hasChildNamed(node *registry.CommandNode, name string) bool {
	for _, c := range node.Children {
		if c != nil && c.Name == name {
			return true
		}
	}
	return false
}

func findChild(parent *registry.CommandNode, name string) *registry.CommandNode {
	if parent == nil {
		return nil
	}
	for _, c := range parent.Children {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// BatchExecutorStep — Fans out one MCP tool call per ID in the batch input.
// ---------------------------------------------------------------------------

// mcpToolCaller is the subset of *mcpclient.Client BatchExecutorStep needs.
// Keeping the dependency narrow lets tests substitute a fake without spinning
// up a real HTTP transport.
type mcpToolCaller interface {
	CallTool(ctx context.Context, name string, params map[string]any) (*mcpResponse, error)
}

// mcpResponse mirrors mcpclient.Response's Data field (the only bit the
// batch step consumes). Declared here so mcpToolCaller does not force
// test code to construct a real mcpclient.Response.
type mcpResponse struct {
	Data any
}

// BatchExecutorStep invokes a single underlying MCP tool once per ID for
// commands tagged with TagMcpBatch. Auth failures fail the whole batch;
// every other per-item error is collected into the response payload.
type BatchExecutorStep struct {
	CommandsFunc func() []types.MappedCommand
	// clientFactory is overridable for tests. When nil, production wiring
	// through newMcpClientFromState is used.
	clientFactory func(state *pipeline.PipelineContext) mcpToolCaller
}

func (s *BatchExecutorStep) Name() string { return "batch_executor" }

func (s *BatchExecutorStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	if state.Parsed.Node.Meta.Tags[TagMcpBatch] != "1" {
		return nil
	}
	perItemParam := state.Parsed.Node.Meta.Tags[TagMcpBatchPerItem]
	if perItemParam == "" {
		return meerrors.NewClientError("CLIENT_MISCONFIGURED",
			"batch command is missing per-item param tag")
	}
	toolName := state.Parsed.Node.HandlerRef
	if toolName == "" {
		return meerrors.NewClientError("CLIENT_MISCONFIGURED",
			"batch command is missing underlying tool reference")
	}

	ids, err := collectBatchIDs(state)
	if err != nil {
		return err
	}

	shared := batchSharedParams(state)
	// Look up the per-item MCP type (e.g. "string" / "number") from the
	// inherited mcp_param_types tag so fan-out sends the per-item value the
	// same way the single-command path would — the exact contract tested
	// against the real MCP server. Without this, the batch would send a
	// raw int64 while the server expects e.g. a string and rejects it.
	perItemType, perItemItems := perItemCoercionTypes(state.Parsed.Node, perItemParam)

	client := s.resolveClient(state)

	// results carries one slot per input id. A slot is written only by the
	// worker processing that index; slots can stay zero-valued if the
	// worker exits early — either on the first auth failure (firstErr is
	// set so we bail before assembling) or when the parent ctx is cancelled
	// (we explicitly surface ctx.Err() after wg.Wait). Both paths avoid
	// presenting a partially-populated response as success.
	results := make([]batchItem, len(ids))
	jobs := make(chan int, len(ids))
	var (
		wg       sync.WaitGroup
		authMu   sync.Mutex
		firstErr error
	)
	fanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := batchConcurrency
	if workers > len(ids) {
		workers = len(ids)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if fanCtx.Err() != nil {
					return
				}
				params := make(map[string]any, len(shared)+1)
				for k, v := range shared {
					params[k] = v
				}
				// Run the per-item value through coerceValue so it matches
				// the wire shape the single-command path uses (string vs
				// number etc. — determined by the MCP schema).
				params[perItemParam] = coerceValue(strconv.FormatInt(ids[idx], 10), perItemType, perItemItems)
				resp, callErr := client.CallTool(fanCtx, toolName, params)
				if callErr != nil {
					if meerrors.IsUnauthorized(callErr) {
						authMu.Lock()
						if firstErr == nil {
							firstErr = callErr
							cancel()
						}
						authMu.Unlock()
						return
					}
					results[idx] = batchItem{ID: ids[idx], Err: callErr}
					continue
				}
				var data any
				if resp != nil {
					data = resp.Data
				}
				results[idx] = batchItem{ID: ids[idx], Data: data}
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	authMu.Lock()
	terminal := firstErr
	authMu.Unlock()
	if terminal != nil {
		if me, ok := terminal.(*meerrors.MeegleError); ok && s.CommandsFunc != nil {
			me.Message = sanitizeToolNames(me.Message, s.CommandsFunc())
		}
		return terminal
	}
	// Parent ctx cancellation: workers may have returned early without
	// populating their slots, so assembling would present zero-valued
	// batchItem{} rows as spurious successes. Surface the cancellation
	// instead of fabricating a response.
	if err := ctx.Err(); err != nil {
		return err
	}

	state.Result = &executor.RawResult{
		Data: assembleBatchResponse(results, perItemParam, s.CommandsFunc),
	}
	return nil
}

// resolveClient returns either the test-injected factory's client or a fresh
// client built from the session state written by SessionStep.
func (s *BatchExecutorStep) resolveClient(state *pipeline.PipelineContext) mcpToolCaller {
	if s.clientFactory != nil {
		return s.clientFactory(state)
	}
	return mcpClientAdapter{inner: newMcpClientFromState(state)}
}

// mcpClientAdapter wraps *mcpclient.Client and narrows its CallTool return
// type to mcpResponse so tests can substitute an in-memory fake.
type mcpClientAdapter struct {
	inner *mcpclient.Client
}

func (a mcpClientAdapter) CallTool(ctx context.Context, name string, params map[string]any) (*mcpResponse, error) {
	resp, err := a.inner.CallTool(ctx, name, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return &mcpResponse{Data: resp.Data}, nil
}

// batchItem is the intermediate per-id result held during fan-out.
type batchItem struct {
	ID   int64
	Data any
	Err  error
}

// assembleBatchResponse turns the internal per-id slice into the public
// JSON-serialisable structure: {results, errors, summary}.
func assembleBatchResponse(items []batchItem, perItemParam string, cmdFn func() []types.MappedCommand) map[string]any {
	results := make([]map[string]any, 0, len(items))
	errs := make([]map[string]any, 0)
	succeeded := 0
	for _, it := range items {
		if it.Err != nil {
			code, message := classifyBatchError(it.Err)
			if cmdFn != nil {
				message = sanitizeToolNames(message, cmdFn())
			}
			errs = append(errs, map[string]any{
				perItemParam: it.ID,
				"code":       code,
				"message":    message,
			})
			continue
		}
		results = append(results, map[string]any{
			perItemParam: it.ID,
			"data":       it.Data,
		})
		succeeded++
	}
	return map[string]any{
		"results": results,
		"errors":  errs,
		"summary": map[string]any{
			"total":     len(items),
			"succeeded": succeeded,
			"failed":    len(items) - succeeded,
		},
	}
}

// perItemCoercionTypes reads the per-item param's MCP type (and optional
// items type for arrays) from the inherited mcp_param_types / mcp_param_items
// tags on the batch node. Missing tags produce empty strings, which
// coerceValue treats as "pass through as-is".
func perItemCoercionTypes(node *registry.CommandNode, perItemParam string) (string, string) {
	if node == nil || node.Meta.Tags == nil {
		return "", ""
	}
	kebab := strings.ReplaceAll(perItemParam, "_", "-")
	var paramTypes map[string]string
	var paramItems map[string]string
	if raw, ok := node.Meta.Tags["mcp_param_types"]; ok {
		_ = json.Unmarshal([]byte(raw), &paramTypes)
	}
	if raw, ok := node.Meta.Tags["mcp_param_items"]; ok {
		_ = json.Unmarshal([]byte(raw), &paramItems)
	}
	return paramTypes[kebab], paramItems[kebab]
}

func classifyBatchError(err error) (code, message string) {
	if me, ok := err.(*meerrors.MeegleError); ok {
		return me.Code, me.Message
	}
	return "EXECUTION_ERROR", err.Error()
}

// reshapeBatchForNDJSON flattens the batch response {results, errors,
// summary} payload into a flat []any so the generic ndjson formatter
// emits one line per item — successes first, then errors, then a final
// `{"summary": {...}}` line. Returns (nil, false) when data is not a
// recognised batch payload.
//
// `json` and `table` (the other two values in the `--format` enum)
// render the raw {results, errors, summary} object as-is; `table` in
// particular shows a 3-row key/value view with JSON blobs, so use
// `--format json` or pipe ndjson into `jq` for any richer layout.
func reshapeBatchForNDJSON(data any) (any, bool) {
	payload, ok := data.(map[string]any)
	if !ok {
		return nil, false
	}
	rawResults, hasResults := payload["results"]
	rawErrors, hasErrors := payload["errors"]
	if !hasResults || !hasErrors {
		return nil, false
	}

	results := toMapSlice(rawResults)
	errs := toMapSlice(rawErrors)

	out := make([]any, 0, len(results)+len(errs)+1)
	for _, r := range results {
		out = append(out, r)
	}
	for _, e := range errs {
		out = append(out, e)
	}
	if summary, ok := payload["summary"]; ok {
		out = append(out, map[string]any{"summary": summary})
	}
	return out, true
}

// toMapSlice coerces []map[string]any / []any to []map[string]any.
// Returns an empty slice when the input cannot be interpreted as a row
// collection — callers treat nil/empty uniformly.
func toMapSlice(v any) []map[string]any {
	switch val := v.(type) {
	case []map[string]any:
		return val
	case []any:
		out := make([]map[string]any, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// batchSharedParams builds snake-case MCP params the same way
// McpExecutorStep.Execute does: iterates state.Values, sends only flags the
// user explicitly set, skips the CLI-only IDs flags, and coerces values
// using the sibling's inherited mcp_param_types / mcp_param_items tags.
func batchSharedParams(state *pipeline.PipelineContext) map[string]any {
	params := map[string]any{}
	bc := batchCommandForNode(state.Parsed.Node)
	if bc == nil {
		return params
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}

	skip := map[string]bool{bc.IDsFlag: true}
	if bc.IDsFileFlag != "" {
		skip[bc.IDsFileFlag] = true
	}
	// The per-item kebab name cannot appear in a batch node's flags, but if
	// someone ever adds it as an alias it must not be forwarded as a shared
	// value — defensively skip.
	skip[strings.ReplaceAll(bc.PerItemParam, "_", "-")] = true

	var paramTypes map[string]string
	var paramItems map[string]string
	if tags := state.Parsed.Node.Meta.Tags; tags != nil {
		if raw, ok := tags["mcp_param_types"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramTypes)
		}
		if raw, ok := tags["mcp_param_items"]; ok {
			_ = json.Unmarshal([]byte(raw), &paramItems)
		}
	}

	explicit := make(map[string]bool, len(state.Parsed.ExplicitFlags))
	for k := range state.Parsed.ExplicitFlags {
		if isMeegleRuntimeFlag(k) {
			continue
		}
		explicit[k] = true
	}

	for k, v := range state.Values {
		if !explicit[k] || skip[k] {
			continue
		}
		snakeKey := strings.ReplaceAll(k, "-", "_")
		params[snakeKey] = coerceValue(v, paramTypes[k], paramItems[k])
	}

	// Inject mcp_fixed_params (user-explicit flags already present win).
	// Mirrors McpExecutorStep.Execute so batch and single paths stay
	// byte-identical for sibling commands that carry fixed params.
	if tags := state.Parsed.Node.Meta.Tags; tags != nil {
		if raw, ok := tags["mcp_fixed_params"]; ok {
			var fixed map[string]any
			if json.Unmarshal([]byte(raw), &fixed) == nil {
				for k, v := range fixed {
					if _, exists := params[k]; !exists {
						params[k] = v
					}
				}
			}
		}
	}
	return params
}

// batchCommandForNode resolves a CommandNode back to its static batchCommand
// entry (by Group/Name). Returns nil if the node is not a registered batch.
func batchCommandForNode(node *registry.CommandNode) *batchCommand {
	if node == nil {
		return nil
	}
	path := node.FullPath()
	if len(path) < 2 {
		return nil
	}
	for i := range batchCommands {
		if batchCommands[i].Group == path[0] && batchCommands[i].Name == path[1] {
			return &batchCommands[i]
		}
	}
	return nil
}

// collectBatchIDs gathers, validates, and deduplicates IDs from the batch
// IDs flag and the optional file flag. Client errors describe exactly which
// constraint failed (missing, non-numeric, over the cap).
func collectBatchIDs(state *pipeline.PipelineContext) ([]int64, error) {
	bc := batchCommandForNode(state.Parsed.Node)
	if bc == nil {
		return nil, meerrors.NewClientError("CLIENT_MISCONFIGURED",
			"batch command wiring is missing")
	}

	var raw []string
	raw = append(raw, flattenStringSliceFlag(state.Parsed.Flags[bc.IDsFlag])...)
	if bc.IDsFileFlag != "" {
		if path, _ := state.Parsed.Flags[bc.IDsFileFlag].(string); strings.TrimSpace(path) != "" {
			tokens, err := parseIDsFromFile(path)
			if err != nil {
				return nil, err
			}
			raw = append(raw, tokens...)
		}
	}

	if len(raw) == 0 {
		return nil, missingIDsError(bc)
	}

	seen := make(map[int64]struct{}, len(raw))
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, meerrors.NewClientError("CLIENT_INVALID_PARAM",
				fmt.Sprintf("invalid --%s value %q: must be an integer", bc.IDsFlag, s))
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		ids = append(ids, n)
	}
	if len(ids) == 0 {
		return nil, missingIDsError(bc)
	}
	if len(ids) > maxBatchIDs {
		return nil, meerrors.NewClientError("CLIENT_INVALID_PARAM",
			fmt.Sprintf("too many IDs: %d > %d (cap)", len(ids), maxBatchIDs)).
			WithSuggestion("split the input or use `search` for broader queries")
	}
	return ids, nil
}

func missingIDsError(bc *batchCommand) *meerrors.MeegleError {
	hint := fmt.Sprintf("meegle %s %s --%s <id>[,<id>...]", bc.Group, bc.Name, bc.IDsFlag)
	var msg string
	if bc.IDsFileFlag != "" {
		msg = fmt.Sprintf("missing required parameter: --%s or --%s", bc.IDsFlag, bc.IDsFileFlag)
	} else {
		msg = fmt.Sprintf("missing required parameter: --%s", bc.IDsFlag)
	}
	return meerrors.NewClientError("CLIENT_MISSING_REQUIRED", msg).WithSuggestion(hint)
}

// flattenStringSliceFlag normalises a cobra StringSlice value (or the
// fallback []any / string produced by router.ParseInput) to a flat string
// slice, splitting comma-separated entries.
func flattenStringSliceFlag(v any) []string {
	switch val := v.(type) {
	case []string:
		return splitCSV(val)
	case []any:
		strs := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
			} else if item != nil {
				strs = append(strs, fmt.Sprint(item))
			}
		}
		return splitCSV(strs)
	case string:
		return splitCSV([]string{val})
	}
	return nil
}

func splitCSV(in []string) []string {
	var out []string
	for _, s := range in {
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// parseIDsFromFile reads IDs from path. Format: one-per-line, comma-
// separated, or mixed. '#' starts a line comment; empty lines are ignored.
// UTF-8 BOM at the start of the file is stripped.
func parseIDsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, meerrors.NewClientError("CLIENT_INVALID_PARAM",
			fmt.Sprintf("failed to open --ids-file %q: %s", path, err))
	}
	defer f.Close()

	var tokens []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		for _, part := range strings.Split(line, ",") {
			if part = strings.TrimSpace(part); part != "" {
				tokens = append(tokens, part)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, meerrors.NewClientError("CLIENT_INVALID_PARAM",
			fmt.Sprintf("failed to read --ids-file %q: %s", path, err))
	}
	return tokens, nil
}
