// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// jsonFileCache owns the shared persistence mechanics for profile-scoped JSON
// caches. T remains product-owned so callers can preserve their existing wire
// format while reusing TTL evaluation, corrupt-as-miss reads, atomic writes,
// and invalidation.
type jsonFileCache[T any] struct {
	filePath  string
	ttl       time.Duration
	timestamp func(T) int64
}

type jsonFileCacheResult[T any] struct {
	Value T
	Stale bool
}

func newJSONFileCache[T any](cacheDir, name, profile string, ttl time.Duration, timestamp func(T) int64) *jsonFileCache[T] {
	filename := name + ".json"
	if profile != "" && profile != "default" {
		filename = name + "-" + profile + ".json"
	}
	return &jsonFileCache[T]{
		filePath:  filepath.Join(cacheDir, filename),
		ttl:       ttl,
		timestamp: timestamp,
	}
}

func (cache *jsonFileCache[T]) Get() (*jsonFileCacheResult[T], error) {
	data, err := os.ReadFile(cache.filePath)
	if err != nil {
		return nil, nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, nil
	}
	stale := time.Since(time.UnixMilli(cache.timestamp(value))) > cache.ttl
	return &jsonFileCacheResult[T]{Value: value, Stale: stale}, nil
}

func (cache *jsonFileCache[T]) Set(value T) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(cache.filePath, data, 0600)
}

func (cache *jsonFileCache[T]) Clear() error {
	if err := os.Remove(cache.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeFileAtomically replaces path only after a complete file has been
// written, synced, and closed in the same directory. Concurrent readers see
// either the previous snapshot or the complete new snapshot, never partial
// JSON. Concurrent writers use distinct temporary files and resolve by the
// normal last-rename-wins cache semantics.
func writeFileAtomically(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temp.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if err = temp.Chmod(mode); err != nil {
		return err
	}
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(tempPath, path)
}
