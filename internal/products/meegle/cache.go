// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

const DefaultTTL = 24 * time.Hour

type cacheData struct {
	Timestamp int64                  `json:"timestamp"`
	Tools     []types.ToolDefinition `json:"tools"`
}

type ToolCache struct {
	filePath string
	ttl      time.Duration
}

// CacheResult holds the retrieved tools and whether the cache is stale.
type CacheResult struct {
	Tools []types.ToolDefinition
	Stale bool
}

func NewToolCache(cacheDir string, profile string, ttl time.Duration) *ToolCache {
	filename := "tools.json"
	if profile != "" && profile != "default" {
		filename = "tools-" + profile + ".json"
	}
	return &ToolCache{filePath: filepath.Join(cacheDir, filename), ttl: ttl}
}

func (c *ToolCache) Get() (*CacheResult, error) {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return nil, nil
	}
	var cached cacheData
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, nil
	}
	stale := time.Since(time.UnixMilli(cached.Timestamp)) > c.ttl
	return &CacheResult{Tools: cached.Tools, Stale: stale}, nil
}

func (c *ToolCache) Set(tools []types.ToolDefinition) error {
	data := cacheData{Timestamp: time.Now().UnixMilli(), Tools: tools}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0700); err != nil {
		return err
	}
	return os.WriteFile(c.filePath, b, 0600)
}

// Clear removes the cache file. A missing file is not an error so callers
// (e.g. `auth login` success) can invalidate unconditionally.
func (c *ToolCache) Clear() error {
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
