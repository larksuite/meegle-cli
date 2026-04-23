// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type RegistryManager struct {
	setup     RegistrySetup
	current   atomic.Pointer[Registry]
	onRebuild []func(*Registry)
	mu        sync.Mutex
}

func NewManager(setup RegistrySetup) *RegistryManager {
	return &RegistryManager{setup: setup}
}

func (m *RegistryManager) Init(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("registry manager is nil")
	}
	return m.Rebuild(ctx)
}

func (m *RegistryManager) Rebuild(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("registry manager is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setup == nil {
		return fmt.Errorf("registry setup is not configured")
	}
	tree, err := m.setup.Setup(ctx)
	if err != nil {
		return err
	}
	reg, err := New(tree)
	if err != nil {
		return err
	}
	m.current.Store(reg)
	for _, fn := range m.onRebuild {
		if fn != nil {
			fn(reg)
		}
	}
	return nil
}

func (m *RegistryManager) Registry() *Registry {
	if m == nil {
		return nil
	}
	return m.current.Load()
}

func (m *RegistryManager) OnRebuild(fn func(*Registry)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRebuild = append(m.onRebuild, fn)
}
