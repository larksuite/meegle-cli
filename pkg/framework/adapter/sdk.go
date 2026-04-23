// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package adapter

import "context"

type SDKAdapter struct {
	Invocation   *CommandInvocation
	Context      context.Context
	Materializer InputMaterializer
}

func (a SDKAdapter) Adapt() (*RawInput, error) {
	materializer := a.Materializer
	if materializer == nil {
		materializer = DefaultMaterializer{}
	}
	if a.Invocation == nil {
		return &RawInput{Context: a.Context, IOMode: IOModeProgrammatic}, nil
	}
	pathArgs := append([]string(nil), a.Invocation.Path...)
	positionalArgs := append([]string(nil), a.Invocation.Args...)
	flagArgs := materializer.Materialize(a.Invocation.Flags)
	args := append(pathArgs, positionalArgs...)
	args = append(args, flagArgs...)
	return &RawInput{
		Args:    args,
		Context: a.Context,
		IOMode:  IOModeProgrammatic,
	}, nil
}
