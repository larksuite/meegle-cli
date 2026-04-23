// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import "context"

type StaticSetup struct {
	Tree *CommandTree
}

func NewStaticSetup(tree *CommandTree) *StaticSetup {
	return &StaticSetup{Tree: tree}
}

func (s *StaticSetup) Setup(ctx context.Context) (*CommandTree, error) {
	_ = ctx
	return s.Tree, nil
}

type ProgrammaticSetup struct {
	Build func(context.Context) (*CommandTree, error)
}

func NewProgrammaticSetup(build func(context.Context) (*CommandTree, error)) *ProgrammaticSetup {
	return &ProgrammaticSetup{Build: build}
}

func (s *ProgrammaticSetup) Setup(ctx context.Context) (*CommandTree, error) {
	if s == nil || s.Build == nil {
		return nil, nil
	}
	return s.Build(ctx)
}
