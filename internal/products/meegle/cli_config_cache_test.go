// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
)

func TestCLIConfigCacheSetAndGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	want := &cliapiclient.Availability{Available: true}
	if err := cache.Set(&cliapiclient.CLIConfig{HandoffSuggestion: *want}); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	result, err := cache.Get()
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if result == nil || result.Config == nil || !result.Config.HandoffSuggestion.Available {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Stale {
		t.Error("should not be stale")
	}
}

func TestCLIConfigCacheExpired(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", -1)
	if err := cache.Set(&cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	result, _ := cache.Get()
	if result == nil {
		t.Fatal("expected stale result, got nil")
	}
	if !result.Stale {
		t.Error("expected stale=true for expired cache")
	}
}

func TestCLIConfigCacheMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	result, _ := cache.Get()
	if result != nil {
		t.Errorf("expected nil for missing cache, got %#v", result)
	}
}

func TestCLIConfigCacheCorruptIsMiss(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte("not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, _ := cache.Get()
	if result != nil {
		t.Errorf("expected nil for corrupt cache, got %#v", result)
	}
}

func TestCLIConfigCacheSetNilIsNoOp(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	if err := cache.Set(nil); err != nil {
		t.Fatalf("set(nil): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cli-config.json")); !os.IsNotExist(err) {
		t.Errorf("set(nil) should not create a file, stat err = %v", err)
	}
}

func TestCLIConfigCachePerProfile(t *testing.T) {
	dir := t.TempDir()
	alpha := NewCLIConfigCache(dir, "alpha", DefaultCLIConfigTTL)
	beta := NewCLIConfigCache(dir, "beta", DefaultCLIConfigTTL)

	if err := alpha.Set(&cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}); err != nil {
		t.Fatalf("alpha set: %v", err)
	}
	if err := beta.Set(&cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: false}}); err != nil {
		t.Fatalf("beta set: %v", err)
	}

	resultAlpha, _ := alpha.Get()
	resultBeta, _ := beta.Get()
	if resultAlpha == nil || !resultAlpha.Config.HandoffSuggestion.Available {
		t.Error("alpha cache corrupted")
	}
	if resultBeta == nil || resultBeta.Config.HandoffSuggestion.Available {
		t.Error("beta cache corrupted")
	}
}

func TestCLIConfigCacheClearRemovesFile(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	if err := cache.Set(&cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	result, _ := cache.Get()
	if result != nil {
		t.Errorf("expected nil after Clear, got %#v", result)
	}
}

func TestCLIConfigCacheClearMissingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	cache := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL)
	if err := cache.Clear(); err != nil {
		t.Errorf("clear on missing file should be no-op, got %v", err)
	}
}
