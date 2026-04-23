// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

// ViewRegistry is a placeholder extension point for per-command column
// overrides. The current design commits to the global priority list covering
// ~80% of commands (see spec §决策摘要); add RegisterView methods here when a
// command needs a bespoke view. Keeping this type published ensures the API
// shape is stable before the first override lands.
type ViewRegistry struct{}

// NewViewRegistry returns a new empty registry.
func NewViewRegistry() *ViewRegistry { return &ViewRegistry{} }
