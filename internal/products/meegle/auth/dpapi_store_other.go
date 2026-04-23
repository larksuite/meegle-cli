// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

//go:build !windows

package auth

// newWindowsStore is a no-op on non-Windows platforms.
func newWindowsStore(_ string) TokenStore {
	return nil
}
