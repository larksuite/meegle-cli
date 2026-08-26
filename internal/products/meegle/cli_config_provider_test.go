// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
)

type fakeCLIConfigSource struct {
	config *cliapiclient.CLIConfig
	err    error
	calls  int
}

func (s *fakeCLIConfigSource) Config(context.Context) (*cliapiclient.CLIConfig, error) {
	s.calls++
	return s.config, s.err
}

type fakeCLIConfigStore struct {
	result     *CLIConfigCacheResult
	getErr     error
	setErr     error
	clearErr   error
	getCalls   int
	setCalls   int
	clearCalls int
	setLast    *cliapiclient.CLIConfig
}

func (s *fakeCLIConfigStore) Get() (*CLIConfigCacheResult, error) {
	s.getCalls++
	return s.result, s.getErr
}

func (s *fakeCLIConfigStore) Set(config *cliapiclient.CLIConfig) error {
	s.setCalls++
	s.setLast = config
	return s.setErr
}

func (s *fakeCLIConfigStore) Clear() error {
	s.clearCalls++
	return s.clearErr
}

func TestCLIConfigProviderReturnsFreshCache(t *testing.T) {
	cached := &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}
	source := &fakeCLIConfigSource{config: &cliapiclient.CLIConfig{}}
	store := &fakeCLIConfigStore{result: &CLIConfigCacheResult{Config: cached}}
	provider := NewCLIConfigProvider(source, store)

	got, err := provider.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != cached {
		t.Fatalf("Get() = %#v, want cached snapshot", got)
	}
	if source.calls != 0 || store.setCalls != 0 || store.clearCalls != 0 {
		t.Fatalf("fresh hit touched source/cache unexpectedly: source=%d set=%d clear=%d",
			source.calls, store.setCalls, store.clearCalls)
	}
}

func TestCLIConfigProviderRefreshesStaleCache(t *testing.T) {
	fresh := &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}
	source := &fakeCLIConfigSource{config: fresh}
	store := &fakeCLIConfigStore{result: &CLIConfigCacheResult{
		Config: &cliapiclient.CLIConfig{},
		Stale:  true,
	}}
	provider := NewCLIConfigProvider(source, store)

	got, err := provider.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != fresh || source.calls != 1 || store.setCalls != 1 || store.setLast != fresh {
		t.Fatalf("refresh = %#v, source=%d set=%d setLast=%#v", got, source.calls, store.setCalls, store.setLast)
	}
	if store.clearCalls != 0 {
		t.Fatalf("Get() must not encode invalidation policy, clearCalls = %d", store.clearCalls)
	}
}

func TestCLIConfigProviderTreatsCacheFailureAsMiss(t *testing.T) {
	fresh := &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}
	source := &fakeCLIConfigSource{config: fresh}
	store := &fakeCLIConfigStore{getErr: errors.New("cache read failed"), setErr: errors.New("cache write failed")}
	provider := NewCLIConfigProvider(source, store)

	got, err := provider.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != fresh || source.calls != 1 || store.setCalls != 1 {
		t.Fatalf("Get() = %#v, source=%d set=%d", got, source.calls, store.setCalls)
	}
}

func TestCLIConfigProviderDoesNotCacheFailedOrEmptyResponse(t *testing.T) {
	t.Run("source error", func(t *testing.T) {
		wantErr := errors.New("source failed")
		store := &fakeCLIConfigStore{}
		provider := NewCLIConfigProvider(&fakeCLIConfigSource{err: wantErr}, store)

		if _, err := provider.Get(context.Background()); !errors.Is(err, wantErr) {
			t.Fatalf("Get() error = %v, want %v", err, wantErr)
		}
		if store.setCalls != 0 {
			t.Fatalf("failed response should not be cached, setCalls = %d", store.setCalls)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		store := &fakeCLIConfigStore{}
		provider := NewCLIConfigProvider(&fakeCLIConfigSource{}, store)

		got, err := provider.Get(context.Background())
		if err != nil || got != nil {
			t.Fatalf("Get() = %#v, %v; want nil, nil", got, err)
		}
		if store.setCalls != 0 {
			t.Fatalf("empty response should not be cached, setCalls = %d", store.setCalls)
		}
	})
}

func TestCLIConfigProviderClearIsExplicit(t *testing.T) {
	wantErr := errors.New("clear failed")
	store := &fakeCLIConfigStore{clearErr: wantErr}
	provider := NewCLIConfigProvider(nil, store)

	if err := provider.Clear(); !errors.Is(err, wantErr) {
		t.Fatalf("Clear() error = %v, want %v", err, wantErr)
	}
	if store.clearCalls != 1 {
		t.Fatalf("Clear() calls = %d, want 1", store.clearCalls)
	}
}
