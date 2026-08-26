// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapiclient

type Limits struct {
	MaxQueryBytes          int `json:"max_query_bytes"`
	MaxRelatedContextItems int `json:"max_related_context_items"`
}

type Availability struct {
	Available  bool   `json:"available"`
	RejectCode string `json:"reject_code,omitempty"`
	RejectMsg  string `json:"reject_msg,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Limits     Limits `json:"limits"`
	LogID      string `json:"-"`
}

// CLIConfig is the extensible server-side capability/config snapshot used by
// Meegle CLI. HandoffSuggestion is the first section; future features can add siblings.
type CLIConfig struct {
	HandoffSuggestion Availability `json:"handoff_suggestion"`
	LogID             string       `json:"-"`
}

type ContextType int32

const (
	ContextTypeProject ContextType = 1
	// Context type 2 is reserved by the IDL for the currently unsupported
	// WorkItemType context and must not be reused.
	ContextTypeWorkItem     ContextType = 3
	ContextTypeView         ContextType = 4
	ContextTypeMeasureChart ContextType = 5
)

type ProjectContext struct {
	ProjectKey string `json:"project_key"`
}

type WorkItemContext struct {
	ProjectKey      string `json:"project_key"`
	WorkItemTypeKey string `json:"work_item_type_key"`
	WorkItemID      string `json:"work_item_id"`
}

type ViewContext struct {
	ProjectKey      string `json:"project_key"`
	WorkItemTypeKey string `json:"work_item_type_key,omitempty"`
	ViewID          string `json:"view_id"`
}

type MeasureChartContext struct {
	ProjectKey string `json:"project_key"`
	ChartID    string `json:"chart_id"`
}

type RelatedContext struct {
	Type         ContextType          `json:"type"`
	Project      *ProjectContext      `json:"project,omitempty"`
	WorkItem     *WorkItemContext     `json:"work_item,omitempty"`
	View         *ViewContext         `json:"view,omitempty"`
	MeasureChart *MeasureChartContext `json:"measure_chart,omitempty"`
}

type CreateLinkRequest struct {
	UserQuery      string           `json:"user_query"`
	RelatedContext []RelatedContext `json:"related_context,omitempty"`
}

type CreateLinkResponse struct {
	Available  bool   `json:"available"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	RejectCode string `json:"reject_code,omitempty"`
	RejectMsg  string `json:"reject_msg,omitempty"`
	URL        string `json:"url,omitempty"`
	LogID      string `json:"-"`
}

type PreferenceUpdateResult struct {
	Success bool   `json:"success"`
	LogID   string `json:"-"`
}
