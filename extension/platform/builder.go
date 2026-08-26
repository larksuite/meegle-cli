// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"errors"
	"fmt"
	"regexp"
)

var extensionNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var pluginVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

type Builder struct {
	name      string
	version   string
	caps      Capabilities
	actions   []func(Registrar)
	rules     []*Rule
	hookNames map[string]bool
	errs      []error
}

func NewPlugin(name, version string) *Builder {
	builder := &Builder{name: name, version: version, hookNames: map[string]bool{}}
	if !extensionNamePattern.MatchString(name) {
		builder.errs = append(builder.errs, fmt.Errorf("invalid plugin name %q", name))
	}
	if !pluginVersionPattern.MatchString(version) {
		builder.errs = append(builder.errs, fmt.Errorf("invalid plugin version %q", version))
	}
	return builder
}

func (b *Builder) RequireCLI(constraint string) *Builder {
	b.caps.RequiredCLIVersion = constraint
	return b
}

func (b *Builder) FailOpen() *Builder {
	b.caps.FailurePolicy = FailOpen
	return b
}

func (b *Builder) FailClosed() *Builder {
	b.caps.FailurePolicy = FailClosed
	return b
}

func (b *Builder) Observer(when When, name string, selector Selector, observer Observer) *Builder {
	if !b.validHookName(name) {
		return b
	}
	if selector == nil {
		b.errs = append(b.errs, fmt.Errorf("observer %q selector must not be nil", name))
	}
	if observer == nil {
		b.errs = append(b.errs, fmt.Errorf("observer must not be nil for hook %q", name))
	}
	b.actions = append(b.actions, func(registrar Registrar) {
		registrar.Observe(when, name, selector, observer)
	})
	return b
}

func (b *Builder) Wrap(name string, selector Selector, wrapper Wrapper) *Builder {
	if !b.validHookName(name) {
		return b
	}
	if selector == nil {
		b.errs = append(b.errs, fmt.Errorf("wrapper %q selector must not be nil", name))
	}
	if wrapper == nil {
		b.errs = append(b.errs, fmt.Errorf("wrapper must not be nil for hook %q", name))
	}
	b.actions = append(b.actions, func(registrar Registrar) {
		registrar.Wrap(name, selector, wrapper)
	})
	return b
}

func (b *Builder) On(event LifecycleEvent, name string, handler LifecycleHandler) *Builder {
	if !b.validHookName(name) {
		return b
	}
	if handler == nil {
		b.errs = append(b.errs, fmt.Errorf("lifecycle handler must not be nil for hook %q", name))
	}
	b.actions = append(b.actions, func(registrar Registrar) {
		registrar.On(event, name, handler)
	})
	return b
}

func (b *Builder) Restrict(rule *Rule) *Builder {
	if rule == nil {
		b.errs = append(b.errs, errors.New("Restrict(nil): rule must not be nil"))
		return b
	}
	clone := *rule
	clone.Allow = append([]string(nil), rule.Allow...)
	clone.Deny = append([]string(nil), rule.Deny...)
	clone.Identities = append([]Identity(nil), rule.Identities...)
	b.rules = append(b.rules, &clone)
	if !extensionNamePattern.MatchString(rule.Name) {
		b.errs = append(b.errs, fmt.Errorf("invalid rule name %q", rule.Name))
	}
	if rule.MaxRisk != "" {
		if _, err := ParseRisk(rule.MaxRisk.String()); err != nil {
			b.errs = append(b.errs, fmt.Errorf("rule %q: %w", rule.Name, err))
		}
	}
	b.caps.Restricts = true
	b.caps.FailurePolicy = FailClosed
	return b
}

func (b *Builder) Build() (Plugin, error) {
	if len(b.rules) > 0 && b.caps.FailurePolicy == FailOpen {
		b.errs = append(b.errs, errors.New("Restrict() requires FailClosed"))
	}
	if len(b.errs) > 0 {
		return nil, errors.Join(b.errs...)
	}
	return &builtPlugin{
		name: b.name, version: b.version, caps: b.caps,
		actions: append([]func(Registrar){}, b.actions...),
		rules:   append([]*Rule(nil), b.rules...),
	}, nil
}

func (b *Builder) MustBuild() Plugin {
	plugin, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("plugin %q: %v", b.name, err))
	}
	return plugin
}

func (b *Builder) validHookName(name string) bool {
	if !extensionNamePattern.MatchString(name) {
		b.errs = append(b.errs, fmt.Errorf("invalid hook name %q", name))
		return false
	}
	if b.hookNames[name] {
		b.errs = append(b.errs, fmt.Errorf("hook name %q already used", name))
		return false
	}
	b.hookNames[name] = true
	return true
}

type builtPlugin struct {
	name    string
	version string
	caps    Capabilities
	actions []func(Registrar)
	rules   []*Rule
}

func (p *builtPlugin) Name() string               { return p.name }
func (p *builtPlugin) Version() string            { return p.version }
func (p *builtPlugin) Capabilities() Capabilities { return p.caps }
func (p *builtPlugin) Install(registrar Registrar) error {
	for _, rule := range p.rules {
		registrar.Restrict(rule)
	}
	for _, action := range p.actions {
		action(registrar)
	}
	return nil
}
