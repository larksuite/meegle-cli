// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

type MeegleValidateStep struct{}

func (s *MeegleValidateStep) Name() string { return "meegle_validate" }

func (s *MeegleValidateStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	_ = ctx
	return validateMeegleCommand(state)
}

func validateMeegleCommand(state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}
	missing := pipeline.CollectMissingRequiredInputs(state.Parsed)
	if missing.Len() > 0 {
		return meerrors.NewClientError("CLIENT_MISSING_REQUIRED", formatMeegleMissingRequiredInputs(missing)).
			WithSuggestion(fmt.Sprintf("meegle %s --help", state.Parsed.Node.FullPathString()))
	}
	for _, flag := range state.Parsed.Node.InheritedFlags() {
		if len(flag.Enum) == 0 {
			continue
		}
		value, ok := state.Parsed.Flags[flag.Name]
		if !ok {
			continue
		}
		candidate := fmt.Sprint(value)
		matched := false
		for _, allowed := range flag.Enum {
			if candidate == allowed {
				matched = true
				break
			}
		}
		if !matched {
			return meerrors.NewClientError("CLIENT_INVALID_VALUE",
				fmt.Sprintf("invalid value %q for --%s; allowed: %s", candidate, flag.Name, strings.Join(flag.Enum, "|")))
		}
	}
	return nil
}

func formatMeegleMissingRequiredInputs(missing pipeline.MissingRequiredInputs) string {
	if len(missing.Flags) == 1 && len(missing.Args) == 0 {
		return fmt.Sprintf("missing required parameter: --%s", missing.Flags[0])
	}
	if len(missing.Flags) == 0 && len(missing.Args) == 1 {
		return fmt.Sprintf("missing required argument <%s>", missing.Args[0])
	}

	inputs := make([]string, 0, missing.Len())
	for _, name := range missing.Flags {
		inputs = append(inputs, "--"+name)
	}
	for _, name := range missing.Args {
		inputs = append(inputs, "<"+name+">")
	}
	switch {
	case len(missing.Args) == 0:
		return "missing required parameters: " + strings.Join(inputs, ", ")
	case len(missing.Flags) == 0:
		return "missing required arguments: " + strings.Join(inputs, ", ")
	default:
		return "missing required inputs: " + strings.Join(inputs, ", ")
	}
}

// StructuredFlagNameNormalizeStep maps MCP snake_case structured parameters
// to their generated kebab-case CLI flag names after ParamMergeStep.
type StructuredFlagNameNormalizeStep struct{}

func (s *StructuredFlagNameNormalizeStep) Name() string { return "structured_flag_name_normalize" }

func (s *StructuredFlagNameNormalizeStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	_ = ctx
	normalizeStructuredFlagNames(state)
	return nil
}

func normalizeStructuredFlagNames(state *pipeline.PipelineContext) {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return
	}
	known := make(map[string]struct{})
	for _, flag := range append(state.Parsed.Node.InheritedFlags(), state.Parsed.Node.Flags...) {
		known[flag.Name] = struct{}{}
	}
	for key, value := range state.Parsed.ExplicitFlags {
		kebab := strings.ReplaceAll(key, "_", "-")
		if key == kebab {
			continue
		}
		if _, ok := known[kebab]; !ok {
			continue
		}
		if _, direct := state.Parsed.ExplicitFlags[kebab]; !direct {
			state.Parsed.Flags[kebab] = value
			state.Parsed.ExplicitFlags[kebab] = value
		}
		delete(state.Parsed.Flags, key)
		delete(state.Parsed.ExplicitFlags, key)
	}
}
