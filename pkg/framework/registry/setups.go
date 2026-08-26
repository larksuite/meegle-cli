// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"fmt"
	"strings"
)

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

// CompositeSetup merges multiple command-tree sources into one final Registry.
// It is intentionally protocol-neutral: products can combine dynamic remote
// commands with local commands without teaching the router about either source.
type CompositeSetup struct {
	Setups []RegistrySetup
}

func NewCompositeSetup(setups ...RegistrySetup) *CompositeSetup {
	return &CompositeSetup{Setups: append([]RegistrySetup(nil), setups...)}
}

func (s *CompositeSetup) Setup(ctx context.Context) (*CommandTree, error) {
	result := &CommandTree{}
	rootNames := make(map[string]struct{})
	for index, setup := range s.Setups {
		if setup == nil {
			continue
		}
		tree, err := setup.Setup(ctx)
		if err != nil {
			return nil, fmt.Errorf("setup[%d]: %w", index, err)
		}
		if tree == nil {
			continue
		}
		if result.Version == "" {
			result.Version = tree.Version
		} else if tree.Version != "" {
			result.Version += "+" + tree.Version
		}
		result.GlobalFlags = append(result.GlobalFlags, tree.GlobalFlags...)
		for _, node := range tree.Nodes {
			if node == nil {
				continue
			}
			for _, name := range append([]string{node.Name}, node.Aliases...) {
				key := strings.ToLower(strings.TrimSpace(name))
				if key == "" {
					continue
				}
				if _, exists := rootNames[key]; exists {
					return nil, fmt.Errorf("duplicate root command %q", name)
				}
				rootNames[key] = struct{}{}
			}
			result.Nodes = append(result.Nodes, node)
		}
	}
	return result, nil
}
