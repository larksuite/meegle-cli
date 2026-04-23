// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

func TestCacheSetAndGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewToolCache(dir, "default", DefaultTTL)
	tools := []types.ToolDefinition{{Name: "test_tool"}}
	if err := cache.Set(tools); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	result, err := cache.Get()
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if result == nil || len(result.Tools) != 1 || result.Tools[0].Name != "test_tool" {
		t.Errorf("unexpected result: %v", result)
	}
	if result.Stale {
		t.Error("should not be stale")
	}
}

func TestCacheExpired(t *testing.T) {
	dir := t.TempDir()
	cache := NewToolCache(dir, "default", -1)
	cache.Set([]types.ToolDefinition{{Name: "test"}})
	result, _ := cache.Get()
	if result == nil {
		t.Fatal("expected stale result, got nil")
	}
	if !result.Stale {
		t.Error("expected stale=true for expired cache")
	}
}

func TestCacheMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	cache := NewToolCache(dir, "default", DefaultTTL)
	result, _ := cache.Get()
	if result != nil {
		t.Error("expected nil for missing cache")
	}
}

func TestCacheFileCreated(t *testing.T) {
	dir := t.TempDir()
	cache := NewToolCache(dir, "default", DefaultTTL)
	cache.Set([]types.ToolDefinition{})
	if _, err := os.Stat(filepath.Join(dir, "tools.json")); os.IsNotExist(err) {
		t.Error("cache file not created")
	}
}

func TestCachePerProfile(t *testing.T) {
	dir := t.TempDir()
	cache1 := NewToolCache(dir, "alpha", DefaultTTL)
	cache2 := NewToolCache(dir, "beta", DefaultTTL)

	cache1.Set([]types.ToolDefinition{{Name: "tool_alpha"}})
	cache2.Set([]types.ToolDefinition{{Name: "tool_beta"}})

	result1, _ := cache1.Get()
	result2, _ := cache2.Get()

	if result1 == nil || len(result1.Tools) != 1 || result1.Tools[0].Name != "tool_alpha" {
		t.Error("alpha cache corrupted")
	}
	if result2 == nil || len(result2.Tools) != 1 || result2.Tools[0].Name != "tool_beta" {
		t.Error("beta cache corrupted")
	}
}
