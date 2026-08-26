// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"fmt"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
)

func (r *Runtime) denial(command platformapi.CommandView) *platformapi.CommandDeniedError {
	if r == nil || len(r.rules) == 0 {
		return nil
	}
	reasonCode := "not_allowed"
	reason := "command is not allowed by the active policy"
	ruleName := r.rules[0].rule.Name
	// Deny patterns are global within the single restriction owner. They must
	// not be bypassable merely because a different rule's allow-list matches.
	for _, entry := range r.rules {
		for _, pattern := range entry.rule.Deny {
			if platformapi.ByCommandPath(pattern)(command) {
				return &platformapi.CommandDeniedError{
					Path: command.Path(), Layer: "policy", PolicySource: "plugin:" + r.restrictor,
					RuleName: entry.rule.Name, ReasonCode: "path_denied",
					Reason: fmt.Sprintf("path %q matches deny pattern %q", command.Path(), pattern),
				}
			}
		}
	}
	allAllowed := true
	for _, entry := range r.rules {
		allowed, code, detail := evaluateRule(entry.rule, command)
		if !allowed {
			allAllowed = false
		}
		if code != "" {
			ruleName, reasonCode, reason = entry.rule.Name, code, detail
		}
	}
	if allAllowed {
		return nil
	}
	return &platformapi.CommandDeniedError{
		Path:         command.Path(),
		Layer:        "policy",
		PolicySource: "plugin:" + r.restrictor,
		RuleName:     ruleName,
		ReasonCode:   reasonCode,
		Reason:       reason,
	}
}

func evaluateRule(rule *platformapi.Rule, command platformapi.CommandView) (bool, string, string) {
	if rule == nil || command == nil {
		return false, "invalid_rule", "policy rule is invalid"
	}
	pathValue := command.Path()
	for _, pattern := range rule.Deny {
		if platformapi.ByCommandPath(pattern)(command) {
			return false, "path_denied", fmt.Sprintf("path %q matches deny pattern %q", pathValue, pattern)
		}
	}
	if len(rule.Allow) > 0 {
		matched := false
		for _, pattern := range rule.Allow {
			if platformapi.ByCommandPath(pattern)(command) {
				matched = true
				break
			}
		}
		if !matched {
			return false, "path_not_allowed", fmt.Sprintf("path %q is outside the policy allow list", pathValue)
		}
	}
	if rule.MaxRisk != "" {
		actual, annotated := command.Risk()
		if !annotated {
			if !rule.AllowUnannotated {
				return false, "risk_not_annotated", "command risk is not annotated"
			}
		} else {
			actualRank, actualOK := actual.Rank()
			maxRank, maxOK := rule.MaxRisk.Rank()
			if !actualOK || !maxOK || actualRank > maxRank {
				return false, "risk_too_high", fmt.Sprintf("command risk %q exceeds maximum %q", actual, rule.MaxRisk)
			}
		}
	}
	if len(rule.Identities) > 0 {
		allowedIdentity := false
		for _, actual := range command.Identities() {
			for _, wanted := range rule.Identities {
				if actual == wanted {
					allowedIdentity = true
				}
			}
		}
		if !allowedIdentity {
			return false, "identity_not_supported", "command identity is outside the policy allow list"
		}
	}
	return true, "", ""
}
