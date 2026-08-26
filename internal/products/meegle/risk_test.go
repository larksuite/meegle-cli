// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"testing"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
)

func TestCommandRiskDirectoryExactlyCoversFallbackTools(t *testing.T) {
	fallbackTools := dynamic.FallbackToolNames()
	seen := make(map[string]struct{}, len(fallbackTools))
	for _, toolName := range fallbackTools {
		if _, duplicate := seen[toolName]; duplicate {
			t.Fatalf("fallback tool names contain duplicate %q", toolName)
		}
		seen[toolName] = struct{}{}

		risk, ok := commandRiskByTool[toolName]
		if !ok {
			t.Errorf("fallback tool %q has no reviewed risk", toolName)
			continue
		}
		if _, err := platformapi.ParseRisk(risk.String()); err != nil {
			t.Errorf("fallback tool %q has invalid risk %q: %v", toolName, risk, err)
		}
	}

	for toolName := range commandRiskByTool {
		if _, ok := seen[toolName]; !ok {
			t.Errorf("risk directory contains stale non-fallback tool %q", toolName)
		}
	}
}

func TestCommandRiskDirectory_ClassifiesCriticalOperations(t *testing.T) {
	want := map[string]platformapi.Risk{
		"get_workitem_brief": platformapi.RiskRead,
		"search_by_mql":      platformapi.RiskRead,
		"create_workitem":    platformapi.RiskWrite,
		"update_field":       platformapi.RiskWrite,
		"transition_state":   platformapi.RiskWrite,
		"add_comment":        platformapi.RiskWrite,
		"upload_file":        platformapi.RiskWrite,
		"create_wbs_draft":   platformapi.RiskWrite,
		"publish_wbs_draft":  platformapi.RiskHighRiskWrite,
		"reset_wbs_draft":    platformapi.RiskHighRiskWrite,
	}

	for toolName, wantRisk := range want {
		if got := commandRiskByTool[toolName]; got != wantRisk {
			t.Errorf("commandRiskByTool[%q] = %q, want %q", toolName, got, wantRisk)
		}
	}
}
