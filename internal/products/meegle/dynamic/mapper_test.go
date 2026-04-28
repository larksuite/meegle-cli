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
	// view (4)
	{"create_fixed_view", "view", "create-fixed"},
	{"get_view_detail", "view", "get"},
	{"update_fixed_view", "view", "update-fixed"},
	{"search_view_by_title", "view", "search"},
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
	if len(fallbackTable) != 36 {
		t.Errorf("expected 36 fallback entries, got %d", len(fallbackTable))
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
