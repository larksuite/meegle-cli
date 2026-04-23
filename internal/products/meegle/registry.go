// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/discovery"
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// resourceDescriptions provides a description for each resource group.
var resourceDescriptions = map[string]string{
	"workitem": "Work Item domain — CRUD, field metadata, time tracking, comments",
	"workflow": "Workflow domain — node transitions, state transitions, node fields",
	"subtask":  "Subtask domain — subtask operations",
	"comment":  "Comment domain — cross-entity commenting",
	"workhour": "Work Hour domain — time tracking and scheduling",
	"relation": "Relation domain — work item relationships",
	"mywork":   "My Work domain — cross-space personal to-do/done items",
	"view":     "View domain — create, query, update views",
	"chart":    "Chart domain — chart queries",
	"team":     "Team domain — space members, team queries",
	"user":     "User domain — user information search",
	"project":  "Project domain — project space queries",
}

// DynamicRegistrySetup builds the command tree via MCP dynamic discovery.
type DynamicRegistrySetup struct {
	client       discovery.ToolLister
	cache        *ToolCache
	tokenManager *auth.TokenManager
	source       IdentitySource
	globalFlags  []registry.FlagDef
	commands     []types.MappedCommand // populated by Setup, read by pipeline steps
	authFailed   bool                  // set when tool discovery fails due to 401 (expired/revoked token)
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

// WithIdentitySource records which source the active token came from so the
// 401 degradation path can surface a source-specific rotation hint via the
// root command's "Not logged in" note instead of hard-failing --help / auth.
func WithIdentitySource(src IdentitySource) RegistryOption {
	return func(s *DynamicRegistrySetup) {
		s.source = src
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
	tools, err := s.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	commands := dynamic.MapTools(tools)
	s.commands = commands
	tree := buildCommandTree(commands)
	tree.GlobalFlags = s.globalFlags
	return tree, nil
}

// MappedCommands returns the commands from the most recent Setup call.
func (s *DynamicRegistrySetup) MappedCommands() []types.MappedCommand {
	return s.commands
}

// AuthFailed reports whether the last Setup call treated the user as
// unauthenticated because discovery returned a terminal 401.
func (s *DynamicRegistrySetup) AuthFailed() bool {
	return s.authFailed
}

// resolveTools fetches the tool list by priority: cache -> dynamic discovery.
// Returns an error when the client is configured but discovery fails.
// Returns (nil, nil) when no client is configured (graceful degradation for CLI not-logged-in).
func (s *DynamicRegistrySetup) resolveTools(ctx context.Context) ([]types.ToolDefinition, error) {
	// 1. Try cache
	if s.cache != nil {
		result, _ := s.cache.Get()
		if result != nil && len(result.Tools) > 0 {
			return result.Tools, nil
		}
	}

	// 2. Try dynamic discovery
	if s.client != nil {
		tools, err := discovery.DiscoverTools(ctx, s.client)
		if err != nil {
			// CLI path: a terminal 401 (stale/revoked token, refresh exhausted)
			// must not break --help, `auth login`, or other offline commands.
			// For all three CLI sources (Store/Env/Config) we degrade silently
			// to "not logged in" and rely on the root command's note to tell
			// the user which knob to rotate. SDK callers (SourceUnset with an
			// injected client) need the 401 to surface so they can react.
			if isUnauthorizedErr(err) && s.source != SourceUnset {
				// ClearToken only when we actually own the credential — env
				// and config tokens must be rotated by the user out-of-band.
				if s.source == SourceStore && s.tokenManager != nil {
					_ = s.tokenManager.ClearToken()
				}
				s.authFailed = true
				return nil, nil
			}
			if isUnauthorizedErr(err) {
				return nil, meerrors.NewClientError("AUTH_REJECTED",
					"token rejected by server during tool discovery").
					WithSuggestion(meerrors.HintConfigTokenRejected)
			}
			return nil, fmt.Errorf("tool discovery failed: %w", err)
		}
		if len(tools) == 0 {
			return nil, fmt.Errorf("tool discovery returned empty list")
		}
		// Write to cache (ignore errors)
		if s.cache != nil {
			_ = s.cache.Set(tools)
		}
		return tools, nil
	}

	// 3. No client configured (e.g. CLI not logged in): graceful degradation
	return nil, nil
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
				Meta:       registry.NodeMeta{Tags: tags},
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
				Meta:       registry.NodeMeta{Tags: tags},
			})
			break
		}
	}
}
