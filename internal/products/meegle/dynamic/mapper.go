// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package dynamic

import "github.com/larksuite/meegle-cli/internal/products/meegle/types"

type fallbackEntry struct {
	resource    string
	method      string
	hasFields   bool
	description string
}

var fallbackTable = map[string]fallbackEntry{
	// workitem — core operations (9)
	"create_workitem":     {resource: "workitem", method: "create", hasFields: true, description: "Create work item (query meta-fields and meta-roles first)"},
	"get_workitem_brief":  {resource: "workitem", method: "get", description: "Get work item brief; pass --fields for custom fields"},
	"update_field":        {resource: "workitem", method: "update", hasFields: true, description: "Update work item fields or roles (not node fields)"},
	"update_node":         {resource: "workflow", method: "update-node", hasFields: true, description: "Update node-level fields (schedule/points/owners); cannot update multiple categories at once"},
	"update_node_subtask": {resource: "subtask", method: "update", hasFields: true, description: "Create, update, complete, or rollback a subtask"},
	"search_by_mql":       {resource: "workitem", method: "query", description: "Query work items by MQL expression"},
	"transition_node":     {resource: "workflow", method: "transition", description: "Transition workflow node (node-flow work items only)"},
	"transition_state":    {resource: "workflow", method: "transition-state", description: "Transition state-flow work item state"},
	"add_comment":         {resource: "comment", method: "add", description: "Add comment"},
	"get_node_detail":     {resource: "workflow", method: "get-node", description: "Get workflow node details including subtasks and custom fields"},

	// workitem — related queries list- (7)
	"list_workitem_comments":        {resource: "comment", method: "list", description: "List work item comments"},
	"list_workitem_relations":       {resource: "relation", method: "meta-definitions", description: "List relation definitions"},
	"list_related_workitem":         {resource: "relation", method: "list", description: "List work items linked by a relation field"},
	"get_workitem_man_hour_records": {resource: "workhour", method: "list-records", description: "List work hour records"},
	"get_workitem_op_record":        {resource: "workitem", method: "list-op-records", description: "List work item operation records"},
	"get_transitable_states":        {resource: "workflow", method: "list-state-transitions", description: "List available state transitions (state-flow work items only)"},
	"get_transition_required":       {resource: "workflow", method: "list-state-required", description: "List required fields for node or state transition"},

	// workitem — metadata meta- (5)
	"list_workitem_types":        {resource: "workitem", method: "meta-types", description: "List work item types"},
	"get_workitem_field_meta":    {resource: "workitem", method: "meta-create-fields", description: "Get required field metadata for creating work items"},
	"list_workitem_field_config": {resource: "workitem", method: "meta-fields", description: "List work item field configuration (excludes disabled fields and roles)"},
	"list_workitem_role_config":  {resource: "workitem", method: "meta-roles", description: "List work item role configuration for validating role keys"},
	"list_node_field_config":     {resource: "workflow", method: "meta-node-fields", description: "List node field configuration"},

	// mywork (1)
	"list_todo": {resource: "mywork", method: "todo", description: "List my work items by action (todo/done/overdue/this_week)"},

	// view (4)
	"create_fixed_view":    {resource: "view", method: "create-fixed", description: "Create fixed view"},
	"get_view_detail":      {resource: "view", method: "get", description: "List work items under a view by view ID"},
	"update_fixed_view":    {resource: "view", method: "update-fixed", description: "Update fixed view"},
	"search_view_by_title": {resource: "view", method: "search", description: "Search views by title to resolve view_id"},

	// chart (2)
	"get_chart_detail": {resource: "chart", method: "get", description: "Get chart details"},
	"list_charts":      {resource: "chart", method: "list", description: "List charts"},

	// team (2)
	"list_project_team": {resource: "team", method: "list", description: "List space teams"},
	"list_team_members": {resource: "team", method: "list-members", description: "List team members"},

	// schedule (1)
	"list_schedule": {resource: "workhour", method: "list-schedule", description: "List schedule and workload details (max 20 users, 3-month span per call)"},

	// project (1)
	"search_project_info": {resource: "project", method: "search", description: "Resolve space name to project_key or verify space existence"},

	// user (1)
	"search_user_info": {resource: "user", method: "search", description: "Resolve user name/email to user_key"},

	// attachment (2)
	"upload_file":      {resource: "attachment", method: "prepare-upload", description: "Preprocess an attachment upload — returns the signed URL + multipart plan"},
	"get_download_url": {resource: "attachment", method: "prepare-download", description: "Preprocess an attachment download — returns the signed URL + multipart plan"},
}

func MapTool(tool types.ToolDefinition) types.MappedCommand {
	if tool.Metadata != nil && tool.Metadata.Resource != "" && tool.Metadata.Method != "" {
		return types.MappedCommand{
			Resource: tool.Metadata.Resource, Method: tool.Metadata.Method,
			ToolName: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
		}
	}
	if entry, ok := fallbackTable[tool.Name]; ok {
		desc := tool.Description
		if entry.description != "" {
			desc = entry.description
		}
		return types.MappedCommand{
			Resource: entry.resource, Method: entry.method,
			ToolName: tool.Name, Description: desc,
			Parameters: tool.Parameters, HasFields: entry.hasFields,
		}
	}
	// Unmapped tools are not exposed
	return types.MappedCommand{}
}

func MapTools(tools []types.ToolDefinition) []types.MappedCommand {
	var cmds []types.MappedCommand
	for _, t := range tools {
		cmd := MapTool(t)
		if cmd.ToolName == "" {
			continue
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}
