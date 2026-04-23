// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

// completionRule defines a data source for flag value completion.
type completionRule struct {
	toolName string
	params   func(cmd *cobra.Command) map[string]any
	extract  func(data any) []string
}

// completionRules maps flag names to their completion rules.
// Note: live MCP calls during tab completion cause noticeable latency (>500ms),
// so only the rule definitions are kept for now; live completion is disabled.
// All flags use noFileComp to prevent file completion fallback.
// TODO: re-enable live completion once caching is in place.
var completionRules = map[string]completionRule{}

// extractStringField returns a function that extracts the given field from an MCP result array.
func extractStringField(field string) func(data any) []string {
	return func(data any) []string {
		items := toSlice(data)
		var result []string
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if v, ok := m[field]; ok {
					if s, ok := v.(string); ok && s != "" {
						result = append(result, s)
					}
				}
			}
		}
		return result
	}
}

// toSlice normalizes MCP response data into a []any slice.
func toSlice(data any) []any {
	if arr, ok := data.([]any); ok {
		return arr
	}
	if m, ok := data.(map[string]any); ok {
		for _, key := range []string{"items", "list", "data", "results"} {
			if v, ok := m[key]; ok {
				if arr, ok := v.([]any); ok {
					return arr
				}
			}
		}
	}
	return nil
}

// makeCompletionFunc creates a Cobra flag completion callback.
func makeCompletionFunc(rule completionRule, client *mcpclient.Client) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if client == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		params := rule.params(cmd)
		if params == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		resp, err := client.CallTool(ctx, rule.toolName, params)
		if err != nil || resp == nil || resp.Data == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		candidates := rule.extract(resp.Data)
		if toComplete == "" {
			return candidates, cobra.ShellCompDirectiveNoFileComp
		}
		var filtered []string
		for _, c := range candidates {
			if strings.HasPrefix(c, toComplete) {
				filtered = append(filtered, c)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
}

// RegisterFlagCompletions walks the Cobra command tree and registers value completion callbacks for matching flags.
func RegisterFlagCompletions(root *cobra.Command, client *mcpclient.Client) {
	_ = root.RegisterFlagCompletionFunc("format", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "table", "ndjson"}, cobra.ShellCompDirectiveNoFileComp
	})
	registerCompletionRecursive(root, client)
}

// noFileComp is a no-op completion callback that prevents flag values from falling back to filename completion.
var noFileComp = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func registerCompletionRecursive(cmd *cobra.Command, client *mcpclient.Client) {
	// Visit PersistentFlags (router registers command flags as Persistent) + LocalFlags
	visitFlags := func(f *pflag.Flag) {
		if _, hasRule := completionRules[f.Name]; hasRule {
			_ = cmd.RegisterFlagCompletionFunc(f.Name, makeCompletionFunc(completionRules[f.Name], client))
		} else if f.Value.Type() != "bool" {
			_ = cmd.RegisterFlagCompletionFunc(f.Name, noFileComp)
		}
	}
	cmd.PersistentFlags().VisitAll(visitFlags)
	cmd.Flags().VisitAll(visitFlags)
	for _, sub := range cmd.Commands() {
		registerCompletionRecursive(sub, client)
	}
}
