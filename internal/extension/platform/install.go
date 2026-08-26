// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

var registrationNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

const DefaultCallbackTimeout = 2 * time.Second

// Install installs all globally registered plugins. Each plugin writes to a
// staging registrar; a failed install commits nothing from that plugin.
func Install(version string) (*Runtime, error) {
	return installPlugins(version, platformapi.RegisteredPlugins())
}

func installPlugins(version string, plugins []platformapi.Plugin) (*Runtime, error) {
	return installPluginsWithTimeout(version, plugins, DefaultCallbackTimeout)
}

func installPluginsWithTimeout(version string, plugins []platformapi.Plugin, timeout time.Duration) (*Runtime, error) {
	runtime := &Runtime{}
	seenPlugins := make(map[string]bool, len(plugins))
	for _, plugin := range plugins {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := installOnePlugin(ctx, version, plugin, runtime, seenPlugins)
		cancel()
		if err != nil {
			return nil, err
		}
	}
	return runtime, nil
}

func installOnePlugin(ctx context.Context, version string, plugin platformapi.Plugin, runtime *Runtime, seenPlugins map[string]bool) error {
	if plugin == nil {
		return fmt.Errorf("platform plugin is nil")
	}
	caps, metadataErr := callPluginMetadataBounded(ctx, "capabilities", plugin.Capabilities)
	if metadataErr != nil {
		return metadataErr
	}
	if caps.FailurePolicy != platformapi.FailOpen && caps.FailurePolicy != platformapi.FailClosed {
		return fmt.Errorf("plugin has invalid failure policy %d", caps.FailurePolicy)
	}
	if caps.Restricts && caps.FailurePolicy == platformapi.FailOpen {
		return errors.New("restricting platform plugin must be fail-closed")
	}
	diagnostic := PluginDiagnostic{FailurePolicy: failurePolicyName(caps.FailurePolicy)}
	skip := func(stage string) {
		diagnostic.Status = "skipped"
		diagnostic.FailureStage = stage
		runtime.pluginDiagnostics = append(runtime.pluginDiagnostics, diagnostic)
	}
	pluginName, metadataErr := callPluginMetadataBounded(ctx, "name", plugin.Name)
	if metadataErr != nil {
		if caps.FailurePolicy == platformapi.FailOpen {
			logHookFailure("plugin-metadata", metadataErr)
			diagnostic.Name = "<unavailable>"
			skip("metadata-name")
			return nil
		}
		return metadataErr
	}
	diagnostic.Name = pluginName
	pluginVersion, metadataErr := callPluginMetadataBounded(ctx, "version", plugin.Version)
	if metadataErr != nil {
		if caps.FailurePolicy == platformapi.FailOpen {
			logHookFailure(pluginName, metadataErr)
			skip("metadata-version")
			return nil
		}
		return metadataErr
	}
	diagnostic.Version = pluginVersion
	diagnostic.Restricts = caps.Restricts
	if seenPlugins[pluginName] {
		return fmt.Errorf("platform plugin name %q is registered more than once", pluginName)
	}
	seenPlugins[pluginName] = true
	if !registrationNamePattern.MatchString(pluginName) {
		if caps.FailurePolicy == platformapi.FailOpen {
			skip("validation-name")
			return nil
		}
		return fmt.Errorf("invalid platform plugin name %q", pluginName)
	}
	if _, err := parseSemanticVersion(pluginVersion); err != nil {
		if caps.FailurePolicy == platformapi.FailOpen {
			skip("validation-version")
			return nil
		}
		return fmt.Errorf("plugin %q has invalid version %q: %w", pluginName, pluginVersion, err)
	}
	if err := checkCLIVersion(caps.RequiredCLIVersion, version); err != nil {
		if caps.FailurePolicy == platformapi.FailOpen {
			skip("compatibility")
			return nil
		}
		return fmt.Errorf("plugin %q is incompatible: %w", pluginName, err)
	}
	stage := &stagingRegistrar{plugin: pluginName, policy: caps.FailurePolicy}
	err := installPluginBounded(ctx, pluginName, plugin, stage)
	stage.freeze()
	if err == nil && len(stage.errs) > 0 {
		err = fmt.Errorf("plugin %q registration is invalid: %w", pluginName, errors.Join(stage.errs...))
	}
	if err == nil && caps.Restricts != (len(stage.rules) > 0) {
		err = fmt.Errorf("plugin %q restricts declaration does not match Install", pluginName)
	}
	if err == nil && len(stage.rules) > 0 && runtime.restrictor != "" {
		err = fmt.Errorf("plugins %q and %q both register Restrict", runtime.restrictor, pluginName)
	}
	if err != nil {
		if caps.FailurePolicy == platformapi.FailOpen {
			skip("install")
			return nil
		}
		return err
	}
	if len(stage.rules) > 0 {
		runtime.restrictor = pluginName
	}
	runtime.observers = append(runtime.observers, stage.observers...)
	runtime.wrappers = append(runtime.wrappers, stage.wrappers...)
	runtime.lifecycles = append(runtime.lifecycles, stage.lifecycles...)
	runtime.rules = append(runtime.rules, stage.rules...)
	runtime.pluginDiagnostics = append(runtime.pluginDiagnostics, activePluginDiagnostic(pluginName, pluginVersion, caps, stage))
	return nil
}

type metadataResult[T any] struct {
	value T
	err   error
}

func callPluginMetadataBounded[T any](ctx context.Context, field string, call func() T) (value T, err error) {
	result := make(chan metadataResult[T], 1)
	go func() {
		value, callErr := callPluginMetadata(field, call)
		result <- metadataResult[T]{value: value, err: callErr}
	}()
	select {
	case value := <-result:
		return value.value, value.err
	case <-ctx.Done():
		return value, runtimeFailure("plugin-metadata", "metadata-"+field, ctx.Err())
	}
}

func callPluginMetadata[T any](field string, call func() T) (value T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = runtimeFailure("plugin-metadata", "metadata-"+field, panicCause(recovered))
		}
	}()
	return call(), nil
}

func installPlugin(pluginName string, plugin platformapi.Plugin, registrar platformapi.Registrar) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("plugin %q install panicked: %w", pluginName, panicCause(recovered))
		}
	}()
	if err := plugin.Install(registrar); err != nil {
		return fmt.Errorf("plugin %q install failed: %w", pluginName, frameworkerrors.GuardCause(err))
	}
	return nil
}

func installPluginBounded(ctx context.Context, pluginName string, plugin platformapi.Plugin, registrar *stagingRegistrar) error {
	result := make(chan error, 1)
	go func() {
		result <- installPlugin(pluginName, plugin, registrar)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		registrar.freeze()
		return runtimeFailure(pluginName, "install", ctx.Err())
	}
}

type stagingRegistrar struct {
	mu         sync.Mutex
	frozen     bool
	plugin     string
	policy     platformapi.FailurePolicy
	observers  []observerEntry
	wrappers   []wrapperEntry
	lifecycles []lifecycleEntry
	rules      []ruleEntry
	hookNames  map[string]bool
	errs       []error
}

func (r *stagingRegistrar) freeze() {
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

func (r *stagingRegistrar) beginRegistration() bool {
	r.mu.Lock()
	if r.frozen {
		r.mu.Unlock()
		return false
	}
	return true
}

func (r *stagingRegistrar) endRegistration() { r.mu.Unlock() }

func (r *stagingRegistrar) Observe(when platformapi.When, name string, selector platformapi.Selector, observer platformapi.Observer) {
	if !r.beginRegistration() {
		return
	}
	defer r.endRegistration()
	if !r.validateHook(name, selector != nil, observer != nil) {
		return
	}
	if when != platformapi.Before && when != platformapi.After {
		r.errs = append(r.errs, fmt.Errorf("observer %q has invalid timing", name))
		return
	}
	r.observers = append(r.observers, observerEntry{
		name: r.plugin + "." + name, when: when, selector: selector, observer: observer, policy: r.policy,
	})
}

func (r *stagingRegistrar) Wrap(name string, selector platformapi.Selector, wrapper platformapi.Wrapper) {
	if !r.beginRegistration() {
		return
	}
	defer r.endRegistration()
	if !r.validateHook(name, selector != nil, wrapper != nil) {
		return
	}
	r.wrappers = append(r.wrappers, wrapperEntry{
		name: r.plugin + "." + name, selector: selector, wrapper: wrapper, policy: r.policy,
	})
}

func (r *stagingRegistrar) On(event platformapi.LifecycleEvent, name string, handler platformapi.LifecycleHandler) {
	if !r.beginRegistration() {
		return
	}
	defer r.endRegistration()
	if !r.validateHook(name, true, handler != nil) {
		return
	}
	if event != platformapi.Startup && event != platformapi.Shutdown {
		r.errs = append(r.errs, fmt.Errorf("lifecycle %q has invalid event", name))
		return
	}
	r.lifecycles = append(r.lifecycles, lifecycleEntry{
		name: r.plugin + "." + name, event: event, handler: handler, policy: r.policy,
	})
}

func (r *stagingRegistrar) Restrict(rule *platformapi.Rule) {
	if !r.beginRegistration() {
		return
	}
	defer r.endRegistration()
	if rule == nil {
		r.errs = append(r.errs, errors.New("restriction rule must not be nil"))
		return
	}
	if !registrationNamePattern.MatchString(rule.Name) {
		r.errs = append(r.errs, fmt.Errorf("invalid restriction rule name %q", rule.Name))
		return
	}
	if rule.MaxRisk != "" {
		if _, err := platformapi.ParseRisk(rule.MaxRisk.String()); err != nil {
			r.errs = append(r.errs, fmt.Errorf("rule %q: %w", rule.Name, err))
			return
		}
	}
	for _, pattern := range append(append([]string(nil), rule.Allow...), rule.Deny...) {
		for _, segment := range strings.Split(pattern, "/") {
			if segment == "**" {
				continue
			}
			if _, err := path.Match(segment, "probe"); err != nil {
				r.errs = append(r.errs, fmt.Errorf("rule %q has invalid path pattern %q: %w", rule.Name, pattern, err))
				return
			}
		}
	}
	clone := *rule
	clone.Allow = append([]string(nil), rule.Allow...)
	clone.Deny = append([]string(nil), rule.Deny...)
	clone.Identities = append([]platformapi.Identity(nil), rule.Identities...)
	r.rules = append(r.rules, ruleEntry{plugin: r.plugin, rule: &clone})
}

func (r *stagingRegistrar) validateHook(name string, selectorValid, callbackValid bool) bool {
	valid := true
	if !registrationNamePattern.MatchString(name) {
		r.errs = append(r.errs, fmt.Errorf("invalid hook name %q", name))
		valid = false
	}
	if r.hookNames == nil {
		r.hookNames = map[string]bool{}
	}
	if r.hookNames[name] {
		r.errs = append(r.errs, fmt.Errorf("hook name %q already used", name))
		valid = false
	}
	r.hookNames[name] = true
	if !selectorValid {
		r.errs = append(r.errs, fmt.Errorf("hook %q selector must not be nil", name))
		valid = false
	}
	if !callbackValid {
		r.errs = append(r.errs, fmt.Errorf("hook %q callback must not be nil", name))
		valid = false
	}
	return valid
}
