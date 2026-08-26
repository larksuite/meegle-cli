// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import "sync"

// Plugin describes one build-time CLI extension. Metadata and Install must be
// local, deterministic work: each plugin gets a bounded installation window.
// If Install returns after that window, late Registrar calls are ignored.
type Plugin interface {
	Name() string
	Version() string
	Capabilities() Capabilities
	Install(registrar Registrar) error
}

type Registrar interface {
	Observe(when When, hookName string, selector Selector, observer Observer)
	Wrap(hookName string, selector Selector, wrapper Wrapper)
	On(event LifecycleEvent, hookName string, handler LifecycleHandler)
	Restrict(rule *Rule)
}

var pluginRegistry struct {
	sync.Mutex
	plugins []Plugin
}

func Register(plugin Plugin) {
	pluginRegistry.Lock()
	defer pluginRegistry.Unlock()
	pluginRegistry.plugins = append(pluginRegistry.plugins, plugin)
}

func RegisteredPlugins() []Plugin {
	pluginRegistry.Lock()
	defer pluginRegistry.Unlock()
	return append([]Plugin(nil), pluginRegistry.plugins...)
}
