// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func snapshotFlags(root *cobra.Command) func() {
	seen := make(map[*pflag.Flag]bool)
	var restore []func()
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		for _, fs := range []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags(), cmd.InheritedFlags()} {
			fs.VisitAll(func(flag *pflag.Flag) {
				if seen[flag] {
					return
				}
				seen[flag] = true
				value, changed := flag.Value.String(), flag.Changed
				if slice, ok := flag.Value.(pflag.SliceValue); ok {
					original := flag.Value
					values := append([]string(nil), slice.GetSlice()...)
					flag.Value = &invocationSlice{Value: original, SliceValue: slice, first: !changed}
					restore = append(restore, func() { _ = slice.Replace(values); flag.Value = original; flag.Changed = changed })
				} else {
					restore = append(restore, func() { _ = flag.Value.Set(value); flag.Changed = changed })
				}
			})
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	return func() {
		for _, reset := range restore {
			reset()
		}
	}
}

// pflag keeps a private changed bit inside slice values. Replace alone cannot
// reset it, so the first explicit Set in a new invocation discards defaults.
type invocationSlice struct {
	pflag.Value
	pflag.SliceValue
	first bool
}

func (v *invocationSlice) Set(value string) error {
	if v.first {
		if err := v.Replace(nil); err != nil {
			return err
		}
		v.first = false
	}
	return v.Value.Set(value)
}
