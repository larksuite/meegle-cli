// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/larksuite/meegle-cli/extension/credential"
)

type countingFirstRunProvider struct{ calls *atomic.Int32 }

func (p countingFirstRunProvider) Name() string { return "first-run-counter" }
func (p countingFirstRunProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	p.calls.Add(1)
	return nil, nil
}
func (p countingFirstRunProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	p.calls.Add(1)
	return nil, nil
}

func TestCheckFirstRunWithIdentity_DoesNotResolveCredentialAgain(t *testing.T) {
	setupTestDir(t)
	var calls atomic.Int32
	credential.Register(countingFirstRunProvider{calls: &calls})
	before := calls.Load()

	err := CheckFirstRunWithIdentity(context.Background(), "default", ResolvedIdentity{})
	if err == nil || !strings.Contains(err.Error(), "does not support interactive setup") {
		t.Fatalf("CheckFirstRunWithIdentity() error = %v, want non-interactive setup error", err)
	}
	if got := calls.Load(); got != before {
		t.Fatalf("credential provider calls = %d, want unchanged %d", got, before)
	}
}
