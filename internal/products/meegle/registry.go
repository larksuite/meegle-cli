// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/discovery"
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

var dynamicCommandNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

var reservedDynamicResources = buildReservedDynamicResources()

// buildReservedDynamicResources protects every root command owned by the CLI.
// Commands installed by the root customizer remain explicit, while roots from
// the local registry are derived so a future local command cannot be shadowed
// by an untrusted tools/list entry and make CompositeSetup reject the whole App.
func buildReservedDynamicResources() map[string]struct{} {
	resources := map[string]struct{}{
		"auth": {}, "completion": {}, "config": {}, "extension": {},
		"help": {}, "inspect": {}, "url": {}, "version": {},
	}
	if tree := newMeegleLocalCommandTree(); tree != nil {
		for _, node := range tree.Nodes {
			if node == nil {
				continue
			}
			for _, name := range append([]string{node.Name}, node.Aliases...) {
				if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
					resources[key] = struct{}{}
				}
			}
		}
	}
	return resources
}

var reservedDynamicFlagNames = map[string]struct{}{
	"help": {}, "version": {},
}

// ToolMappingIssue describes one tools/list entry that was deliberately
// isolated from the command tree. It contains only public tool/path metadata
// and must never include credentials or request headers.
type ToolMappingIssue struct {
	Code     string
	ToolName string
	Path     string
}

// resourceDescriptions provides optional help text for each resource group.
// It is description-only metadata: lookups fall back to a generated default
// when a group is absent, so it must NOT be treated as the authoritative list
// of domains (that list comes from dynamic.FallbackResources).
var resourceDescriptions = map[string]string{
	"workitem":    "Work Item domain — CRUD, field metadata, time tracking, comments",
	"workflow":    "Workflow domain — node transitions, state transitions, node fields",
	"subtask":     "Subtask domain — subtask operations",
	"comment":     "Comment domain — cross-entity commenting",
	"workhour":    "Work Hour domain — time tracking and scheduling",
	"relation":    "Relation domain — work item relationships",
	"mywork":      "My Work domain — cross-space personal to-do/done items",
	"view":        "View domain — create, query, update views",
	"chart":       "Chart domain — chart queries",
	"team":        "Team domain — space members, team queries",
	"user":        "User domain — user information search",
	"project":     "Project domain — project space queries",
	"attachment":  "Attachment domain — upload/download files via Lark Project attachment protocol",
	"deliverable": "Deliverable domain — deliverable listing and related work item context",
	"wbs":         "WBS domain — plan-table draft and instance workflows",
	"resource":    "Resource domain — resource-library template (resource instance) operations",
}

const (
	discoveryFailureHandlerRef      = "__meegle_tool_discovery_failed__"
	tagDiscoveryFailure             = "meegle_discovery_failure"
	tagConfigResolutionFailure      = "meegle_config_resolution_failure"
	tagRouterAllowUnknownFlags      = "router_allow_unknown_flags"
	discoveryFailureFallbackVersion = "dynamic-discovery-failed"
)

// DynamicRegistrySetup builds the command tree via MCP dynamic discovery.
type DynamicRegistrySetup struct {
	mu                       sync.RWMutex
	rebuildMu                sync.Mutex
	client                   discovery.ToolLister
	cache                    *ToolCache
	tokenManager             *auth.TokenManager
	activeToken              string
	source                   IdentitySource
	globalFlags              []registry.FlagDef
	commands                 []types.MappedCommand // populated by Setup, read by pipeline steps
	mappingIssues            []ToolMappingIssue
	authFailed               bool // set when tool discovery fails due to 401 (expired/revoked token)
	forceRefresh             bool // when true, bypass fresh cache and fetch from server
	degradeDiscoveryFailures bool // CLI startup only: keep static commands bootable on non-auth discovery errors
	bootstrapError           error
}

// RegistryOption configures optional parameters for DynamicRegistrySetup.
type RegistryOption func(*DynamicRegistrySetup)

// WithGlobalFlags injects global flags.
func WithGlobalFlags(flags []registry.FlagDef) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.globalFlags = flags
	}
}

// WithTokenManager injects a TokenManager so resolveTools can clear an
// invalid token when discovery returns a terminal 401.
func WithTokenManager(tm *auth.TokenManager) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.tokenManager = tm
	}
}

// WithActiveToken records the token used to construct the discovery client.
// A terminal 401 may clear the store only if this exact token is still there.
func WithActiveToken(token string) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.activeToken = token
	}
}

// WithIdentitySource records which source the active token came from so the
// 401 degradation path can surface a source-specific rotation hint via the
// root command's "Not logged in" note instead of hard-failing --help / auth.
func WithIdentitySource(src IdentitySource) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.source = src
	}
}

// WithForceRefresh, when true, makes resolveTools skip the fresh-cache
// shortcut and pull from the server. Stale cache is still kept as a
// fallback for non-auth discovery errors so offline users do not lose
// their last-known command set.
func WithForceRefresh(force bool) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.forceRefresh = force
	}
}

// WithDiscoveryFailureDegradation keeps CLI bootstrap alive when the server is
// unreachable and no cache exists. SDK callers should keep the default hard
// failure so programmatic clients can handle discovery errors explicitly.
func WithDiscoveryFailureDegradation(enabled bool) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.degradeDiscoveryFailures = enabled
	}
}

// WithBootstrapError preserves a deferred CLI bootstrap error so business
// domains remain routable even when no discovery cache exists. Static recovery
// commands still boot normally.
func WithBootstrapError(err error) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.bootstrapError = err
	}
}

// IdentitySource reports the source of the active token (for external
// callers that want to render source-specific hints after Setup runs).
func (s *DynamicRegistrySetup) IdentitySource() IdentitySource {
	return s.source
}

// NewDynamicRegistrySetup constructs a DynamicRegistrySetup.
// Both client and cache may be nil (graceful degradation).
func NewDynamicRegistrySetup(client discovery.ToolLister, cache *ToolCache, opts ...RegistryOption) *DynamicRegistrySetup {
	s := &DynamicRegistrySetup{client: client, cache: cache}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Setup implements the registry.RegistrySetup interface.
// Priority: cache -> dynamic discovery -> graceful degradation (no client).
// Returns an error when a client is configured but tool discovery fails,
// so callers (especially SDK mode) get a clear signal instead of an empty tree.
func (s *DynamicRegistrySetup) Setup(ctx context.Context) (*registry.CommandTree, error) {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	if s.bootstrapError != nil {
		tree := buildConfigResolutionFailureTree(s.bootstrapError)
		tree.GlobalFlags = s.globalFlags
		s.publishSnapshot(nil, nil, false)
		return tree, nil
	}

	tools, authFailed, err := s.resolveToolsState(ctx)
	if err != nil {
		if s.degradeDiscoveryFailures && !isUnauthorizedErr(err) {
			tree := buildDiscoveryFailureTree(err)
			tree.GlobalFlags = s.globalFlags
			s.publishSnapshot(nil, nil, false)
			return tree, nil
		}
		return nil, err
	}
	commands, issues := mapAndSanitizeTools(tools, s.globalFlags)
	tree := buildCommandTree(commands)
	tree.GlobalFlags = s.globalFlags
	s.publishSnapshot(commands, issues, authFailed)
	return tree, nil
}

func buildConfigResolutionFailureTree(err error) *registry.CommandTree {
	tree := buildDiscoveryFailureTree(err)
	for _, node := range tree.Nodes {
		if node == nil {
			continue
		}
		if node.Meta.Tags == nil {
			node.Meta.Tags = make(map[string]string)
		}
		node.Meta.Tags[tagConfigResolutionFailure] = "1"
	}
	return tree
}

func (s *DynamicRegistrySetup) publishSnapshot(commands []types.MappedCommand, issues []ToolMappingIssue, authFailed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = cloneMappedCommands(commands)
	s.mappingIssues = append([]ToolMappingIssue(nil), issues...)
	s.authFailed = authFailed
}

func cloneMappedCommands(commands []types.MappedCommand) []types.MappedCommand {
	if commands == nil {
		return nil
	}
	cloned := make([]types.MappedCommand, len(commands))
	for commandIndex, command := range commands {
		cloned[commandIndex] = command
		if command.Parameters == nil {
			continue
		}
		cloned[commandIndex].Parameters = make([]types.ToolParameter, len(command.Parameters))
		for parameterIndex, parameter := range command.Parameters {
			cloned[commandIndex].Parameters[parameterIndex] = parameter
			if parameter.Items != nil {
				items := *parameter.Items
				cloned[commandIndex].Parameters[parameterIndex].Items = &items
			}
		}
	}
	return cloned
}

func mapAndSanitizeTools(tools []types.ToolDefinition, globalFlags []registry.FlagDef) ([]types.MappedCommand, []ToolMappingIssue) {
	var wireIssues []ToolMappingIssue
	for _, tool := range tools {
		if tool.Issue != nil {
			wireIssues = append(wireIssues, ToolMappingIssue{
				Code: tool.Issue.Code, ToolName: tool.Name, Path: tool.Issue.Path,
			})
			continue
		}
		if dynamic.IsFallbackTool(tool.Name) {
			continue
		}
		if tool.Metadata == nil {
			wireIssues = append(wireIssues, ToolMappingIssue{Code: "missing_mapping", ToolName: tool.Name})
			continue
		}
		if strings.TrimSpace(tool.Metadata.Resource) == "" || strings.TrimSpace(tool.Metadata.Method) == "" {
			wireIssues = append(wireIssues, ToolMappingIssue{
				Code: "invalid_tool_definition", ToolName: tool.Name,
				Path: strings.Trim(tool.Metadata.Resource+"/"+tool.Metadata.Method, "/"),
			})
		}
	}
	commands, mappingIssues := sanitizeMappedCommands(dynamic.MapTools(tools), globalFlags)
	return commands, append(wireIssues, mappingIssues...)
}

// MappedCommands returns the commands from the most recent Setup call.
func (s *DynamicRegistrySetup) MappedCommands() []types.MappedCommand {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMappedCommands(s.commands)
}

// MappingIssues returns deterministic diagnostics for tools/list entries that
// were rejected without making the complete dynamic registry unavailable.
func (s *DynamicRegistrySetup) MappingIssues() []ToolMappingIssue {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ToolMappingIssue(nil), s.mappingIssues...)
}

func sanitizeMappedCommands(commands []types.MappedCommand, globalFlags []registry.FlagDef) ([]types.MappedCommand, []ToolMappingIssue) {
	accepted := make([]types.MappedCommand, 0, len(commands))
	pathIndex := make(map[string]int, len(commands))
	var issues []ToolMappingIssue
	issue := func(code string, command types.MappedCommand) {
		issues = append(issues, ToolMappingIssue{
			Code: code, ToolName: command.ToolName,
			Path: strings.Trim(command.Resource+"/"+command.Method, "/"),
		})
	}

	for _, command := range commands {
		if _, reserved := reservedDynamicResources[command.Resource]; reserved {
			issue("reserved_path", command)
			continue
		}
		if owner, reserved := dynamic.FallbackToolForPath(command.Resource, command.Method); reserved && owner != command.ToolName {
			issue("reserved_path", command)
			continue
		}
		if !dynamicCommandNamePattern.MatchString(command.Resource) || !dynamicCommandNamePattern.MatchString(command.Method) {
			issue("invalid_command", command)
			continue
		}
		command.Description = sanitizeHelpText(command.Description)
		if command.Description == "" {
			command.Description = "Run dynamic tool"
		}
		command.Parameters = append([]types.ToolParameter(nil), command.Parameters...)
		for index := range command.Parameters {
			command.Parameters[index].Description = sanitizeHelpText(command.Parameters[index].Description)
		}
		parametersValid := true
		for _, parameter := range command.Parameters {
			if parameter.Name == "url" {
				continue
			}
			flagName := toFlagName(parameter.Name)
			if !dynamicCommandNamePattern.MatchString(flagName) {
				parametersValid = false
				break
			}
			if _, reserved := reservedDynamicFlagNames[flagName]; reserved {
				parametersValid = false
				break
			}
			for _, globalFlag := range globalFlags {
				if strings.EqualFold(flagName, globalFlag.Name) {
					parametersValid = false
					break
				}
			}
			if !parametersValid {
				break
			}
		}
		if !parametersValid {
			issue("invalid_command", command)
			continue
		}

		// Validate each untrusted entry independently before it can poison the
		// aggregate tree. Cross-entry path conflicts are handled below.
		candidate := buildCommandTree([]types.MappedCommand{command})
		if validation := registry.ValidateTree(candidate); validation.HasErrors() {
			issue("invalid_command", command)
			continue
		}

		path := command.Resource + "/" + command.Method
		if _, exists := pathIndex[path]; exists {
			issue("duplicate_path", command)
			continue
		}
		pathIndex[path] = len(accepted)
		accepted = append(accepted, command)
	}
	return accepted, issues
}

// AuthFailed reports whether the last Setup call treated the user as
// unauthenticated because discovery returned a terminal 401.
func (s *DynamicRegistrySetup) AuthFailed() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authFailed
}

func sanitizeHelpText(value string) string {
	value = ansiEscapePattern.ReplaceAllString(value, "")
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

// resolveTools fetches the tool list with these rules:
//  1. Fresh cache (within TTL) → return immediately, skip discovery.
//  2. Stale cache or --refresh → try discovery; on success overwrite cache.
//  3. Discovery fails with non-auth error and stale cache exists → fall back
//     to stale tools so offline users keep their last-known command set.
//  4. Discovery fails with 401 → existing graceful-degradation path.
//  5. No client and no usable cache → (nil, nil) for not-logged-in CLI.
func (s *DynamicRegistrySetup) resolveTools(ctx context.Context) ([]types.ToolDefinition, error) {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	tools, _, err := s.resolveToolsState(ctx)
	return tools, err
}

func (s *DynamicRegistrySetup) resolveToolsState(ctx context.Context) ([]types.ToolDefinition, bool, error) {
	// 1. Read cache; remember stale entry as fallback for offline use.
	var staleTools []types.ToolDefinition
	if s.cache != nil {
		result, _ := s.cache.Get()
		if result != nil && len(result.Tools) > 0 {
			if !result.Stale && !s.forceRefresh {
				return result.Tools, false, nil
			}
			staleTools = result.Tools
		}
	}

	// 2. Try dynamic discovery.
	if s.client != nil {
		tools, err := discovery.DiscoverTools(ctx, s.client)
		if err != nil {
			// CLI path: a terminal 401 (stale/revoked token, refresh exhausted)
			// must not break --help, `auth login`, or other offline commands.
			// For all three CLI sources (Store/Env/Config) we degrade silently
			// to "not logged in" and rely on the root command's note to tell
			// the user which knob to rotate. SDK callers (SourceUnset with an
			// injected client) need the 401 to surface so they can react.
			if meerrors.IsUnauthorized(err) && s.source != SourceUnset {
				// Clear only a store token that is still the one rejected by
				// this process. Another process may already have refreshed it.
				// Env/config tokens remain caller-owned and are never cleared.
				if s.source == SourceStore && s.tokenManager != nil {
					_, _ = s.tokenManager.ClearTokenIfCurrent(s.activeToken)
				}
				return nil, true, nil
			}
			if meerrors.IsUnauthorized(err) {
				return nil, false, meerrors.NewClientError("AUTH_REJECTED",
					"token rejected by server during tool discovery").
					WithSuggestion(meerrors.HintConfigTokenRejected)
			}
			// Non-auth error: prefer stale cache over hard failure so that a
			// transient outage does not turn into "no commands available".
			if staleTools != nil {
				fmt.Fprintf(os.Stderr,
					"[meegle] using stale command cache (server unreachable: %v)\n", err)
				return staleTools, false, nil
			}
			return nil, false, fmt.Errorf("tool discovery failed: %w", err)
		}
		if len(tools) == 0 {
			if staleTools != nil {
				return staleTools, false, nil
			}
			return nil, false, fmt.Errorf("tool discovery returned empty list")
		}
		if s.cache != nil {
			_ = s.cache.Set(tools)
		}
		return tools, false, nil
	}

	// 3. No client (offline / not logged in): keep stale cache if we have it.
	if staleTools != nil {
		return staleTools, false, nil
	}
	return nil, false, nil
}

func buildDiscoveryFailureTree(err error) *registry.CommandTree {
	// Derive the domain list from the static fallback table — the authoritative
	// set of business domains the CLI knows — so every real domain gets a clean
	// TOOL_DISCOVERY_FAILED placeholder. resourceDescriptions only supplies help
	// text and must NOT gate which domains are covered (a domain missing from it
	// previously fell through to "unknown command").
	groupNames := dynamic.FallbackResources()

	nodes := make([]*registry.CommandNode, 0, len(groupNames))
	errText := strings.TrimSpace(fmt.Sprint(err))
	for _, name := range groupNames {
		desc := resourceDescriptions[name]
		if desc == "" {
			desc = fmt.Sprintf("%s domain", name)
		}
		nodes = append(nodes, &registry.CommandNode{
			Name: name,
			Help: registry.HelpText{
				Brief: desc,
				Long:  "Dynamic Meegle commands are unavailable because tool discovery failed.",
			},
			Args: []registry.ArgDef{{
				Name:     "command-and-flags",
				Variadic: true,
			}},
			HandlerRef: discoveryFailureHandlerRef,
			Meta: registry.NodeMeta{
				Hidden: true,
				Tags: map[string]string{
					tagDiscoveryFailure:        errText,
					tagRouterAllowUnknownFlags: "1",
				},
			},
		})
	}
	return &registry.CommandTree{
		Version: discoveryFailureFallbackVersion,
		Nodes:   nodes,
	}
}

// isUnauthorizedErr reports whether err is a terminal auth error from
// mcpclient — either a raw 401 or the AUTH_EXPIRED sentinel returned after
// a failed refresh attempt.
func isUnauthorizedErr(err error) bool {
	var me *meerrors.MeegleError
	if !stderrors.As(err, &me) {
		return false
	}
	if me.HTTPStatus == 401 {
		return true
	}
	return me.Code == "AUTH_EXPIRED"
}

// buildCommandTree converts a MappedCommand list into a CommandTree.
// Groups by resource as first-level CommandNode, method as second-level CommandNode.
func buildCommandTree(commands []types.MappedCommand) *registry.CommandTree {
	// Group by resource
	groups := make(map[string][]types.MappedCommand)
	for _, cmd := range commands {
		groups[cmd.Resource] = append(groups[cmd.Resource], cmd)
	}

	// Sort group names
	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	nodes := make([]*registry.CommandNode, 0, len(groupNames))
	for _, groupName := range groupNames {
		cmds := groups[groupName]
		// Sort children by method
		sort.Slice(cmds, func(i, j int) bool {
			return cmds[i].Method < cmds[j].Method
		})

		children := make([]*registry.CommandNode, 0, len(cmds))
		for _, cmd := range cmds {
			// Store original MCP parameter type info for McpExecutorStep type conversion
			paramTypes := make(map[string]string) // flag-name -> original MCP type
			paramItems := make(map[string]string) // flag-name -> items.type (array only)
			for _, p := range cmd.Parameters {
				if p.Name == "url" {
					continue
				}
				kebab := strings.ReplaceAll(p.Name, "_", "-")
				paramTypes[kebab] = p.Type
				if p.Type == "array" && p.Items != nil && p.Items.Type != "" {
					paramItems[kebab] = p.Items.Type
				}
			}
			tags := make(map[string]string)
			if len(paramTypes) > 0 {
				typesJSON, _ := json.Marshal(paramTypes)
				tags["mcp_param_types"] = string(typesJSON)
			}
			if len(paramItems) > 0 {
				itemsJSON, _ := json.Marshal(paramItems)
				tags["mcp_param_items"] = string(itemsJSON)
			}

			children = append(children, &registry.CommandNode{
				Name: cmd.Method,
				Help: registry.HelpText{
					Brief: buildShortDesc(cmd),
				},
				Flags:      convertParameters(cmd.Parameters),
				HandlerRef: cmd.ToolName,
				Meta: registry.NodeMeta{
					CommandID: "mcp:" + cmd.ToolName,
					Source:    "mcp",
					Risk:      riskForTool(cmd.ToolName),
					ToolName:  cmd.ToolName,
					Tags:      tags,
				},
			})
		}

		desc := resourceDescriptions[groupName]
		if desc == "" {
			desc = fmt.Sprintf("%s domain", groupName)
		}
		nodes = append(nodes, &registry.CommandNode{
			Name: groupName,
			Help: registry.HelpText{
				Brief: desc,
			},
			Children: children,
		})
	}

	// Inject sugar commands
	injectSugarCommands(nodes)
	// Inject the attachment +upload-entire / +download-entire shortcuts under
	// the MCP-discovered attachment group. The basic prepare-upload /
	// prepare-download commands themselves come from the MCP path via
	// dynamic.fallbackTable, so this only adds the scenario shortcuts.
	nodes = injectAttachmentCommands(nodes)
	// Inject batch commands (one CLI command calling an MCP tool N times).
	injectBatchCommands(nodes)

	return &registry.CommandTree{
		Version: "dynamic",
		Nodes:   nodes,
	}
}

// convertParameters converts a ToolParameter list to a FlagDef list.
// Skips parameters with name="url".
func convertParameters(params []types.ToolParameter) []registry.FlagDef {
	var flags []registry.FlagDef
	for _, p := range params {
		if p.Name == "url" {
			continue
		}
		flags = append(flags, registry.FlagDef{
			Name:        toFlagName(p.Name),
			Type:        mapParamType(p.Type),
			Required:    p.Required,
			Description: p.Description,
		})
	}
	return flags
}

// mapParamType maps MCP parameter types to framework FlagDef types.
func mapParamType(paramType string) string {
	switch paramType {
	case "string":
		return registry.FlagTypeString
	case "number":
		return registry.FlagTypeString // Product layer conversion
	case "boolean":
		return registry.FlagTypeBool
	case "array":
		// StringArray (not StringSlice) so JSON values aren't mis-parsed by
		// cobra's CSV splitter — each `--flag value` is stored verbatim as
		// one element, which is what callers expect for object-shaped
		// fields[] entries.
		return registry.FlagTypeStringArray
	case "object":
		return registry.FlagTypeString
	default:
		return registry.FlagTypeString
	}
}

// toFlagName converts underscore naming to kebab-case (work_item_id -> work-item-id).
func toFlagName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

// buildShortDesc builds a short description for a command.
// Format matches master: `"Description [--work-item-id*, --set]"`
func buildShortDesc(cmd types.MappedCommand) string {
	desc := cmd.Description

	var flagHints []string
	for _, p := range cmd.Parameters {
		if p.Name == "url" {
			continue
		}
		kebab := toFlagName(p.Name)
		if p.Required {
			flagHints = append(flagHints, "--"+kebab+"*")
		}
	}
	if cmd.HasFields {
		flagHints = append(flagHints, "--set")
	}
	if len(flagHints) > 0 {
		desc += " [" + strings.Join(flagHints, ", ") + "]"
	}

	return desc
}

// ResultTransformFunc performs custom transformation on MCP response data.
type ResultTransformFunc func(data any) any

// sugarResultTransforms stores result transform functions for sugar commands, indexed by "group/name".
// Initialized once at package init from the compile-time sugarCommands list — never mutated at runtime.
var sugarResultTransforms map[string]ResultTransformFunc

// sugarCommands defines the sugar commands to be injected.
var sugarCommands = []struct {
	Group           string              // Parent resource group name
	Name            string              // Subcommand name
	Brief           string              // Short description
	HandlerRef      string              // MCP tool name
	Flags           []registry.FlagDef  // User-provided parameters
	FixedParams     map[string]any      // Fixed injected parameters
	ResultTransform ResultTransformFunc // Custom result transformation
}{
	{
		Group:       "user",
		Name:        "me",
		Brief:       "View current logged-in user information",
		HandlerRef:  "search_user_info",
		Flags:       []registry.FlagDef{},
		FixedParams: map[string]any{"user_keys": []string{"current_login_user()"}},
		ResultTransform: func(data any) any {
			arr, ok := data.([]any)
			if !ok || len(arr) == 0 {
				return data
			}
			m, ok := arr[0].(map[string]any)
			if !ok {
				return arr[0]
			}
			return map[string]any{
				"user_key":   m["user_key"],
				"name_cn":    m["name_cn"],
				"name_en":    m["name_en"],
				"email":      m["email"],
				"avatar_url": m["avatar_url"],
			}
		},
	},
}

func init() {
	sugarResultTransforms = make(map[string]ResultTransformFunc, len(sugarCommands))
	for _, sugar := range sugarCommands {
		if sugar.ResultTransform != nil {
			sugarResultTransforms[sugar.Group+"/"+sugar.Name] = sugar.ResultTransform
		}
	}
}

// LookupResultTransform looks up the sugar command result transform function by command path.
func LookupResultTransform(fullPath []string) ResultTransformFunc {
	if len(fullPath) < 2 {
		return nil
	}
	key := fullPath[0] + "/" + fullPath[1]
	return sugarResultTransforms[key]
}

// injectSugarCommands injects sugar commands into existing command groups.
func injectSugarCommands(nodes []*registry.CommandNode) {
	for _, sugar := range sugarCommands {
		for _, node := range nodes {
			if node.Name != sugar.Group {
				continue
			}
			tags := make(map[string]string)
			if len(sugar.FixedParams) > 0 {
				b, _ := json.Marshal(sugar.FixedParams)
				tags["mcp_fixed_params"] = string(b)
			}
			node.Children = append(node.Children, &registry.CommandNode{
				Name:       sugar.Name,
				Help:       registry.HelpText{Brief: sugar.Brief},
				Flags:      sugar.Flags,
				HandlerRef: sugar.HandlerRef,
				Meta: registry.NodeMeta{
					CommandID: "sugar:" + sugar.Group + "/" + sugar.Name,
					Source:    "sugar",
					Risk:      riskForTool(sugar.HandlerRef),
					ToolName:  sugar.HandlerRef,
					Tags:      tags,
				},
			})
			break
		}
	}
}
