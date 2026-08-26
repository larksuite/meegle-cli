// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"strings"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

const (
	TagExecutorKind = "executor_kind"

	ExecutorKindMCP    = "mcp"
	ExecutorKindCLIAPI = "cliapi"

	handlerHandoffAvailability = "meegle.local.ai-handoff.availability"
	handlerHandoffCreateLink   = "meegle.local.ai-handoff.create-link"
	handlerPreferenceAuto      = "meegle.local.preference.handoff.auto"
	handlerPreferenceAsk       = "meegle.local.preference.handoff.ask"
	handlerPreferenceOff       = "meegle.local.preference.handoff.off"
)

func NewMeegleLocalSetup() registry.RegistrySetup {
	return registry.NewStaticSetup(newMeegleLocalCommandTree())
}

func newMeegleLocalCommandTree() *registry.CommandTree {
	return &registry.CommandTree{
		Version: "meegle-local-v1",
		Nodes: []*registry.CommandNode{
			{
				Name: "ai-handoff",
				Help: registry.HelpText{
					Brief: "Hand work off to the AI assistant",
					Long: "Check whether AI handoff is currently available, then create a link with an optional typed Meegle context. " +
						"Availability is an optional preflight; create-link always performs an authoritative server-side check. " +
						"Set MEEGLE_AI_HANDOFF=disabled to hard-disable both operations in the local installation.",
				},
				Children: []*registry.CommandNode{
					{
						Name:       "availability",
						HandlerRef: handlerHandoffAvailability,
						Help: registry.HelpText{
							Brief: "Check current AI handoff availability without creating a link",
							Long: "Optional preflight that checks the business switch, rollout, AI entitlement, personal preference, and link-service readiness. " +
								"The result contains available and the negotiated query/context limits; it may contain mode, or reject_code and reject_msg when unavailable. " +
								"Successful snapshots are cached per profile for up to 1 hour. create-link does not trust this cache and always rechecks the server. " +
								"When MEEGLE_AI_HANDOFF=disabled, the CLI returns available=false with reject_code=LOCAL_DISABLED without requiring authentication, reading the cache, or calling the Handoff API.",
							Examples: []registry.Example{
								{
									Description: "Check availability and negotiated limits",
									Command:     "meegle ai-handoff availability --format json",
								},
							},
						},
						Meta: localNodeMeta(ExecutorKindCLIAPI, "read"),
					},
					{
						Name:       "create-link",
						HandlerRef: handlerHandoffCreateLink,
						Help: registry.HelpText{
							Brief: "Create an AI assistant link from a query and optional Meegle context",
							Long: "--query is required and must contain non-empty text after trimming. --related-context is optional and repeatable; " +
								"each occurrence accepts a strict JSON object or array. Unknown fields, trailing JSON, mismatched type/payload pairs, and missing required identifiers are rejected.\n\n" +
								"Context types and payloads:\n" +
								"  1 Project       project(project_key)\n" +
								"  2 Reserved      WorkItemType is currently unsupported; do not use\n" +
								"  3 WorkItem      work_item(project_key, work_item_type_key, work_item_id)\n" +
								"  4 View          view(project_key, view_id[, work_item_type_key])\n" +
								"  5 MeasureChart  measure_chart(project_key, chart_id)\n\n" +
								"The limits returned by availability are advisory for request preparation; create-link always applies the authoritative server-side policy and limits. " +
								"On success, the CLI replaces the returned URL host with the current login host while preserving its scheme, path, query, and fragment. " +
								"When MEEGLE_AI_HANDOFF=disabled, a valid invocation returns available=false with reject_code=LOCAL_DISABLED without requiring authentication or calling the Handoff API.",
							Examples: []registry.Example{
								{
									Description: "Create a link from query text only",
									Command:     "meegle ai-handoff create-link --query 'Summarize the risks and propose next actions'",
								},
								{
									Description: "Create a link with a WorkItem context",
									Command:     `meegle ai-handoff create-link --query "Summarize this work item" --related-context '{"type":3,"work_item":{"project_key":"PROJ","work_item_type_key":"story","work_item_id":"123"}}'`,
								},
							},
						},
						Flags: []registry.FlagDef{
							{
								Name:        "query",
								Type:        registry.FlagTypeString,
								Required:    true,
								Description: "Non-empty text to prefill; maximum UTF-8 bytes are reported by availability.limits.max_query_bytes",
							},
							{
								Name:        "related-context",
								Type:        registry.FlagTypeStringArray,
								Description: "Strict typed JSON object or array; repeatable, with the item limit reported by availability.limits.max_related_context_items",
							},
						},
						Meta: localNodeMeta(ExecutorKindCLIAPI, "write"),
					},
				},
			},
			{
				Name: "preference",
				Help: registry.HelpText{Brief: "Manage personal CLI preferences"},
				Children: []*registry.CommandNode{
					{
						Name: "handoff",
						Help: registry.HelpText{
							Brief: "Manage AI handoff recommendations",
							Long: "Set the personal handoff recommendation mode. These commands take no project or tenant parameter. " +
								"A successful update invalidates the local CLI configuration cache; run 'meegle ai-handoff availability' to read the effective mode. " +
								"The MVP supports auto, ask, and off; status and reset are not exposed.",
						},
						Children: []*registry.CommandNode{
							{
								Name:       "auto",
								HandlerRef: handlerPreferenceAuto,
								Help: registry.HelpText{
									Brief: "Automatically show AI handoff recommendations",
									Long:  "Set the personal handoff recommendation mode to auto and invalidate the local CLI configuration cache.",
									Examples: []registry.Example{{
										Command: "meegle preference handoff auto",
									}},
								},
								Meta: localNodeMeta(ExecutorKindCLIAPI, "write"),
							},
							{
								Name:       "ask",
								HandlerRef: handlerPreferenceAsk,
								Help: registry.HelpText{
									Brief: "Ask before showing an AI handoff recommendation",
									Long:  "Set the personal handoff recommendation mode to ask and invalidate the local CLI configuration cache.",
									Examples: []registry.Example{{
										Command: "meegle preference handoff ask",
									}},
								},
								Meta: localNodeMeta(ExecutorKindCLIAPI, "write"),
							},
							{
								Name:       "off",
								HandlerRef: handlerPreferenceOff,
								Help: registry.HelpText{
									Brief: "Disable AI handoff recommendations",
									Long:  "Set the personal handoff recommendation mode to off and invalidate the local CLI configuration cache.",
									Examples: []registry.Example{{
										Command: "meegle preference handoff off",
									}},
								},
								Meta: localNodeMeta(ExecutorKindCLIAPI, "write"),
							},
						},
					},
				},
			},
		},
	}
}

// localMappedCommands derives inspect metadata from the same registry tree
// used by routing so local command help and inspect output cannot drift.
func localMappedCommands() []types.MappedCommand {
	tree := newMeegleLocalCommandTree()
	commands := make([]types.MappedCommand, 0)
	for _, node := range tree.Nodes {
		appendLocalMappedCommands(&commands, nil, node)
	}
	return commands
}

func appendLocalMappedCommands(commands *[]types.MappedCommand, parentPath []string, node *registry.CommandNode) {
	if node == nil {
		return
	}
	path := append(append([]string(nil), parentPath...), node.Name)
	if node.IsExecutable() && len(path) >= 2 {
		parameters := make([]types.ToolParameter, 0, len(node.Flags))
		for _, flag := range node.Flags {
			parameter := types.ToolParameter{
				Name:        flag.Name,
				Type:        inspectParameterType(flag.Type),
				Description: flag.Description,
				Required:    flag.Required,
			}
			if parameter.Type == "array" {
				parameter.Items = &types.ParameterItems{Type: inspectParameterItemType(flag.Type)}
			}
			parameters = append(parameters, parameter)
		}
		*commands = append(*commands, types.MappedCommand{
			Resource:    path[0],
			Method:      strings.Join(path[1:], " "),
			ToolName:    node.HandlerRef,
			Description: node.Help.Brief,
			Parameters:  parameters,
		})
	}
	for _, child := range node.Children {
		appendLocalMappedCommands(commands, path, child)
	}
}

func inspectParameterType(flagType string) string {
	switch flagType {
	case registry.FlagTypeInt, registry.FlagTypeFloat:
		return "number"
	case registry.FlagTypeBool:
		return "boolean"
	case registry.FlagTypeStringSlice, registry.FlagTypeStringArray, registry.FlagTypeIntSlice, registry.FlagTypeObjectSlice:
		return "array"
	case registry.FlagTypeObject:
		return "object"
	default:
		return "string"
	}
}

func inspectParameterItemType(flagType string) string {
	switch flagType {
	case registry.FlagTypeIntSlice:
		return "number"
	case registry.FlagTypeObjectSlice:
		return "object"
	default:
		return "string"
	}
}

func localNodeMeta(kind, risk string) registry.NodeMeta {
	return registry.NodeMeta{
		Source: "static",
		Risk:   risk,
		Tags:   map[string]string{TagExecutorKind: kind},
	}
}
