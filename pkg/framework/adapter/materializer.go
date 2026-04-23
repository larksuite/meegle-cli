// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package adapter

import (
	"fmt"
	"sort"
	"strings"
)

type DefaultMaterializer struct{}

func (m DefaultMaterializer) Materialize(params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		value := params[key]
		switch typed := value.(type) {
		case []string:
			args = append(args, "--"+key, strings.Join(typed, ","))
		case []any:
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, fmt.Sprint(item))
			}
			args = append(args, "--"+key, strings.Join(parts, ","))
		default:
			args = append(args, "--"+key, fmt.Sprint(value))
		}
	}
	return args
}
