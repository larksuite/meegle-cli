// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

type testJSONCacheData struct {
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
}

func TestJSONFileCacheConcurrentReadersNeverSeePartialWrite(t *testing.T) {
	dir := t.TempDir()
	newCache := func() *jsonFileCache[testJSONCacheData] {
		return newJSONFileCache(dir, "shared", "default", time.Hour, func(data testJSONCacheData) int64 {
			return data.Timestamp
		})
	}
	if err := newCache().Set(testJSONCacheData{Timestamp: time.Now().UnixMilli(), Value: "seed"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	const workers = 6
	const iterations = 100
	errors := make(chan error, workers*2)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			cache := newCache()
			for index := 0; index < iterations; index++ {
				value := testJSONCacheData{
					Timestamp: time.Now().UnixMilli(),
					Value:     fmt.Sprintf("worker-%d-write-%d", worker, index),
				}
				if err := cache.Set(value); err != nil {
					errors <- fmt.Errorf("set cache: %w", err)
					return
				}
			}
		}()

		wait.Add(1)
		go func() {
			defer wait.Done()
			cache := newCache()
			for index := 0; index < iterations; index++ {
				result, err := cache.Get()
				if err != nil {
					errors <- fmt.Errorf("get cache: %w", err)
					return
				}
				if result == nil || result.Value.Value == "" {
					errors <- fmt.Errorf("reader observed a missing or partial cache: %#v", result)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	tempFiles, err := filepath.Glob(filepath.Join(dir, ".shared.json.tmp-*"))
	if err != nil {
		t.Fatalf("glob temporary cache files: %v", err)
	}
	if len(tempFiles) != 0 {
		t.Fatalf("atomic cache writes left temporary files: %v", tempFiles)
	}
	info, err := os.Stat(filepath.Join(dir, "shared.json"))
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
}

func TestCacheWrappersPreserveExistingJSONShape(t *testing.T) {
	dir := t.TempDir()
	if err := NewToolCache(dir, "default", DefaultTTL).Set([]types.ToolDefinition{{Name: "demo"}}); err != nil {
		t.Fatalf("set tool cache: %v", err)
	}
	assertJSONFields(t, filepath.Join(dir, "tools.json"), "timestamp", "tools")

	config := &cliapiclient.CLIConfig{HandoffSuggestion: cliapiclient.Availability{Available: true}}
	if err := NewCLIConfigCache(dir, "default", DefaultCLIConfigTTL).Set(config); err != nil {
		t.Fatalf("set CLI config cache: %v", err)
	}
	assertJSONFields(t, filepath.Join(dir, "cli-config.json"), "timestamp", "config")
}

func assertJSONFields(t *testing.T, path string, fields ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(object) != len(fields) {
		t.Fatalf("%s fields = %v, want exactly %v", path, object, fields)
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			t.Errorf("%s is missing field %q: %s", path, field, data)
		}
	}
}
