// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/output/formatter"
)

// DefaultProcessor assembles the full output pipeline for a product:
// select → envelope → composite formatter (json/ndjson/table) → stdio
// writer. Products should use this instead of
// pkg/framework/output/formatter.DefaultProcessor() when they want table
// rendering.
//
// No implicit wrapper-array unwrap happens in the hook chain — every format
// receives the raw response so metadata siblings (total, pagination, ...)
// are preserved. Users drill into wrapped records explicitly via
// --select=KEY or --select=KEY.field, where the FieldSelectorHook
// broadcasts the remaining path over array elements when needed.
// CompositeFormatter still applies a loss-less single-key peel before
// table/ndjson rendering so {"list":[...]} alone still renders as rows.
func DefaultProcessor() *frameworkoutput.Processor {
	return &frameworkoutput.Processor{
		Hooks: []frameworkoutput.OutputHook{
			frameworkoutput.FieldSelectorHook{},
			frameworkoutput.EnvelopeHook{},
		},
		Formatter: NewComposite(formatter.Standard{}, DefaultTableView()),
		Writer:    formatter.Stdio{},
	}
}
