// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
)

// CLIConfigSource loads the authoritative CLI configuration from the server.
type CLIConfigSource interface {
	Config(context.Context) (*cliapiclient.CLIConfig, error)
}

// CLIConfigStore is the persistence boundary used by CLIConfigProvider.
type CLIConfigStore interface {
	Get() (*CLIConfigCacheResult, error)
	Set(*cliapiclient.CLIConfig) error
	Clear() error
}

// CLIConfigProvider provides read-through access to the server-side CLI
// configuration. It owns cache lookup and refresh, but not cache invalidation
// policy: business commands decide when to call Clear.
type CLIConfigProvider struct {
	source CLIConfigSource
	cache  CLIConfigStore
}

func NewCLIConfigProvider(source CLIConfigSource, cache CLIConfigStore) *CLIConfigProvider {
	return &CLIConfigProvider{source: source, cache: cache}
}

// Get returns a fresh cached snapshot when available and otherwise refreshes it
// from the server. Cache failures are best-effort and never mask a server result.
func (p *CLIConfigProvider) Get(ctx context.Context) (*cliapiclient.CLIConfig, error) {
	if p == nil {
		return nil, errors.New("CLI config provider is not configured")
	}
	if p.cache != nil {
		if cached, _ := p.cache.Get(); cached != nil && !cached.Stale && cached.Config != nil {
			return cached.Config, nil
		}
	}
	if p.source == nil {
		return nil, errors.New("CLI config source is not configured")
	}
	config, err := p.source.Config(ctx)
	if err != nil {
		return nil, err
	}
	if config != nil && p.cache != nil {
		_ = p.cache.Set(config)
	}
	return config, nil
}

// Clear removes the cached snapshot. The provider deliberately does not decide
// when to call it; invalidation timing belongs to the invoking business command.
func (p *CLIConfigProvider) Clear() error {
	if p == nil || p.cache == nil {
		return nil
	}
	return p.cache.Clear()
}
