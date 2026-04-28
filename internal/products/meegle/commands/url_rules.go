// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

// urlRules maps path patterns to url_kind values. Rule order matters: the
// matcher uses first-match-wins, so more specific patterns must precede
// generic ones that would also match. Every pattern is derived from
// packages/platform/config/router/base.config.tsx and drawer.config.tsx in
// project_manager_fe — when that table changes, this one must follow.
//
// Conventions:
//   - Literal segments must match exactly.
//   - `:name` captures into DecodeResult via applyParam. Unknown names are
//     ignored, so you can add hints without changing the struct.
//   - There is no regex or alternation support — list alternatives as
//     separate rules.
var urlRules = []rule{
	// =============================================================
	// Reserved top-level single-segment paths. These MUST come before
	// any rule whose first segment is :simple_name, or a bare
	// `/workbench` would be parsed as a space named "workbench".
	// =============================================================
	{"/", "root", nil},
	{"/workbench", "workbench", nil},
	{"/workspaces", "workspaces", nil},
	{"/favorites", "favorites", nil},
	{"/inbox", "inbox", nil},
	{"/teams", "teams", nil},
	{"/list", "project_list", nil},
	{"/templates", "templates", nil},
	{"/tenant", "tenant_select", nil},
	{"/home", "home_ka", nil},
	{"/helpcenter", "helpcenter", nil},
	{"/openapp", "openapp", nil},
	{"/new-form", "quick_create_form", nil},
	{"/issue-trans", "issue_trans", nil},
	{"/jump-to-outer", "jump_to_outer", nil},
	{"/light-share", "light_share", nil},
	{"/channel-error", "channel_error", nil},
	{"/enterprise_manage", "enterprise_manage", nil},
	{"/fetch-cookie-by-login", "login_fetch_cookie", nil},
	{"/login-asset", "login_asset", nil},
	{"/switch-asset", "switch_asset", nil},

	// Teams
	{"/teams/empty", "teams", nil},
	{"/teams/detail/:team_id", "team_detail", nil},

	// Tenant
	{"/tenant/create", "tenant_create", nil},

	// Templates
	{"/templates/manage", "template_manage", nil},
	{"/templates/detail/:template_id", "template_detail", nil},

	// External quick entries
	{"/issue-create-open/use-case", "issue_create_open_usecase", nil},
	{"/story-create-open/:business_name", "story_create_open", nil},
	{"/aiApplication/ai_application_share", "ai_application_share", nil},

	// Global error pages
	{"/error-page/lark-page-404", "lark_page_404", nil},
	{"/error-page/project-empty", "project_empty_page", nil},
	{"/error-page/loading", "route_loading", nil},
	{"/error-page/system-upgrade", "system_upgrade", nil},

	// =============================================================
	// /b/* system-scope routes. Literal prefix, no conflict with
	// :simple_name routes.
	// =============================================================
	{"/b", "b_home", nil},
	{"/b/noAuth", "no_project_auth", nil},
	{"/b/preference", "preference", nil},
	{"/b/mcp", "mcp_config", nil},
	{"/b/mcp-config", "mcp_config", nil},
	{"/b/ai", "ai_hub", nil},
	{"/b/handover", "handover", nil},
	{"/b/login", "login_datacenter", nil},
	{"/b/price", "trial_price", nil},
	{"/b/trial/enable", "trial_enable", nil},
	{"/b/trial/new", "trial_new", nil},
	{"/b/onboarding/task", "onboarding_task", nil},
	{"/b/onboarding/operation-task", "onboarding_operation_task", nil},
	{"/b/onboarding/create-project-by-shared", "onboarding_create_shared", nil},
	{"/b/integration/slack/connect", "slack_connect", nil},
	{"/b/cross/fail", "cross_fail", nil},
	{"/b/cross/invite", "cross_invite", nil},
	{"/b/auth/mcp", "mcp_auth", nil},
	{"/b/auth/mcp-page", "mcp_auth", nil},
	{"/b/resource/:user", "resource_handover", nil},
	{"/b/register-meego-unbundled/result", "unbundled_register_result", nil},

	// =============================================================
	// Space-scoped routes (/:simple_name/*). List most-specific first
	// so preset segments (user-gantt, chart, …) win over the generic
	// :work_item_type slot.
	// =============================================================

	// Setting (multi-segment must come before single /setting)
	{"/:simple_name/setting/workObject/:setting_type", "setting_workobject", nil},
	{"/:simple_name/setting/workObjectSetting/advancedConfig", "setting_workobject_advanced", nil},
	{"/:simple_name/setting/userGroup/:setting_type", "setting_user_group_legacy", nil},
	{"/:simple_name/setting/userGroupList", "setting_user_group", nil},
	{"/:simple_name/setting/permission/:setting_type", "setting_permission", nil},
	{"/:simple_name/setting/permission_preview", "setting_permission_preview", nil},
	{"/:simple_name/setting/applyCrossProjectSync", "setting_apply_cross_sync", nil},
	// Wildcard fallback for any other setting subpage. Frontend declares
	// /:project/setting as a non-exact route and renders subpages via an
	// internal child router, so new pages (e.g. workObjectSetting root,
	// future settings) show up without a corresponding base.config entry.
	{"/:simple_name/setting/*", "setting_other", nil},
	{"/:simple_name/setting", "setting_home", nil},

	// Import / data
	{"/:simple_name/jira-import", "import_jira", nil},
	{"/:simple_name/import-ins-data", "import_excel", nil},
	{"/:simple_name/data-recycle-page", "data_recycle", nil},

	// Preset function homepages
	{"/:simple_name/user-gantt/homepage", "gantt_homepage", nil},
	{"/:simple_name/multi-project-view/homepage", "multi_project_homepage", nil},
	{"/:simple_name/chart/homepage", "chart_homepage", nil},
	{"/:simple_name/sub_task/homepage", "subtask_homepage", nil},
	{"/:simple_name/project-overview/homepage", "project_overview_homepage", nil},

	// Chart CRUD (specific before generic workitem)
	{"/:simple_name/chart/detail/:chart_id", "chart_detail", nil},
	{"/:simple_name/chart/create", "chart_create", nil},

	// Drawer-only chart helpers (same path shape exposed in drawer.config)
	{"/:simple_name/chartdatascope/detail/:chart_id", "chart_datascope", nil},
	{"/:simple_name/chartdatascopeV2/detail/:chart_id", "chart_datascope_v2", nil},
	{"/:simple_name/chartpenetrate/default/detail/:chart_id", "chart_penetrate", nil},
	{"/:simple_name/chartpenetrate/detail/:chart_id", "chart_penetrate", nil},
	{"/:simple_name/chartpenetrate/small/detail/:chart_id", "chart_penetrate_small", nil},
	{"/:simple_name/chartpenetrateV2/default/detail/:chart_id", "chart_penetrate_v2", nil},
	{"/:simple_name/chartpenetrateV2/detail/:chart_id", "chart_penetrate_v2", nil},
	{"/:simple_name/chartpenetrateV2/small/detail/:chart_id", "chart_penetrate_v2_small", nil},

	// Views
	{"/:simple_name/storyView/:view_id", "view_story", map[string]string{"work_item_type": "story"}},
	{"/:simple_name/issueView/:view_id", "view_issue", map[string]string{"work_item_type": "issue"}},
	{"/:simple_name/multiProjectView/:view_id", "view_multi_project", nil},
	{"/:simple_name/project-overview/:view_id", "view_project_overview", nil},
	{"/:simple_name/userGantt/:view_id", "view_user_gantt", nil},
	{"/:simple_name/workObjectView/chart/:view_id", "view_chart", nil},
	{"/:simple_name/workObjectView/:work_item_type/:view_id", "view_workitem", nil},

	// Plugin
	{"/:simple_name/meegoPlg/:plugin_key", "plugin_page", nil},

	// AI assist
	{"/:simple_name/ai-assist", "project_ai_assist", nil},

	// Workitem CRUD
	{"/:simple_name/:work_item_type/detail/:work_item_id", "workitem_detail", nil},
	{"/:simple_name/:work_item_type/create", "workitem_create", nil},
	{"/:simple_name/:work_item_type/draft", "workitem_draft", nil},
	{"/:simple_name/:work_item_type/homepage/:edit_str", "workitem_homepage_edit", nil},
	{"/:simple_name/:work_item_type/homepage", "workitem_homepage", nil},

	// Project overview & space root (overview has an edit-str variant)
	{"/:simple_name/overview/:edit_str", "project_overview_edit", nil},
	{"/:simple_name/overview", "project_overview", nil},
	{"/:simple_name/empty", "project_empty", nil},
	{"/:simple_name/404", "project_404", nil},
	{"/:simple_name/401", "project_401", nil},
	{"/:simple_name/500", "project_500", nil},
	{"/:simple_name", "project_home", nil},
}

// urlAliases normalises legacy paths (usually snake_case or camelCase
// variants that pre-date the current kebab-case routes) to their canonical
// form. The target path is looked up again in urlRules, so every target
// MUST correspond to an existing rule.
//
// NOTE: We deliberately do NOT alias bare `/:simple_name/:something`-style
// paths that the frontend redirects to `/homepage`. A stripped URL like
// that usually means the user pasted something incomplete (no view id, no
// detail id) and skills should ask them for the right link rather than
// guess.
var urlAliases = []alias{
	// Global snake_case → kebab-case
	{"/feishu_workbench_mobile", "/workbench"},
	{"/fetchCookieByLogin", "/fetch-cookie-by-login"},
	{"/loginAsset", "/login-asset"},
	{"/switchAsset", "/switch-asset"},
	{"/new_form", "/new-form"},
	{"/issue_trans", "/issue-trans"},
	{"/issue_create_open/use_case", "/issue-create-open/use-case"},
	{"/story_create_open/:business_name", "/story-create-open/:business_name"},
	{"/jumpToOuter", "/jump-to-outer"},
	{"/light_share", "/light-share"},

	// Space-scoped camelCase → kebab-case
	{"/:simple_name/jiraImport", "/:simple_name/jira-import"},
	{"/:simple_name/importInsData", "/:simple_name/import-ins-data"},
}
