// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

type Request struct {
	Command *router.ParsedCommand
	Values  map[string]any
	Meta    RequestMeta
	Source  RequestSource
}

type RequestMeta struct {
	DryRun  bool   `json:"dry_run,omitempty"`
	Output  string `json:"output,omitempty"`
	Select  string `json:"select,omitempty"`
	Verbose bool   `json:"verbose,omitempty"`
}

type RequestSource struct {
	Channel string `json:"channel,omitempty"`
	Command string `json:"command,omitempty"`
	Product string `json:"product,omitempty"`
	Version string `json:"version,omitempty"`
}

type NormalizedRequest struct {
	Tool   string         `json:"tool"`
	Input  map[string]any `json:"input"`
	Meta   RequestMeta    `json:"meta,omitempty"`
	Source RequestSource  `json:"source,omitempty"`
}

type RawResult struct {
	Data     any
	Metadata map[string]any
	Err      error
}

type Executor interface {
	Execute(ctx context.Context, req *Request) (*RawResult, error)
}
