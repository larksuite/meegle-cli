// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

type Context struct {
	Input   *frameworkadapter.RawInput
	Parsed  *frameworkrouter.ParsedCommand
	Values  map[string]any
	Result  *executor.RawResult
	Options *FormatOptions
}

type FormatOptions struct {
	Mode     string
	Select   []string
	Envelope bool
	Verbose  bool
	// ErrorEnvelope is set by Processor.RenderError when formatting the
	// pre-built error envelope `{data:null, meta, error:{code,message,retryable}}`.
	// Downstream formatters must not re-wrap or peel the payload, and table
	// mode renders the inner error record as a KV table rather than the full
	// envelope. Not user-settable.
	ErrorEnvelope bool
}

type OutputHook interface {
	Name() string
	Process(ctx *Context, data any) (any, error)
}

type Formatter interface {
	Format(data any, opts *FormatOptions) ([]byte, error)
}

type IOWriter interface {
	Write(ctx *Context, payload []byte) error
}
