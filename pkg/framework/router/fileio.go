// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"context"
	"fmt"
	"strings"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/fileio"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// MergeCommandParams prepares explicit file options before business validation.
func MergeCommandParams(ctx context.Context, parsed *ParsedCommand) error {
	if parsed.ExplicitFlags == nil {
		parsed.ExplicitFlags = make(map[string]any)
	}
	base, err := inputFileParams(ctx, parsed)
	if err != nil {
		return fileParamError(err)
	}
	merged, err := mergeStructuredParams(parsed.Flags, parsed.ExplicitFlags, base)
	if err != nil {
		return err
	}
	parsed.Flags = merged
	parsed.FileOutputs = nil
	raw, requested := parsed.ExplicitFlags["output-file"]
	if !requested {
		return nil
	}
	if err := validateOutputOptions(parsed); err != nil {
		return fileParamError(err)
	}
	values, ok := raw.([]string)
	if !ok {
		return fileParamError(fmt.Errorf("--output-file expects JSON objects"))
	}
	parsed.FileOutputs, err = fileio.ParseOutputs(values)
	if err != nil {
		return fileParamError(err)
	}
	parsed.Flags["envelope"] = true
	return nil
}

func inputFileParams(ctx context.Context, parsed *ParsedCommand) (map[string]any, error) {
	raw, exists := parsed.ExplicitFlags["input-file"]
	if !exists {
		return nil, nil
	}
	if _, exists := parsed.ExplicitFlags["params"]; exists {
		return nil, fmt.Errorf("--input-file and --params are mutually exclusive")
	}
	values, ok := raw.([]string)
	if !ok || len(values) != 1 {
		return nil, fmt.Errorf("--input-file expects exactly one @path")
	}
	params, err := fileio.ReadInput(ctx, values[0])
	if err != nil {
		return nil, fmt.Errorf("--input-file: %w", err)
	}
	controls := map[string]bool{"help": true, "version": true}
	for _, flag := range registry.BuiltinFlags {
		controls[flag.Name] = true
	}
	for _, name := range parsed.ControlFlags {
		controls[name] = true
	}
	if parsed.Node != nil {
		for _, flag := range append(parsed.Node.InheritedFlags(), parsed.Node.Flags...) {
			if flag.NoFileInput {
				controls[flag.Name] = true
			}
		}
	}
	for name := range params {
		if controls[strings.ToLower(strings.ReplaceAll(name, "_", "-"))] {
			return nil, fmt.Errorf("input file may only contain business parameters; %q is a CLI control option", name)
		}
	}
	return params, nil
}

func validateOutputOptions(parsed *ParsedCommand) error {
	if mode, _ := parsed.Flags["format"].(string); mode != "" && mode != "json" {
		return fmt.Errorf("--output-file requires JSON output")
	}
	if _, exists := parsed.ExplicitFlags["select"]; exists {
		return fmt.Errorf("--output-file and --select are mutually exclusive")
	}
	if dry, _ := parsed.Flags["dry-run"].(bool); dry {
		return fmt.Errorf("--output-file and --dry-run are mutually exclusive")
	}
	if envelope, exists := parsed.ExplicitFlags["envelope"]; exists && envelope != true {
		return fmt.Errorf("--output-file requires the result envelope")
	}
	return nil
}

func fileParamError(err error) error {
	return &frameworkerrors.CLIError{Category: frameworkerrors.CategoryUser, Code: frameworkerrors.CodeParamInvalid, Message: err.Error(), Cause: err}
}

func controlFlagNames(flags []registry.FlagDef) []string {
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		names = append(names, flag.Name)
	}
	return names
}
