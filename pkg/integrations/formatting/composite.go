// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"fmt"
	"strings"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

// CompositeFormatter dispatches by FormatOptions.Mode:
//   - "table"  → TableView.Encode (after wrapper-array unwrap)
//   - "ndjson" → Base.Format (after wrapper-array unwrap)
//   - "json" / "" → Base.Format (pass-through, no unwrap)
//   - anything else → user-facing error listing the supported set
//
// Unwrap rationale: most MCP list responses arrive as {"list": [...]} /
// {"data": [...]} wrappers. For ndjson and table the wrapper hides the
// records users want to stream/tabulate, so we peel it; json stays faithful
// so existing scripts reading .list[0] keep working. When --envelope is on,
// we do NOT unwrap (the envelope itself is a {data, meta, error} wrapper
// and that is exactly what the user asked to see).
//
// Error-envelope rendering (FormatOptions.ErrorEnvelope=true) takes priority
// over everything above: no unwrapping, and table mode renders the inner
// `error` record as a KV table with code/message/retryable rows per the
// design spec §"错误处理契约".
type CompositeFormatter struct {
	Base  frameworkoutput.Formatter
	Table TableView
}

// NewComposite wires a base framework Formatter and a TableView into a single
// Formatter usable by frameworkoutput.Processor.
func NewComposite(base frameworkoutput.Formatter, table TableView) CompositeFormatter {
	return CompositeFormatter{Base: base, Table: table}
}

func (c CompositeFormatter) Format(data any, opts *frameworkoutput.FormatOptions) ([]byte, error) {
	mode := ""
	envelope := false
	errorEnvelope := false
	if opts != nil {
		mode = strings.TrimSpace(opts.Mode)
		envelope = opts.Envelope
		errorEnvelope = opts.ErrorEnvelope
	}
	if errorEnvelope {
		return c.formatErrorEnvelope(data, mode, opts)
	}
	unwrap := !envelope && (mode == "table" || mode == "ndjson")
	if unwrap {
		data = UnwrapWrapperObject(data)
	}
	switch mode {
	case "table":
		return c.Table.Encode(data, opts)
	case "", "json", "ndjson":
		return c.Base.Format(data, opts)
	default:
		return nil, fmt.Errorf("unsupported --format %q; expected one of: json, ndjson, table", mode)
	}
}

// formatErrorEnvelope handles the pre-built {data:null, meta, error} payload.
// json / ndjson / "" pass it through verbatim so agents see the full envelope;
// table drills into the inner `error` record and renders {code, message,
// retryable} as a KV table per the spec. Unknown modes still surface the
// enum-validation error so typos in --format are reported uniformly.
func (c CompositeFormatter) formatErrorEnvelope(data any, mode string, opts *frameworkoutput.FormatOptions) ([]byte, error) {
	switch mode {
	case "table":
		table := c.Table
		if table.MaxColumns == 0 {
			stderr := table.Stderr
			table = DefaultTableView()
			if stderr != nil {
				table.Stderr = stderr
			}
		}
		// Error messages and suggestions are actionable data. Truncating them
		// can hide required parameters or recovery commands, so error-envelope
		// tables deliberately disable the normal data-cell width limit.
		table.MaxCellWidth = 0
		if env, ok := data.(map[string]any); ok {
			if errRec, ok := env["error"].(map[string]any); ok {
				return table.Encode(errRec, opts)
			}
		}
		return table.Encode(data, opts)
	case "", "json", "ndjson":
		return c.Base.Format(data, opts)
	default:
		return nil, fmt.Errorf("unsupported --format %q; expected one of: json, ndjson, table", mode)
	}
}
