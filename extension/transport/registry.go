// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport

import (
	"reflect"
	"sync"
)

var transportRegistry struct {
	sync.Mutex
	provider Provider
}

// Register installs a process-wide provider. A later registration replaces an
// earlier one so an embedding binary has one unambiguous network policy.
func Register(provider Provider) {
	if isNilProvider(provider) {
		return
	}
	transportRegistry.Lock()
	defer transportRegistry.Unlock()
	transportRegistry.provider = provider
}

func isNilProvider(provider Provider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// GetProvider returns the active provider, or nil when none was registered.
func GetProvider() Provider {
	transportRegistry.Lock()
	defer transportRegistry.Unlock()
	return transportRegistry.provider
}
