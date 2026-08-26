// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import platformapi "github.com/larksuite/meegle-cli/extension/platform"

// commandRiskByTool is the CLI-owned risk directory for MCP tools known by the
// built-in mapper. A newly exposed tool is intentionally unannotated until it
// is reviewed here; active Restrict rules then deny it by default.
var commandRiskByTool = map[string]platformapi.Risk{
	"get_workitem_brief":                platformapi.RiskRead,
	"search_by_mql":                     platformapi.RiskRead,
	"get_node_detail":                   platformapi.RiskRead,
	"list_workitem_comments":            platformapi.RiskRead,
	"list_workitem_relations":           platformapi.RiskRead,
	"list_related_workitem":             platformapi.RiskRead,
	"get_workitem_man_hour_records":     platformapi.RiskRead,
	"get_workitem_op_record":            platformapi.RiskRead,
	"get_transitable_states":            platformapi.RiskRead,
	"get_transition_required":           platformapi.RiskRead,
	"list_workitem_types":               platformapi.RiskRead,
	"get_workitem_field_meta":           platformapi.RiskRead,
	"list_workitem_field_config":        platformapi.RiskRead,
	"list_workitem_role_config":         platformapi.RiskRead,
	"list_node_field_config":            platformapi.RiskRead,
	"list_todo":                         platformapi.RiskRead,
	"get_view_detail":                   platformapi.RiskRead,
	"search_view_by_title":              platformapi.RiskRead,
	"list_multi_project_view_workitems": platformapi.RiskRead,
	"get_chart_detail":                  platformapi.RiskRead,
	"list_charts":                       platformapi.RiskRead,
	"list_project_team":                 platformapi.RiskRead,
	"list_team_members":                 platformapi.RiskRead,
	"list_schedule":                     platformapi.RiskRead,
	"search_project_info":               platformapi.RiskRead,
	"search_user_info":                  platformapi.RiskRead,
	"get_download_url":                  platformapi.RiskRead,
	"list_deliverables":                 platformapi.RiskRead,
	"get_resource_work_item_type_conf":  platformapi.RiskRead,
	"list_wbs_draft_rows":               platformapi.RiskRead,
	"list_wbs_instance_rows":            platformapi.RiskRead,
	"get_wbs_draft_operation_progress":  platformapi.RiskRead,
	"list_element_template":             platformapi.RiskRead,
	"create_workitem":                   platformapi.RiskWrite,
	"update_field":                      platformapi.RiskWrite,
	"update_node":                       platformapi.RiskWrite,
	"update_node_subtask":               platformapi.RiskWrite,
	"transition_node":                   platformapi.RiskWrite,
	"transition_state":                  platformapi.RiskWrite,
	"add_comment":                       platformapi.RiskWrite,
	"create_fixed_view":                 platformapi.RiskWrite,
	"update_fixed_view":                 platformapi.RiskWrite,
	"upload_file":                       platformapi.RiskWrite,
	"create_resource_work_item":         platformapi.RiskWrite,
	"edit_wbs_draft":                    platformapi.RiskWrite,
	"create_wbs_draft":                  platformapi.RiskWrite,
	"publish_wbs_draft":                 platformapi.RiskHighRiskWrite,
	"reset_wbs_draft":                   platformapi.RiskHighRiskWrite,
}

func riskForTool(toolName string) string { return commandRiskByTool[toolName].String() }
