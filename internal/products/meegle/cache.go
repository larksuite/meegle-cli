// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

const DefaultTTL = 24 * time.Hour

type cacheData struct {
	Timestamp int64                  `json:"timestamp"`
	Tools     []types.ToolDefinition `json:"tools"`
}

type ToolCache struct {
	storage *jsonFileCache[cacheData]
}

// CacheResult holds the retrieved tools and whether the cache is stale.
type CacheResult struct {
	Tools []types.ToolDefinition
	Stale bool
}

func NewToolCache(cacheDir string, profile string, ttl time.Duration) *ToolCache {
	return &ToolCache{storage: newJSONFileCache(cacheDir, "tools", profile, ttl, func(data cacheData) int64 {
		return data.Timestamp
	})}
}

func (c *ToolCache) Get() (*CacheResult, error) {
	cached, err := c.storage.Get()
	if err != nil {
		return nil, err
	}
	if cached == nil {
		return nil, nil
	}
	return &CacheResult{Tools: cached.Value.Tools, Stale: cached.Stale}, nil
}

func (c *ToolCache) Set(tools []types.ToolDefinition) error {
	return c.storage.Set(cacheData{Timestamp: time.Now().UnixMilli(), Tools: tools})
}

// Clear removes the cache file. A missing file is not an error so callers
// (e.g. `auth login` success) can invalidate unconditionally.
func (c *ToolCache) Clear() error {
	return c.storage.Clear()
}
