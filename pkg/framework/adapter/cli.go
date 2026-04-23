// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package adapter

import (
	"context"
	"io"
	"os"
)

type CLIAdapter struct {
	Args    []string
	Context context.Context
	Stdout  io.Writer
	Stderr  io.Writer
}

func (a CLIAdapter) Adapt() (*RawInput, error) {
	args := a.Args
	if args == nil {
		args = os.Args[1:]
	}
	return &RawInput{
		Args:    append([]string(nil), args...),
		Context: a.Context,
		IOMode:  IOModeTTY,
		Stdout:  a.Stdout,
		Stderr:  a.Stderr,
	}, nil
}
