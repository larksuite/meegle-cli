// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

type CommandRouter struct {
	manager     *registry.RegistryManager
	rootCmd     *cobra.Command
	help        *registry.HelpGenerator
	appName     string
	execMu      sync.Mutex
	rwMu        sync.RWMutex
	activeInput *frameworkadapter.RawInput
	onParsed    func(*ParsedCommand) error
}

func NewCommandRouter(manager *registry.RegistryManager, appName string) (*CommandRouter, error) {
	if manager == nil || manager.Registry() == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "registry manager is not initialized")
	}
	r := &CommandRouter{
		manager: manager,
		help:    &registry.HelpGenerator{},
		appName: appName,
	}
	if err := r.Build(nil); err != nil {
		return nil, err
	}
	manager.OnRebuild(func(_ *registry.Registry) {
		_ = r.Build(nil)
	})
	return r, nil
}

func (r *CommandRouter) Build(executor func(*ParsedCommand) error) error {
	r.rwMu.Lock()
	defer r.rwMu.Unlock()
	reg := r.manager.Registry()
	if reg == nil {
		return frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "registry is not initialized")
	}
	root := &cobra.Command{
		Use:           firstNonEmpty(strings.TrimSpace(r.appName), "app"),
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return normalizeRouteError(err)
	})
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		node := r.nodeForCommand(cmd)
		if node == nil {
			defaultHelp(cmd, args)
			return
		}
		_, _ = io.WriteString(cmd.OutOrStdout(), r.help.Render(node))
		_, _ = io.WriteString(cmd.OutOrStdout(), "\n")
	})
	for _, flag := range registry.BuiltinFlags {
		registerFlag(root.PersistentFlags(), flag)
		if flag.Required {
			_ = root.MarkPersistentFlagRequired(flag.Name)
		}
	}
	for _, flag := range reg.Tree().GlobalFlags {
		registerFlag(root.PersistentFlags(), flag)
		if flag.Required {
			_ = root.MarkPersistentFlagRequired(flag.Name)
		}
	}
	for _, node := range reg.Root().Children {
		r.registerNode(root, node, executor)
	}
	r.rootCmd = root
	return nil
}

func (r *CommandRouter) Route(input *frameworkadapter.RawInput) (*ParsedCommand, error) {
	var parsed *ParsedCommand
	err := r.execute(input, func(candidate *ParsedCommand) error {
		parsed = candidate
		return nil
	})
	return parsed, err
}

func (r *CommandRouter) Execute(input *frameworkadapter.RawInput, executor func(*ParsedCommand) error) error {
	return r.execute(input, executor)
}

func (r *CommandRouter) execute(input *frameworkadapter.RawInput, executor func(*ParsedCommand) error) error {
	if input == nil {
		return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, "input is required")
	}
	r.execMu.Lock()
	defer r.execMu.Unlock()
	r.rwMu.Lock()
	r.activeInput = input
	r.onParsed = executor
	root := r.rootCmd
	r.rwMu.Unlock()
	defer func() {
		r.rwMu.Lock()
		r.activeInput = nil
		r.onParsed = nil
		r.rwMu.Unlock()
	}()
	if root == nil {
		return frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "router is not built")
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	root.SetArgs(input.Args)
	root.SetContext(ctx)
	stdout := input.Stdout
	stderr := input.Stderr
	switch input.IOMode {
	case frameworkadapter.IOModeTTY, frameworkadapter.IOModeStructured:
		if stdout == nil {
			stdout = os.Stdout
		}
		if stderr == nil {
			stderr = os.Stderr
		}
		root.SetOut(stdout)
		root.SetErr(stderr)
	default:
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
	}
	err := root.ExecuteContext(ctx)
	if err != nil {
		return normalizeRouteError(err)
	}
	return nil
}

func (r *CommandRouter) registerNode(parent *cobra.Command, node *registry.CommandNode, executor func(*ParsedCommand) error) {
	cmd := &cobra.Command{
		Use:        buildUse(node),
		Short:      strings.TrimSpace(node.Help.Brief),
		Long:       strings.TrimSpace(node.Help.Long),
		Aliases:    append([]string(nil), node.Aliases...),
		Hidden:     node.Meta.Hidden,
		Deprecated: node.Meta.Deprecated,
	}
	if node.Meta.Tags["router_allow_unknown_flags"] == "1" {
		cmd.FParseErrWhitelist.UnknownFlags = true
	}
	cmd.Annotations = map[string]string{"command_path": node.FullPathString()}
	for _, flag := range node.Flags {
		registerFlag(cmd.PersistentFlags(), flag)
		if flag.Required {
			_ = cmd.MarkPersistentFlagRequired(flag.Name)
		}
	}
	if node.IsExecutable() {
		// Executable commands do not accept positional args; disable filename completion fallback
		cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		nodeCopy := node
		cmd.RunE = func(c *cobra.Command, args []string) error {
			parsed, err := r.newParsedCommand(c, nodeCopy, args)
			if err != nil {
				return err
			}
			handler := executor
			r.rwMu.RLock()
			if r.onParsed != nil {
				handler = r.onParsed
			}
			r.rwMu.RUnlock()
			if handler == nil {
				return nil
			}
			return handler(parsed)
		}
	}
	for _, child := range node.Children {
		r.registerNode(cmd, child, executor)
	}
	parent.AddCommand(cmd)
}

func (r *CommandRouter) newParsedCommand(cmd *cobra.Command, node *registry.CommandNode, args []string) (*ParsedCommand, error) {
	effective, explicit, raw, diagnostics, err := collectFlags(cmd)
	if err != nil {
		return nil, err
	}
	r.rwMu.RLock()
	active := r.activeInput
	r.rwMu.RUnlock()
	rawArgs := append([]string(nil), args...)
	if active != nil {
		rawArgs = append([]string(nil), active.Args...)
	}
	return &ParsedCommand{
		Node:          node,
		FullPath:      node.FullPath(),
		Flags:         effective,
		ExplicitFlags: explicit,
		Args:          append([]string(nil), args...),
		RawArgs:       rawArgs,
		RawFlags:      raw,
		Diagnostics:   diagnostics,
	}, nil
}

func (r *CommandRouter) nodeForCommand(cmd *cobra.Command) *registry.CommandNode {
	if cmd == nil || r == nil || r.manager == nil || r.manager.Registry() == nil {
		return nil
	}
	path := cmd.Annotations["command_path"]
	if path == "" {
		return nil
	}
	return r.manager.Registry().GetByPath(path)
}

func buildUse(node *registry.CommandNode) string {
	if node == nil {
		return ""
	}
	parts := []string{node.Name}
	for _, arg := range node.Args {
		name := arg.Name
		if arg.Variadic {
			name += "..."
		}
		if arg.Required {
			parts = append(parts, fmt.Sprintf("<%s>", name))
		} else {
			parts = append(parts, fmt.Sprintf("[%s]", name))
		}
	}
	return strings.Join(parts, " ")
}

func registerFlag(fs *pflag.FlagSet, def registry.FlagDef) {
	switch def.Type {
	case registry.FlagTypeString:
		fs.StringP(def.Name, def.Short, stringDefault(def.Default), def.Description)
	case registry.FlagTypeInt:
		fs.IntP(def.Name, def.Short, intDefault(def.Default), def.Description)
	case registry.FlagTypeBool:
		fs.BoolP(def.Name, def.Short, boolDefault(def.Default), def.Description)
	case registry.FlagTypeFloat:
		fs.Float64P(def.Name, def.Short, floatDefault(def.Default), def.Description)
	case registry.FlagTypeStringSlice:
		fs.StringSliceP(def.Name, def.Short, stringSliceDefault(def.Default), def.Description)
	case registry.FlagTypeStringArray:
		fs.StringArrayP(def.Name, def.Short, stringSliceDefault(def.Default), def.Description)
	case registry.FlagTypeIntSlice:
		fs.IntSliceP(def.Name, def.Short, intSliceDefault(def.Default), def.Description)
	default:
		fs.StringP(def.Name, def.Short, stringDefault(def.Default), def.Description)
	}
	if def.Hidden {
		_ = fs.MarkHidden(def.Name)
	}
	if def.Deprecated != "" {
		_ = fs.MarkDeprecated(def.Name, def.Deprecated)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *CommandRouter) RootCommand() *cobra.Command {
	r.rwMu.RLock()
	defer r.rwMu.RUnlock()
	return r.rootCmd
}

func (r *CommandRouter) CurrentInput() *frameworkadapter.RawInput {
	r.rwMu.RLock()
	defer r.rwMu.RUnlock()
	return r.activeInput
}

func normalizeRouteError(err error) error {
	if err == nil {
		return nil
	}
	if frameworkerrors.As(err) != nil {
		return err
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "required flag"):
		return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamRequired, message)
	case strings.Contains(lower, "unknown flag"):
		return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, message)
	case strings.Contains(lower, "unknown command"):
		return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeCommandNotFound, message)
	case strings.Contains(lower, "accepts") && strings.Contains(lower, "arg"):
		return frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, message)
	default:
		return err
	}
}
