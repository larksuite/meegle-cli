// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

type ParsedCommand struct {
	Node          *registry.CommandNode
	FullPath      []string
	Flags         map[string]any
	ExplicitFlags map[string]any
	Args          []string
	IsHelp        bool
	RawArgs       []string
	RawFlags      map[string]string
	Diagnostics   []RouteDiagnostic
}

type RouteDiagnostic struct {
	Kind    string
	Token   string
	Message string
}

// ErrInvalidArgs returns a user-category CLIError for invalid argument input.
// It is provided here so that callers (e.g. cliapp) do not need to import the
// full errors package just to construct a simple user error.
func ErrInvalidArgs(msg string) error {
	return errors.New(errors.CategoryUser, errors.CodeParamInvalid, msg)
}
