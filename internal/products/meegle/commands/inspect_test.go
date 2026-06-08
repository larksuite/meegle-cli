// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

func TestNewInspectCmdWithProviderIsLazy(t *testing.T) {
	calls := 0
	cmd := NewInspectCmdWithProvider(func() []types.MappedCommand {
		calls++
		return nil
	})
	if calls != 0 {
		t.Fatalf("provider should not be called during construction, got %d calls", calls)
	}

	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inspect command failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider should be called once during execution, got %d calls", calls)
	}
}
