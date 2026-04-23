// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"fmt"
	"strconv"
	"strings"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
)

// ParseInput converts RawInput to ParsedCommand by resolving the command and parsing parameters.
// This is a helper function primarily used in tests and for programmatic command construction.
//
// NOTE: In production CLI flows, parameter parsing is handled by cobra. This function is
// mainly for SDK/programmatic access and test scenarios.
func ParseInput(reg *registry.Registry, input *frameworkadapter.RawInput) (*ParsedCommand, error) {
	if reg == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "registry is not configured")
	}
	if input == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, "input is required")
	}
	node, remaining := reg.Resolve(input.Args)
	if node == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeCommandNotFound, "command not found")
	}
	flags, explicitFlags, rawFlags, args, isHelp, err := parseTokens(remaining, reg.AllFlagsForNode(node))
	if err != nil {
		return nil, err
	}
	if err := validateArgs(node.Args, args); err != nil {
		return nil, err
	}
	return &ParsedCommand{
		Node:          node,
		FullPath:      node.FullPath(),
		Flags:         flags,
		ExplicitFlags: explicitFlags,
		Args:          args,
		IsHelp:        isHelp,
		RawArgs:       append([]string(nil), input.Args...),
		RawFlags:      rawFlags,
	}, nil
}

func parseTokens(tokens []string, defs []registry.FlagDef) (map[string]any, map[string]any, map[string]string, []string, bool, error) {
	flags := make(map[string]any)
	explicitFlags := make(map[string]any)
	rawFlags := make(map[string]string)
	var args []string
	isHelp := false

	flagDefs := make(map[string]registry.FlagDef)
	flagShorts := make(map[string]string)
	for _, def := range defs {
		key := strings.ToLower(def.Name)
		flagDefs[key] = def
		if def.Short != "" {
			flagShorts[strings.ToLower(def.Short)] = key
		}
	}

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if strings.HasPrefix(token, "--") {
			key := token[2:]
			if key == "help" {
				isHelp = true
				continue
			}
			if strings.Contains(key, "=") {
				parts := strings.SplitN(key, "=", 2)
				key = strings.ToLower(parts[0])
				value := parts[1]
				if def, ok := flagDefs[key]; ok {
					flags[key] = parseValue(value, def.Type)
					explicitFlags[key] = parseValue(value, def.Type)
					rawFlags[key] = value
				}
			} else {
				key = strings.ToLower(key)
				if def, ok := flagDefs[key]; ok {
					if def.Type == registry.FlagTypeBool {
						flags[key] = true
						explicitFlags[key] = true
						rawFlags[key] = "true"
					} else if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
						i++
						flags[key] = parseValue(tokens[i], def.Type)
						explicitFlags[key] = parseValue(tokens[i], def.Type)
						rawFlags[key] = tokens[i]
					}
				}
			}
		} else if strings.HasPrefix(token, "-") && len(token) > 1 && token != "-" {
			short := strings.ToLower(token[1:])
			if fullName, ok := flagShorts[short]; ok {
				def := flagDefs[fullName]
				if def.Type == registry.FlagTypeBool {
					flags[fullName] = true
					explicitFlags[fullName] = true
					rawFlags[fullName] = "true"
				} else if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
					i++
					flags[fullName] = parseValue(tokens[i], def.Type)
					explicitFlags[fullName] = parseValue(tokens[i], def.Type)
					rawFlags[fullName] = tokens[i]
				}
			}
		} else {
			args = append(args, token)
		}
	}

	return flags, explicitFlags, rawFlags, args, isHelp, nil
}

func parseValue(val string, typ string) any {
	switch typ {
	case registry.FlagTypeInt:
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return int(i)
		}
	case registry.FlagTypeFloat:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	case registry.FlagTypeBool:
		return val == "true" || val == "1" || val == "yes"
	case registry.FlagTypeStringSlice, registry.FlagTypeStringArray:
		return []string{val}
	}
	return val
}

func validateArgs(defs []registry.ArgDef, args []string) error {
	argIndex := 0
	for _, def := range defs {
		if def.Required && argIndex >= len(args) {
			return fmt.Errorf("required argument %q is missing", def.Name)
		}
		if def.Variadic {
			if argIndex >= len(args) && def.Required {
				return fmt.Errorf("required variadic argument %q has no values", def.Name)
			}
			argIndex = len(args)
		} else {
			argIndex++
		}
	}
	if argIndex < len(args) && (len(defs) == 0 || !defs[len(defs)-1].Variadic) {
		return fmt.Errorf("too many arguments: expected %d, got %d", len(defs), len(args))
	}
	return nil
}
