// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pipeline

import "context"

type PipelineStep interface {
	Name() string
	Execute(ctx context.Context, state *PipelineContext) error
}

type Pipeline struct {
	Steps []PipelineStep
}
