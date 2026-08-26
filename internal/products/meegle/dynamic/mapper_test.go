// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package dynamic

import (
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

func TestMapToolMetadata(t *testing.T) {
	tool := types.ToolDefinition{
		Name:     "some_tool",
		Metadata: &types.ToolMetadata{Resource: "custom", Method: "action"},
	}
	cmd := MapTool(tool)
	if cmd.Resource != "custom" || cmd.Method != "action" {
		t.Errorf("expected custom/action, got %s/%s", cmd.Resource, cmd.Method)
	}
}

func TestMapToolKnownFallbackCannotBeRemappedByServerMetadata(t *testing.T) {
	tool := types.ToolDefinition{
		Name:        "get_workitem_brief",
		Description: "server supplied description",
		Metadata:    &types.ToolMetadata{Resource: "auth", Method: "status"},
	}
	cmd := MapTool(tool)
	if cmd.Resource != "workitem" || cmd.Method != "get" {
		t.Fatalf("known tool mapped to %s/%s, want immutable fallback workitem/get", cmd.Resource, cmd.Method)
	}
}

func TestMapToolUnmappedReturnsEmpty(t *testing.T) {
	tool := types.ToolDefinition{Name: "unknown_foo_bar"}
	cmd := MapTool(tool)
	if cmd.ToolName != "" {
		t.Errorf("expected empty ToolName for unmapped tool, got %q", cmd.ToolName)
	}
}

var fallbackTests = []struct {
	toolName string
	resource string
	method   string
}{
	// workitem — core operations (9)
	{"create_workitem", "workitem", "create"},
	{"get_workitem_brief", "workitem", "get"},
	{"update_field", "workitem", "update"},
	{"update_node", "workflow", "update-node"},
	{"update_node_subtask", "subtask", "update"},
	{"search_by_mql", "workitem", "query"},
	{"transition_node", "workflow", "transition"},
	{"transition_state", "workflow", "transition-state"},
	{"add_comment", "comment", "add"},
	{"get_node_detail", "workflow", "get-node"},
	// workitem — related queries list- (7)
	{"list_workitem_comments", "comment", "list"},
	{"list_workitem_relations", "relation", "meta-definitions"},
	{"list_related_workitem", "relation", "list"},
	{"get_workitem_man_hour_records", "workhour", "list-records"},
	{"get_workitem_op_record", "workitem", "list-op-records"},
	{"get_transitable_states", "workflow", "list-state-transitions"},
	{"get_transition_required", "workflow", "list-state-required"},
	// workitem — metadata meta- (5)
	{"list_workitem_types", "workitem", "meta-types"},
	{"get_workitem_field_meta", "workitem", "meta-create-fields"},
	{"list_workitem_field_config", "workitem", "meta-fields"},
	{"list_workitem_role_config", "workitem", "meta-roles"},
	{"list_node_field_config", "workflow", "meta-node-fields"},
	// mywork (1)
	{"list_todo", "mywork", "todo"},
	// view (5)
	{"create_fixed_view", "view", "create-fixed"},
	{"get_view_detail", "view", "get"},
	{"update_fixed_view", "view", "update-fixed"},
	{"search_view_by_title", "view", "search"},
	{"list_multi_project_view_workitems", "view", "list-multi-project-workitems"},
	// chart (2)
	{"get_chart_detail", "chart", "get"},
	{"list_charts", "chart", "list"},
	// team (2)
	{"list_project_team", "team", "list"},
	{"list_team_members", "team", "list-members"},
	// schedule (1)
	{"list_schedule", "workhour", "list-schedule"},
	// project (1)
	{"search_project_info", "project", "search"},
	// user (1)
	{"search_user_info", "user", "search"},
	// deliverable (1)
	{"list_deliverables", "deliverable", "list"},
	// resource (2)
	{"create_resource_work_item", "resource", "create"},
	{"get_resource_work_item_type_conf", "resource", "meta-fields"},
	// wbs (8)
	{"list_wbs_draft_rows", "wbs", "list-draft-rows"},
	{"list_wbs_instance_rows", "wbs", "list-instance-rows"},
	{"edit_wbs_draft", "wbs", "edit-draft"},
	{"create_wbs_draft", "wbs", "create-draft"},
	{"publish_wbs_draft", "wbs", "publish-draft"},
	{"reset_wbs_draft", "wbs", "reset-draft"},
	{"get_wbs_draft_operation_progress", "wbs", "get-draft-progress"},
	{"list_element_template", "wbs", "list-element-templates"},
}

func TestFallbackTable(t *testing.T) {
	for _, tt := range fallbackTests {
		t.Run(tt.toolName, func(t *testing.T) {
			tool := types.ToolDefinition{Name: tt.toolName}
			cmd := MapTool(tool)
			if cmd.Resource != tt.resource {
				t.Errorf("resource: expected %s, got %s", tt.resource, cmd.Resource)
			}
			if cmd.Method != tt.method {
				t.Errorf("method: expected %s, got %s", tt.method, cmd.Method)
			}
		})
	}
}

func TestFallbackTableCount(t *testing.T) {
	if len(fallbackTable) != 48 {
		t.Errorf("expected 48 fallback entries, got %d", len(fallbackTable))
	}
}

func TestMapTools(t *testing.T) {
	tools := []types.ToolDefinition{{Name: "create_workitem"}, {Name: "list_charts"}}
	cmds := MapTools(tools)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
}

func TestMapToolsFiltersUnmapped(t *testing.T) {
	tools := []types.ToolDefinition{
		{Name: "create_workitem"},
		{Name: "totally_unknown_tool"},
		{Name: "list_charts"},
	}
	cmds := MapTools(tools)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands (unmapped filtered), got %d", len(cmds))
	}
}

func TestFallbackDescriptionOverride(t *testing.T) {
	tool := types.ToolDefinition{
		Name:        "create_workitem",
		Description: "MCP original description",
	}
	cmd := MapTool(tool)
	if cmd.Description != "Create work item (query meta-fields and meta-roles first)" {
		t.Errorf("expected fallback description, got %q", cmd.Description)
	}
}

func TestFallbackDescriptionAllPresent(t *testing.T) {
	for name, entry := range fallbackTable {
		if entry.description == "" {
			t.Errorf("fallback entry %q has empty description", name)
		}
	}
}

func TestUnmappedToolReturnsEmptyDescription(t *testing.T) {
	tool := types.ToolDefinition{
		Name:        "unknown_tool_xyz",
		Description: "MCP original description",
	}
	cmd := MapTool(tool)
	if cmd.Description != "" {
		t.Errorf("expected empty description for unmapped tool, got %q", cmd.Description)
	}
}

func TestFallbackResources(t *testing.T) {
	got := FallbackResources()

	// Strictly sorted and de-duplicated.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("FallbackResources not strictly sorted/unique: %v", got)
		}
	}

	// Must cover exactly the distinct resources in the fallback table.
	want := map[string]struct{}{}
	for _, entry := range fallbackTable {
		want[entry.resource] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("FallbackResources len = %d, want %d distinct resources", len(got), len(want))
	}
	for _, r := range got {
		if _, ok := want[r]; !ok {
			t.Errorf("unexpected resource %q", r)
		}
	}
	// "resource" is a real domain that was historically easy to miss.
	if _, ok := want["resource"]; !ok {
		t.Fatal("fallback table missing resource domain")
	}
}
