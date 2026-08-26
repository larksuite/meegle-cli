// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
)

// DefaultCLIConfigTTL bounds how long a cached server-side CLI configuration
// snapshot is served without hitting the server.
const DefaultCLIConfigTTL = time.Hour

type cliConfigCacheData struct {
	Timestamp int64                   `json:"timestamp"`
	Config    *cliapiclient.CLIConfig `json:"config"`
}

// CLIConfigCache persists the last server-side CLI configuration snapshot per
// profile using the same atomic JSON cache mechanics as ToolCache.
type CLIConfigCache struct {
	storage *jsonFileCache[cliConfigCacheData]
}

// CLIConfigCacheResult holds the cached snapshot and whether it is past its TTL.
type CLIConfigCacheResult struct {
	Config *cliapiclient.CLIConfig
	Stale  bool
}

func NewCLIConfigCache(cacheDir string, profile string, ttl time.Duration) *CLIConfigCache {
	return &CLIConfigCache{storage: newJSONFileCache(cacheDir, "cli-config", profile, ttl, func(data cliConfigCacheData) int64 {
		return data.Timestamp
	})}
}

// Get returns the cached snapshot, or nil when the file is missing or corrupt
// (both treated as a miss so the caller falls through to the server).
func (c *CLIConfigCache) Get() (*CLIConfigCacheResult, error) {
	cached, err := c.storage.Get()
	if err != nil {
		return nil, err
	}
	if cached == nil || cached.Value.Config == nil {
		return nil, nil
	}
	return &CLIConfigCacheResult{Config: cached.Value.Config, Stale: cached.Stale}, nil
}

func (c *CLIConfigCache) Set(config *cliapiclient.CLIConfig) error {
	if config == nil {
		return nil
	}
	return c.storage.Set(cliConfigCacheData{Timestamp: time.Now().UnixMilli(), Config: config})
}

// Clear removes the cache file. A missing file is not an error so callers can
// invalidate unconditionally when their business operation requires it.
func (c *CLIConfigCache) Clear() error {
	return c.storage.Clear()
}
