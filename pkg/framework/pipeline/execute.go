// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pipeline

import "context"

func (p *Pipeline) Execute(ctx context.Context, state *PipelineContext) error {
	if p == nil {
		return nil
	}
	if state == nil {
		state = &PipelineContext{}
	}
	for _, step := range p.Steps {
		if step == nil {
			continue
		}
		if err := step.Execute(ctx, state); err != nil {
			return err
		}
	}
	return nil
}
