// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

func collectFlags(cmd *cobra.Command) (map[string]any, map[string]any, map[string]string, []RouteDiagnostic, error) {
	effective := make(map[string]any)
	explicit := make(map[string]any)
	raw := make(map[string]string)
	diagnostics := make([]RouteDiagnostic, 0)
	collect := func(fs *pflag.FlagSet) error {
		var visitErr error
		fs.VisitAll(func(flag *pflag.Flag) {
			if visitErr != nil {
				return
			}
			if flag == nil || flag.Name == "help" {
				return
			}
			value, err := readFlagValue(fs, flag)
			if err != nil {
				visitErr = err
				return
			}
			effective[flag.Name] = value
			if flag.Changed {
				explicit[flag.Name] = value
				raw[flag.Name] = flag.Value.String()
			}
		})
		return visitErr
	}
	if err := collect(cmd.InheritedFlags()); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := collect(cmd.Flags()); err != nil {
		return nil, nil, nil, nil, err
	}
	_ = diagnostics
	return effective, explicit, raw, diagnostics, nil
}

func readFlagValue(fs *pflag.FlagSet, flag *pflag.Flag) (any, error) {
	switch flag.Value.Type() {
	case "string":
		return fs.GetString(flag.Name)
	case "int":
		return fs.GetInt(flag.Name)
	case "bool":
		return fs.GetBool(flag.Name)
	case "float64":
		return fs.GetFloat64(flag.Name)
	case "stringSlice":
		return fs.GetStringSlice(flag.Name)
	case "stringArray":
		return fs.GetStringArray(flag.Name)
	case "intSlice":
		return fs.GetIntSlice(flag.Name)
	default:
		return nil, frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, fmt.Sprintf("unsupported flag type %s", flag.Value.Type()))
	}
}

func stringDefault(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func intDefault(value any) int {
	if typed, ok := value.(int); ok {
		return typed
	}
	return 0
}

func boolDefault(value any) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return false
}

func floatDefault(value any) float64 {
	if typed, ok := value.(float64); ok {
		return typed
	}
	return 0
}

func stringSliceDefault(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	return nil
}

func intSliceDefault(value any) []int {
	if typed, ok := value.([]int); ok {
		return append([]int(nil), typed...)
	}
	return nil
}
