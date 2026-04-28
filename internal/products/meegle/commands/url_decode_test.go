// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"testing"
)

// TestDecodeURL_KindCoverage walks every url_kind exposed by the rule
// table with at least one representative input. If a rule regresses (kind
// renamed, pattern removed) this test fails before the skills layer does.
func TestDecodeURL_KindCoverage(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		// Reserved top-level
		{"root", "https://meegle.com/", "root"},
		{"workbench", "https://meegle.com/workbench", "workbench"},
		{"workspaces", "https://meegle.com/workspaces", "workspaces"},
		{"favorites", "https://meegle.com/favorites", "favorites"},
		{"inbox", "https://meegle.com/inbox", "inbox"},
		{"teams", "https://meegle.com/teams", "teams"},
		{"teams_detail", "https://meegle.com/teams/detail/team_abc", "team_detail"},
		{"teams_empty", "https://meegle.com/teams/empty", "teams"},
		{"list", "https://meegle.com/list", "project_list"},
		{"templates", "https://meegle.com/templates", "templates"},
		{"template_detail", "https://meegle.com/templates/detail/tpl_42", "template_detail"},
		{"template_manage", "https://meegle.com/templates/manage", "template_manage"},
		{"tenant", "https://meegle.com/tenant", "tenant_select"},
		{"tenant_create", "https://meegle.com/tenant/create", "tenant_create"},
		{"home_ka", "https://meegle.com/home", "home_ka"},
		{"helpcenter", "https://meegle.com/helpcenter", "helpcenter"},
		{"openapp", "https://meegle.com/openapp", "openapp"},
		{"quick_create_form", "https://meegle.com/new-form", "quick_create_form"},
		{"issue_trans", "https://meegle.com/issue-trans", "issue_trans"},
		{"jump_to_outer", "https://meegle.com/jump-to-outer", "jump_to_outer"},
		{"light_share", "https://meegle.com/light-share", "light_share"},
		{"channel_error", "https://meegle.com/channel-error", "channel_error"},
		{"enterprise_manage", "https://meegle.com/enterprise_manage", "enterprise_manage"},
		{"login_fetch_cookie", "https://meegle.com/fetch-cookie-by-login", "login_fetch_cookie"},
		{"login_asset", "https://meegle.com/login-asset", "login_asset"},
		{"switch_asset", "https://meegle.com/switch-asset", "switch_asset"},
		{"issue_create_open_usecase", "https://meegle.com/issue-create-open/use-case", "issue_create_open_usecase"},
		{"story_create_open", "https://meegle.com/story-create-open/bn_42", "story_create_open"},
		{"ai_application_share", "https://meegle.com/aiApplication/ai_application_share", "ai_application_share"},

		// Error pages
		{"lark_page_404", "https://meegle.com/error-page/lark-page-404", "lark_page_404"},
		{"project_empty_page", "https://meegle.com/error-page/project-empty", "project_empty_page"},
		{"route_loading", "https://meegle.com/error-page/loading", "route_loading"},
		{"system_upgrade", "https://meegle.com/error-page/system-upgrade", "system_upgrade"},

		// /b/*
		{"b_home", "https://meegle.com/b", "b_home"},
		{"no_project_auth", "https://meegle.com/b/noAuth", "no_project_auth"},
		{"preference", "https://meegle.com/b/preference", "preference"},
		{"mcp_config", "https://meegle.com/b/mcp", "mcp_config"},
		{"mcp_config_legacy", "https://meegle.com/b/mcp-config", "mcp_config"},
		{"ai_hub", "https://meegle.com/b/ai", "ai_hub"},
		{"handover", "https://meegle.com/b/handover", "handover"},
		{"login_datacenter", "https://meegle.com/b/login", "login_datacenter"},
		{"trial_price", "https://meegle.com/b/price", "trial_price"},
		{"trial_enable", "https://meegle.com/b/trial/enable", "trial_enable"},
		{"trial_new", "https://meegle.com/b/trial/new", "trial_new"},
		{"onboarding_task", "https://meegle.com/b/onboarding/task", "onboarding_task"},
		{"onboarding_operation_task", "https://meegle.com/b/onboarding/operation-task", "onboarding_operation_task"},
		{"onboarding_create_shared", "https://meegle.com/b/onboarding/create-project-by-shared", "onboarding_create_shared"},
		{"slack_connect", "https://meegle.com/b/integration/slack/connect", "slack_connect"},
		{"cross_fail", "https://meegle.com/b/cross/fail", "cross_fail"},
		{"cross_invite", "https://meegle.com/b/cross/invite", "cross_invite"},
		{"mcp_auth", "https://meegle.com/b/auth/mcp", "mcp_auth"},
		{"mcp_auth_page", "https://meegle.com/b/auth/mcp-page", "mcp_auth"},
		{"resource_handover", "https://meegle.com/b/resource/u123", "resource_handover"},
		{"unbundled_register_result", "https://meegle.com/b/register-meego-unbundled/result", "unbundled_register_result"},

		// Setting
		{"setting_home", "https://meegle.com/xopenapp/setting", "setting_home"},
		{"setting_workobject", "https://meegle.com/xopenapp/setting/workObject/story", "setting_workobject"},
		{"setting_workobject_advanced", "https://meegle.com/xopenapp/setting/workObjectSetting/advancedConfig", "setting_workobject_advanced"},
		{"setting_user_group_legacy", "https://meegle.com/xopenapp/setting/userGroup/roleA", "setting_user_group_legacy"},
		{"setting_user_group", "https://meegle.com/xopenapp/setting/userGroupList", "setting_user_group"},
		{"setting_permission", "https://meegle.com/xopenapp/setting/permission/story", "setting_permission"},
		{"setting_permission_preview", "https://meegle.com/xopenapp/setting/permission_preview", "setting_permission_preview"},
		{"setting_apply_cross_sync", "https://meegle.com/xopenapp/setting/applyCrossProjectSync", "setting_apply_cross_sync"},

		// Import / data
		{"import_jira", "https://meegle.com/xopenapp/jira-import", "import_jira"},
		{"import_excel", "https://meegle.com/xopenapp/import-ins-data", "import_excel"},
		{"data_recycle", "https://meegle.com/xopenapp/data-recycle-page", "data_recycle"},

		// Preset function homepages
		{"gantt_homepage", "https://meegle.com/xopenapp/user-gantt/homepage", "gantt_homepage"},
		{"multi_project_homepage", "https://meegle.com/xopenapp/multi-project-view/homepage", "multi_project_homepage"},
		{"chart_homepage", "https://meegle.com/xopenapp/chart/homepage", "chart_homepage"},
		{"subtask_homepage", "https://meegle.com/xopenapp/sub_task/homepage", "subtask_homepage"},
		{"project_overview_homepage", "https://meegle.com/xopenapp/project-overview/homepage", "project_overview_homepage"},

		// Chart
		{"chart_detail", "https://meegle.com/xopenapp/chart/detail/c42", "chart_detail"},
		{"chart_create", "https://meegle.com/xopenapp/chart/create", "chart_create"},
		{"chart_datascope", "https://meegle.com/xopenapp/chartdatascope/detail/c42", "chart_datascope"},
		{"chart_datascope_v2", "https://meegle.com/xopenapp/chartdatascopeV2/detail/c42", "chart_datascope_v2"},
		{"chart_penetrate_default", "https://meegle.com/xopenapp/chartpenetrate/default/detail/c42", "chart_penetrate"},
		{"chart_penetrate_short", "https://meegle.com/xopenapp/chartpenetrate/detail/c42", "chart_penetrate"},
		{"chart_penetrate_small", "https://meegle.com/xopenapp/chartpenetrate/small/detail/c42", "chart_penetrate_small"},
		{"chart_penetrate_v2", "https://meegle.com/xopenapp/chartpenetrateV2/default/detail/c42", "chart_penetrate_v2"},
		{"chart_penetrate_v2_small", "https://meegle.com/xopenapp/chartpenetrateV2/small/detail/c42", "chart_penetrate_v2_small"},

		// Views
		{"view_story", "https://meegle.com/xopenapp/storyView/v1", "view_story"},
		{"view_issue", "https://meegle.com/xopenapp/issueView/v1", "view_issue"},
		{"view_multi_project", "https://meegle.com/xopenapp/multiProjectView/v1", "view_multi_project"},
		{"view_project_overview", "https://meegle.com/xopenapp/project-overview/v1", "view_project_overview"},
		{"view_user_gantt", "https://meegle.com/xopenapp/userGantt/v1", "view_user_gantt"},
		{"view_chart", "https://meegle.com/xopenapp/workObjectView/chart/v1", "view_chart"},
		{"view_workitem", "https://meegle.com/xopenapp/workObjectView/story/v1", "view_workitem"},

		// Plugin
		{"plugin_page", "https://meegle.com/xopenapp/meegoPlg/my_plg", "plugin_page"},

		// Project basics
		{"project_ai_assist", "https://meegle.com/xopenapp/ai-assist", "project_ai_assist"},
		{"project_home", "https://meegle.com/xopenapp", "project_home"},
		{"project_empty", "https://meegle.com/xopenapp/empty", "project_empty"},
		{"project_overview", "https://meegle.com/xopenapp/overview", "project_overview"},
		{"project_overview_edit", "https://meegle.com/xopenapp/overview/edit", "project_overview_edit"},
		{"project_404", "https://meegle.com/xopenapp/404", "project_404"},
		{"project_401", "https://meegle.com/xopenapp/401", "project_401"},
		{"project_500", "https://meegle.com/xopenapp/500", "project_500"},

		// Workitem
		{"workitem_detail", "https://meegle.com/xopenapp/story/detail/7092625364", "workitem_detail"},
		{"workitem_create", "https://meegle.com/xopenapp/story/create", "workitem_create"},
		{"workitem_draft", "https://meegle.com/xopenapp/story/draft", "workitem_draft"},
		{"workitem_homepage", "https://meegle.com/xopenapp/story/homepage", "workitem_homepage"},
		{"workitem_homepage_edit", "https://meegle.com/xopenapp/story/homepage/edit", "workitem_homepage_edit"},

		// Setting wildcard fallback — specific rules still win first, the
		// /:simple_name/setting/* catches anything the frontend renders
		// via its internal child router.
		{"setting_other_workobjectsetting", "https://meegle.com/xopenapp/setting/workObjectSetting", "setting_other"},
		{"setting_other_future_subpage", "https://meegle.com/xopenapp/setting/futureFeature", "setting_other"},
		{"setting_other_nested", "https://meegle.com/xopenapp/setting/workObjectSetting/nestedPage", "setting_other"},

		// Unknown
		{"unknown_bogus", "https://meegle.com/this-is/not/a/meego/path/that/matches", "unknown"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := DecodeURL(tc.url)
			if err != nil {
				t.Fatalf("DecodeURL(%q) unexpected error: %v", tc.url, err)
			}
			if res.URLKind != tc.want {
				t.Fatalf("DecodeURL(%q).URLKind = %q, want %q", tc.url, res.URLKind, tc.want)
			}
		})
	}
}

func TestDecodeURL_FieldsAreCaptured(t *testing.T) {
	t.Run("workitem_detail captures simple_name, work_item_type, work_item_id, host, query", func(t *testing.T) {
		res, err := DecodeURL("https://project.feishu.cn/xopenapp/story/detail/7092625364?scope=workspaces&node=abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.URLKind != "workitem_detail" {
			t.Fatalf("URLKind = %q", res.URLKind)
		}
		if res.Host != "project.feishu.cn" {
			t.Errorf("Host = %q", res.Host)
		}
		if res.SimpleName != "xopenapp" {
			t.Errorf("SimpleName = %q", res.SimpleName)
		}
		if res.WorkItemType != "story" {
			t.Errorf("WorkItemType = %q", res.WorkItemType)
		}
		if res.WorkItemID != "7092625364" {
			t.Errorf("WorkItemID = %q", res.WorkItemID)
		}
		if res.Query["scope"] != "workspaces" || res.Query["node"] != "abc" {
			t.Errorf("Query = %v", res.Query)
		}
	})

	t.Run("view_story preserves preset work_item_type extra", func(t *testing.T) {
		res, _ := DecodeURL("https://meegle.com/xopenapp/storyView/v1")
		if res.WorkItemType != "story" {
			t.Errorf("WorkItemType = %q, want story", res.WorkItemType)
		}
		if res.ViewID != "v1" {
			t.Errorf("ViewID = %q", res.ViewID)
		}
	})

	t.Run("resource work item type is unwrapped", func(t *testing.T) {
		res, _ := DecodeURL("https://meegle.com/xopenapp/workObjectView/_story_resource/v1")
		if res.URLKind != "view_workitem" {
			t.Fatalf("URLKind = %q", res.URLKind)
		}
		if res.WorkItemType != "story" {
			t.Errorf("WorkItemType = %q, want story", res.WorkItemType)
		}
		if !res.IsResource {
			t.Errorf("IsResource = false, want true")
		}
	})
}

func TestDecodeURL_Aliases(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		wantKind     string
		wantRedirect string
		extraAssert  func(t *testing.T, r *DecodeResult)
	}{
		{
			name:         "snake_case fetchCookieByLogin",
			url:          "https://meegle.com/fetchCookieByLogin",
			wantKind:     "login_fetch_cookie",
			wantRedirect: "/fetchCookieByLogin",
		},
		{
			name:         "feishu workbench mobile",
			url:          "https://meegle.com/feishu_workbench_mobile",
			wantKind:     "workbench",
			wantRedirect: "/feishu_workbench_mobile",
		},
		{
			name:         "space-scoped jiraImport",
			url:          "https://meegle.com/xopenapp/jiraImport",
			wantKind:     "import_jira",
			wantRedirect: "/xopenapp/jiraImport",
			extraAssert: func(t *testing.T, r *DecodeResult) {
				if r.SimpleName != "xopenapp" {
					t.Errorf("SimpleName = %q", r.SimpleName)
				}
			},
		},
		{
			name:         "/meego prefix stripped",
			url:          "https://meegle.com/meego/workbench",
			wantKind:     "workbench",
			wantRedirect: "/meego/workbench",
		},
		{
			name:         "story_create_open with param",
			url:          "https://meegle.com/story_create_open/bn_42",
			wantKind:     "story_create_open",
			wantRedirect: "/story_create_open/bn_42",
			extraAssert: func(t *testing.T, r *DecodeResult) {
				if r.BusinessName != "bn_42" {
					t.Errorf("BusinessName = %q", r.BusinessName)
				}
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := DecodeURL(tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.URLKind != tc.wantKind {
				t.Fatalf("URLKind = %q, want %q", res.URLKind, tc.wantKind)
			}
			if res.RedirectedFrom != tc.wantRedirect {
				t.Errorf("RedirectedFrom = %q, want %q", res.RedirectedFrom, tc.wantRedirect)
			}
			if tc.extraAssert != nil {
				tc.extraAssert(t, res)
			}
		})
	}
}

func TestDecodeURL_BarePathsAndHostless(t *testing.T) {
	t.Run("bare path without host", func(t *testing.T) {
		res, err := DecodeURL("/xopenapp/story/detail/123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.URLKind != "workitem_detail" {
			t.Fatalf("URLKind = %q", res.URLKind)
		}
		if res.Host != "" {
			t.Errorf("Host = %q, want empty for bare path", res.Host)
		}
	})

	t.Run("empty input is an error", func(t *testing.T) {
		if _, err := DecodeURL("   "); err == nil {
			t.Fatalf("expected error for empty input")
		}
	})
}
