// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"reflect"
	"runtime"
	"strings"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
)

// HookDiagnostic is the non-secret description of one installed hook.
type HookDiagnostic struct {
	Name     string
	Kind     string
	Timing   string
	Selector string
}

// PluginDiagnostic describes whether a compiled plugin became active. It
// intentionally stores a stable failure stage instead of an arbitrary plugin
// error string, which may contain credentials or request data.
type PluginDiagnostic struct {
	Name          string
	Version       string
	Status        string
	FailurePolicy string
	FailureStage  string
	Restricts     bool
	Hooks         []HookDiagnostic
	Rules         []string
	RuleDetails   []RuleDiagnostic
}

// RuleDiagnostic is the non-secret configuration of one installed policy
// rule. It deliberately excludes runtime command arguments and provider errors.
type RuleDiagnostic struct {
	Name             string
	Allow            []string
	Deny             []string
	MaxRisk          string
	Identities       []string
	AllowUnannotated bool
}

// Diagnostics is an immutable, non-secret snapshot of platform installation.
type Diagnostics struct {
	Plugins    []PluginDiagnostic
	Restrictor string
}

func (r *Runtime) Diagnostics() Diagnostics {
	if r == nil {
		return Diagnostics{}
	}
	out := Diagnostics{Restrictor: r.restrictor, Plugins: make([]PluginDiagnostic, len(r.pluginDiagnostics))}
	for index, plugin := range r.pluginDiagnostics {
		out.Plugins[index] = plugin
		out.Plugins[index].Hooks = append([]HookDiagnostic(nil), plugin.Hooks...)
		out.Plugins[index].Rules = append([]string(nil), plugin.Rules...)
		out.Plugins[index].RuleDetails = cloneRuleDiagnostics(plugin.RuleDetails)
	}
	return out
}

func failurePolicyName(policy platformapi.FailurePolicy) string {
	if policy == platformapi.FailClosed {
		return "fail-closed"
	}
	return "fail-open"
}

func activePluginDiagnostic(name, version string, caps platformapi.Capabilities, stage *stagingRegistrar) PluginDiagnostic {
	diagnostic := PluginDiagnostic{
		Name: name, Version: version, Status: "active",
		FailurePolicy: failurePolicyName(caps.FailurePolicy), Restricts: caps.Restricts,
	}
	for _, entry := range stage.observers {
		timing := "before"
		if entry.when == platformapi.After {
			timing = "after"
		}
		diagnostic.Hooks = append(diagnostic.Hooks, HookDiagnostic{
			Name: entry.name, Kind: "observer", Timing: timing, Selector: selectorName(entry.selector),
		})
	}
	for _, entry := range stage.wrappers {
		diagnostic.Hooks = append(diagnostic.Hooks, HookDiagnostic{
			Name: entry.name, Kind: "wrapper", Selector: selectorName(entry.selector),
		})
	}
	for _, entry := range stage.lifecycles {
		timing := "startup"
		if entry.event == platformapi.Shutdown {
			timing = "shutdown"
		}
		diagnostic.Hooks = append(diagnostic.Hooks, HookDiagnostic{
			Name: entry.name, Kind: "lifecycle", Timing: timing, Selector: "n/a",
		})
	}
	for _, entry := range stage.rules {
		diagnostic.Rules = append(diagnostic.Rules, entry.rule.Name)
		detail := RuleDiagnostic{
			Name: entry.rule.Name, Allow: append([]string(nil), entry.rule.Allow...), Deny: append([]string(nil), entry.rule.Deny...),
			MaxRisk: entry.rule.MaxRisk.String(), AllowUnannotated: entry.rule.AllowUnannotated,
		}
		for _, identity := range entry.rule.Identities {
			detail.Identities = append(detail.Identities, string(identity))
		}
		diagnostic.RuleDetails = append(diagnostic.RuleDetails, detail)
	}
	return diagnostic
}

func cloneRuleDiagnostics(input []RuleDiagnostic) []RuleDiagnostic {
	output := make([]RuleDiagnostic, len(input))
	for index, rule := range input {
		output[index] = rule
		output[index].Allow = append([]string(nil), rule.Allow...)
		output[index].Deny = append([]string(nil), rule.Deny...)
		output[index].Identities = append([]string(nil), rule.Identities...)
	}
	return output
}

func selectorName(selector platformapi.Selector) string {
	if selector == nil {
		return "none"
	}
	function := runtime.FuncForPC(reflect.ValueOf(selector).Pointer())
	if function == nil {
		return "custom"
	}
	name := function.Name()
	for _, candidate := range []struct{ fragment, label string }{
		{".All.", "all"}, {".None.", "none"}, {".ByDomain.", "domain"},
		{".ByCommandPath.", "command-path"}, {".ByIdentity.", "identity"},
		{".ByExactRisk.", "exact-risk"}, {".ByWrite.", "write"},
		{".And.", "and"}, {".Or.", "or"}, {".Not.", "not"},
	} {
		if strings.Contains(name, candidate.fragment) {
			return candidate.label
		}
	}
	return "custom"
}
