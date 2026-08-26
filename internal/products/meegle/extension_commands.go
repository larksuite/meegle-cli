// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/extension/credential"
	"github.com/larksuite/meegle-cli/extension/transport"
	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"
	internaltransport "github.com/larksuite/meegle-cli/internal/extension/transport"
)

func newExtensionCommand(diagnostics platformruntime.Diagnostics, identity ResolvedIdentity, issues []ToolMappingIssue, transportDiagnostics ...internaltransport.Diagnostics) *cobra.Command {
	root := &cobra.Command{Use: "extension", Short: "Inspect compiled CLI extensions"}
	root.AddCommand(
		newExtensionDoctorCommand(diagnostics, identity, issues, transportDiagnostics...),
		newCredentialDiagnosticsCommand(identity),
		newTransportDiagnosticsCommand(transportDiagnostics...),
		newPluginDiagnosticsCommand(diagnostics),
		newPolicyDiagnosticsCommand(diagnostics),
		newDynamicToolDiagnosticsCommand(issues),
	)
	return root
}

func newExtensionDoctorCommand(diagnostics platformruntime.Diagnostics, identity ResolvedIdentity, issues []ToolMappingIssue, transportDiagnostics ...internaltransport.Diagnostics) *cobra.Command {
	return diagnosticCommand("doctor", "Summarize active CLI extensions", func(cmd *cobra.Command) {
		printCredentialDiagnostics(cmd, identity)
		printTransportDiagnostics(cmd, transportDiagnostics...)
		printPluginDiagnostics(cmd, diagnostics)
		printPolicyDiagnostics(cmd, diagnostics)
		printDynamicToolDiagnostics(cmd, issues)
	})
}

func newCredentialDiagnosticsCommand(identity ResolvedIdentity) *cobra.Command {
	return diagnosticCommand("credentials", "List credential providers", func(cmd *cobra.Command) {
		printCredentialDiagnostics(cmd, identity)
	})
}

func newTransportDiagnosticsCommand(diagnostics ...internaltransport.Diagnostics) *cobra.Command {
	return diagnosticCommand("transport", "Show the active transport provider", func(cmd *cobra.Command) {
		printTransportDiagnostics(cmd, diagnostics...)
	})
}

func newPluginDiagnosticsCommand(diagnostics platformruntime.Diagnostics) *cobra.Command {
	return diagnosticCommand("plugins", "List platform plugins", func(cmd *cobra.Command) {
		printPluginDiagnostics(cmd, diagnostics)
	})
}

func newPolicyDiagnosticsCommand(diagnostics platformruntime.Diagnostics) *cobra.Command {
	return diagnosticCommand("policy", "Show the active restriction owner", func(cmd *cobra.Command) {
		printPolicyDiagnostics(cmd, diagnostics)
	})
}

func newDynamicToolDiagnosticsCommand(issues []ToolMappingIssue) *cobra.Command {
	return diagnosticCommand("discovery", "List isolated dynamic tool definitions", func(cmd *cobra.Command) {
		printDynamicToolDiagnostics(cmd, issues)
	})
}

func diagnosticCommand(use, short string, render func(*cobra.Command)) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Annotations: map[string]string{"command_source": "static", "risk_level": "read"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			render(cmd)
			return nil
		},
	}
}

func printCredentialDiagnostics(cmd *cobra.Command, identity ResolvedIdentity) {
	registrations := credential.Registrations()
	if len(registrations) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "credential: built-in")
	} else {
		for _, registration := range registrations {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "credential: %s priority=%d\n", registration.Name, registration.Priority)
		}
	}
	active := identity.CredentialProvider
	notEvaluated := identity.ProfileName == "" && identity.Host == "" && identity.Token == "" && active == ""
	if notEvaluated {
		active = "not-evaluated"
	} else if active == "" {
		active = "built-in"
	}
	active = safeCredentialLabel(active, "built-in")
	tokenSource := identity.CredentialTokenSource
	if notEvaluated {
		tokenSource = "unknown"
	} else if tokenSource == "" {
		tokenSource = identity.Source.String()
	}
	tokenSource = safeCredentialLabel(tokenSource, "unknown")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "credential-active: %s token-source=%s\n", active, tokenSource)
}

func printTransportDiagnostics(cmd *cobra.Command, snapshots ...internaltransport.Diagnostics) {
	if len(snapshots) > 0 {
		snapshot := snapshots[0]
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "transport: %s status=%s", snapshot.Provider, snapshot.Status)
		if snapshot.FailureStage != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " failure-stage=%s", snapshot.FailureStage)
		}
		if snapshot.Status == "active" && snapshot.Provider != "built-in" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " hook-timeout=%s tls-downgrade=blocked redirects=%d", internaltransport.DefaultTimeout, internaltransport.MaxRedirects)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		return
	}
	provider := transport.GetProvider()
	if provider == nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "transport: built-in")
		return
	}
	name, _ := internaltransport.ResolveProviderName(cmd.Context(), provider)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "transport: %s hook-timeout=%s tls-downgrade=blocked redirects=%d\n",
		name, internaltransport.DefaultTimeout, internaltransport.MaxRedirects)
}

func printPluginDiagnostics(cmd *cobra.Command, diagnostics platformruntime.Diagnostics) {
	if len(diagnostics.Plugins) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "plugin: none")
		return
	}
	for _, plugin := range diagnostics.Plugins {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "plugin: %s version=%s status=%s policy=%s restricts=%t",
			plugin.Name, plugin.Version, plugin.Status, plugin.FailurePolicy, plugin.Restricts)
		if plugin.FailureStage != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " failure-stage=%s", plugin.FailureStage)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		for _, hook := range plugin.Hooks {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  hook: %s kind=%s timing=%s selector=%s\n",
				hook.Name, hook.Kind, hook.Timing, hook.Selector)
		}
		for _, rule := range plugin.Rules {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  rule: %s\n", rule)
		}
	}
}

func printPolicyDiagnostics(cmd *cobra.Command, diagnostics platformruntime.Diagnostics) {
	owner := diagnostics.Restrictor
	if owner == "" {
		owner = "none"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "policy: %s\n", owner)
	for _, plugin := range diagnostics.Plugins {
		for _, rule := range plugin.RuleDetails {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  rule: %s allow=%s deny=%s max-risk=%s identities=%s allow-unannotated=%t\n",
				rule.Name, diagnosticList(rule.Allow), diagnosticList(rule.Deny), diagnosticValue(rule.MaxRisk), diagnosticList(rule.Identities), rule.AllowUnannotated)
		}
	}
}

func printDynamicToolDiagnostics(cmd *cobra.Command, issues []ToolMappingIssue) {
	if len(issues) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "dynamic-tool: all accepted")
		return
	}
	for _, issue := range issues {
		// The stable code/path/tool tuple is sufficient and avoids reflecting
		// untrusted free-form validation details in CLI diagnostics.
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dynamic-tool: %q path=%q status=skipped code=%s\n",
			issue.ToolName, issue.Path, issue.Code)
	}
}

func diagnosticList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ",")
}

func diagnosticValue(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
