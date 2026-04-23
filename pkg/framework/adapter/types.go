// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package adapter

import (
	"context"
	"io"
)

type EntrypointAdapter interface {
	Adapt() (*RawInput, error)
}

type InputMaterializer interface {
	Materialize(params map[string]any) []string
}

type CommandInvocation struct {
	Path  []string
	Args  []string
	Flags map[string]any
}

type StructuredCommandRequest struct {
	Path   []string
	Params map[string]any
}

type IOMode int

const (
	IOModeTTY IOMode = iota
	IOModeStructured
	IOModeProgrammatic
)

type InputSourceMeta struct {
	Channel string
	Command string
	Product string
	Version string
}

type RawInput struct {
	Args    []string
	Env     map[string]string
	Context context.Context
	IOMode  IOMode
	Source  InputSourceMeta
	Stdout  io.Writer
	Stderr  io.Writer
}
