// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package dynamic

import (
	"sort"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

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

	// view (5)
	"create_fixed_view":                 {resource: "view", method: "create-fixed", description: "Create fixed view"},
	"get_view_detail":                   {resource: "view", method: "get", description: "List work items under a view by view ID"},
	"update_fixed_view":                 {resource: "view", method: "update-fixed", description: "Update fixed view"},
	"search_view_by_title":              {resource: "view", method: "search", description: "Search views by title to resolve view_id"},
	"list_multi_project_view_workitems": {resource: "view", method: "list-multi-project-workitems", description: "List work items the caller can access under a multi-project (panoramic) view; pass --view-id from a multiProjectView URL"},

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

	// deliverable (1)
	"list_deliverables": {resource: "deliverable", method: "list", description: "List deliverables with their root and source work items"},

	// resource — resource library (2)
	"create_resource_work_item":        {resource: "resource", method: "create", hasFields: true, description: "Create a resource template (resource instance) under a resource-library-enabled work item type"},
	"get_resource_work_item_type_conf": {resource: "resource", method: "meta-fields", description: "List resource library configuration (resource fields and roles)"},

	// wbs — plan tables (8)
	"list_wbs_draft_rows":              {resource: "wbs", method: "list-draft-rows", description: "List rows in a WBS draft, filtered by query and projected to selected fields"},
	"list_wbs_instance_rows":           {resource: "wbs", method: "list-instance-rows", description: "List rows in a published WBS instance, filtered by query and projected to selected fields"},
	"edit_wbs_draft":                   {resource: "wbs", method: "edit-draft", description: "Apply one atomic operation to a single WBS draft row (add/delete/restore/sort/rename/owner/schedule); operation type via --params"},
	"create_wbs_draft":                 {resource: "wbs", method: "create-draft", description: "Create a new WBS draft for a work item instance"},
	"publish_wbs_draft":                {resource: "wbs", method: "publish-draft", description: "Publish a WBS draft online"},
	"reset_wbs_draft":                  {resource: "wbs", method: "reset-draft", description: "Reset a WBS draft to match the published instance, discarding unpublished changes"},
	"get_wbs_draft_operation_progress": {resource: "wbs", method: "get-draft-progress", description: "Get the execution progress of a WBS draft operation (create/edit/publish)"},
	"list_element_template":            {resource: "wbs", method: "list-element-templates", description: "List element templates (resource nodes and tasks) from the flow resource library"},
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

// FallbackResources returns the sorted, de-duplicated set of resource (command
// group) names known to the static fallback table. It is the authoritative list
// of business domains the CLI can map offline, so callers that need to enumerate
// every domain without a live tool list (e.g. the discovery-failure placeholder
// tree) should derive it from here rather than hand-maintaining a parallel list.
func FallbackResources() []string {
	seen := make(map[string]struct{}, len(fallbackTable))
	resources := make([]string, 0, len(fallbackTable))
	for _, entry := range fallbackTable {
		if _, ok := seen[entry.resource]; ok {
			continue
		}
		seen[entry.resource] = struct{}{}
		resources = append(resources, entry.resource)
	}
	sort.Strings(resources)
	return resources
}
