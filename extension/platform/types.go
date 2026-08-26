// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package platform defines the public command-governance extension seam.
package platform

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

type FailurePolicy int

const (
	FailOpen FailurePolicy = iota
	FailClosed
)

type Capabilities struct {
	RequiredCLIVersion string
	// Restricts declares that Install registers command restriction rules. A
	// restricting plugin must use FailClosed; a contradictory hand-written
	// declaration fails CLI startup before Install is called.
	Restricts     bool
	FailurePolicy FailurePolicy
}

type When int

const (
	Before When = iota
	After
)

type LifecycleEvent int

const (
	Startup LifecycleEvent = iota
	Shutdown
)

type LifecycleContext struct {
	Event LifecycleEvent
	Err   error
}

type Risk string

const (
	RiskRead          Risk = "read"
	RiskWrite         Risk = "write"
	RiskHighRiskWrite Risk = "high-risk-write"
)

func (r Risk) Rank() (int, bool) {
	switch r {
	case RiskRead:
		return 0, true
	case RiskWrite:
		return 1, true
	case RiskHighRiskWrite:
		return 2, true
	default:
		return 0, false
	}
}

func (r Risk) String() string { return string(r) }

func ParseRisk(value string) (Risk, error) {
	if value == "" {
		return "", nil
	}
	risk := Risk(value)
	if _, ok := risk.Rank(); !ok {
		return "", fmt.Errorf("invalid risk %q: must be read|write|high-risk-write", value)
	}
	return risk, nil
}

type Identity string

const (
	IdentityUser Identity = "user"
	IdentityBot  Identity = "bot"
)

// Rule is one restriction constraint. When one plugin registers multiple
// rules, a command must satisfy every rule; Deny patterns are global and take
// precedence over all Allow patterns.
type Rule struct {
	Name             string
	Description      string
	Allow            []string
	Deny             []string
	MaxRisk          Risk
	Identities       []Identity
	AllowUnannotated bool
}

type CommandView interface {
	Path() string
	Domain() string
	Risk() (Risk, bool)
	Identities() []Identity
	Annotation(key string) (string, bool)
}

type Invocation interface {
	Cmd() CommandView
	Args() []string
	Started() time.Time
	Err() error
	DeniedByPolicy() bool
	DenialLayer() string
	DenialPolicySource() string
}

type Handler func(ctx context.Context, invocation Invocation) error
type Observer func(ctx context.Context, invocation Invocation)

// Wrapper decorates one command handler. A wrapper that returns nil must call
// next before returning; it may call next at most once. A non-nil error from
// next remains the command result even if the wrapper ignores it and returns
// nil. To stop execution deliberately, return a non-nil error such as
// AbortError instead.
type Wrapper func(next Handler) Handler

// LifecycleHandler receives a callback Context with a runtime deadline.
// Startup callbacks have an independent bounded window; Shutdown callbacks
// share the process cleanup window and run in reverse registration order.
type LifecycleHandler func(ctx context.Context, lifecycle *LifecycleContext) error
type Selector func(command CommandView) bool

func All() Selector  { return func(CommandView) bool { return true } }
func None() Selector { return func(CommandView) bool { return false } }

func ByDomain(domains ...string) Selector {
	wanted := make(map[string]bool, len(domains))
	for _, domain := range domains {
		wanted[domain] = true
	}
	return func(command CommandView) bool { return command != nil && wanted[command.Domain()] }
}

func ByCommandPath(patterns ...string) Selector {
	return func(command CommandView) bool {
		if command == nil {
			return false
		}
		for _, pattern := range patterns {
			if matchCommandPath(pattern, command.Path()) {
				return true
			}
		}
		return false
	}
}

func ByIdentity(identity Identity) Selector {
	return func(command CommandView) bool {
		if command == nil {
			return false
		}
		for _, candidate := range command.Identities() {
			if candidate == identity {
				return true
			}
		}
		return false
	}
}

func ByExactRisk(risk Risk) Selector {
	return func(command CommandView) bool {
		if command == nil {
			return false
		}
		actual, ok := command.Risk()
		return ok && actual == risk
	}
}

func ByReadOnly() Selector { return ByExactRisk(RiskRead) }

func ByWrite() Selector {
	return func(command CommandView) bool {
		if command == nil {
			return false
		}
		risk, ok := command.Risk()
		return ok && (risk == RiskWrite || risk == RiskHighRiskWrite)
	}
}

func (selector Selector) And(other Selector) Selector {
	return func(command CommandView) bool { return matches(selector, command) && matches(other, command) }
}

func (selector Selector) Or(other Selector) Selector {
	return func(command CommandView) bool { return matches(selector, command) || matches(other, command) }
}

func (selector Selector) Not() Selector {
	return func(command CommandView) bool { return !matches(selector, command) }
}

func matches(selector Selector, command CommandView) bool {
	return selector != nil && selector(command)
}

func matchCommandPath(pattern, value string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	valueParts := strings.Split(strings.Trim(value, "/"), "/")
	var match func(int, int) bool
	match = func(patternIndex, valueIndex int) bool {
		if patternIndex == len(patternParts) {
			return valueIndex == len(valueParts)
		}
		if patternParts[patternIndex] == "**" {
			return match(patternIndex+1, valueIndex) ||
				(valueIndex < len(valueParts) && match(patternIndex, valueIndex+1))
		}
		if valueIndex == len(valueParts) {
			return false
		}
		matched, err := path.Match(patternParts[patternIndex], valueParts[valueIndex])
		return err == nil && matched && match(patternIndex+1, valueIndex+1)
	}
	return match(0, 0)
}

type AbortError struct {
	HookName string
	Reason   string
	Cause    error
	Detail   any
}

func (e *AbortError) Error() string {
	if e == nil {
		return "CLI extension aborted"
	}
	message := fmt.Sprintf("hook %q aborted: %s", e.HookName, e.Reason)
	return message
}

func (e *AbortError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorPayload provides a stable structured rejection without serializing the
// optional internal cause supplied by an enterprise wrapper.
func (e *AbortError) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"code":      "CLIENT_EXTENSION_ABORTED",
		"message":   e.Error(),
		"retryable": false,
		"detail": map[string]any{
			"hook":   e.HookName,
			"reason": e.Reason,
		},
	}
}

type CommandDeniedError struct {
	Path         string
	Layer        string
	PolicySource string
	RuleName     string
	ReasonCode   string
	Reason       string
}

func (e *CommandDeniedError) Error() string {
	return fmt.Sprintf("command %q denied: %s", e.Path, e.Reason)
}

// ErrorPayload provides the stable structured error contract used by CLI
// formatters without coupling this public extension package to a product-only
// error type.
func (e *CommandDeniedError) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"code":      "CLIENT_COMMAND_DENIED",
		"message":   e.Error(),
		"retryable": false,
		"detail": map[string]any{
			"path":          e.Path,
			"layer":         e.Layer,
			"policy_source": e.PolicySource,
			"rule_name":     e.RuleName,
			"reason_code":   e.ReasonCode,
			"reason":        e.Reason,
		},
	}
}
