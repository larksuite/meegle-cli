// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

// MergeStructuredParams folds --params (JSON) and --set (key=value) into the
// flag set. Keys already present in explicit (i.e. the user passed a real CLI
// flag) take precedence and are left untouched.
//
// Keys contributed by --params and --set are promoted into explicit so
// downstream steps that filter by ExplicitFlags treat them the same as a
// user-typed CLI flag.
//
// explicit is mutated in place; callers should pass the same map they intend
// to use as ParsedCommand.ExplicitFlags.
func MergeStructuredParams(flags map[string]any, explicit map[string]any) (map[string]any, error) {
	if flags == nil {
		flags = make(map[string]any)
	}
	if explicit == nil {
		explicit = make(map[string]any)
	}
	merged := make(map[string]any)
	if rawParams, ok := flags["params"].(string); ok && strings.TrimSpace(rawParams) != "" {
		payload, err := resolveParamsPayload(rawParams)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &merged); err != nil {
			return nil, frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeInvalidParams, fmt.Sprintf("--params is not valid JSON: %v", err))
		}
	}
	if rawSets, ok := flags["set"].([]string); ok {
		for _, item := range rawSets {
			path, value, err := parseSetKV(item)
			if err != nil {
				return nil, err
			}
			setNestedField(merged, path, inferType(value))
		}
	}
	for key, value := range merged {
		if _, exists := explicit[key]; exists {
			continue
		}
		flags[key] = value
		explicit[key] = value
	}
	delete(flags, "params")
	delete(flags, "set")
	return flags, nil
}

// resolveParamsPayload returns the JSON bytes that --params should be parsed
// from. When raw starts with '@', the remainder is treated as a file path and
// the file contents are returned — sidestepping shell quoting differences
// (CMD vs PowerShell vs bash) that make embedded JSON painful on Windows.
// Otherwise the input is returned unchanged.
func resolveParamsPayload(raw string) ([]byte, error) {
	if !strings.HasPrefix(raw, "@") {
		return []byte(raw), nil
	}
	path := strings.TrimPrefix(raw, "@")
	if strings.TrimSpace(path) == "" {
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, "--params @<path> requires a non-empty file path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, fmt.Sprintf("--params cannot read file %q: %v", path, err))
	}
	return data, nil
}

func parseSetKV(item string) ([]string, string, error) {
	key, value, ok := strings.Cut(item, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return nil, "", frameworkerrors.New(frameworkerrors.CategoryUser, frameworkerrors.CodeParamInvalid, "--set expects key=value")
	}
	return strings.Split(key, "."), value, nil
}

func setNestedField(target map[string]any, path []string, value any) {
	current := target
	for index, key := range path {
		if index == len(path)-1 {
			current[key] = value
			return
		}
		next, ok := current[key].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[key] = next
		}
		current = next
	}
}

func inferType(value string) any {
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		return parsed
	}
	return value
}
