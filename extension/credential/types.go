// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package credential defines the public Meegle CLI credential extension seam.
package credential

import (
	"context"
	"fmt"
)

// Account identifies the Meegle endpoint and profile selected by a provider.
type Account struct {
	Host        string
	ProfileName string
}

// Token is a resolved Meegle user token and its non-secret metadata.
type Token struct {
	Value  string
	Header string
	Source string
}

// TokenSpec describes the account for which a token is requested.
type TokenSpec struct {
	Host        string
	ProfileName string
}

// BlockError marks an intentional policy denial during credential resolution.
// Every non-nil provider error stops resolution without built-in fallback;
// BlockError distinguishes a deliberate block from an operational failure.
type BlockError struct {
	Provider string
	Reason   string
}

func (e *BlockError) Error() string {
	return fmt.Sprintf("blocked by %s: %s", e.Provider, e.Reason)
}

// Provider resolves an optional account and token.
// Returning nil, nil skips to the next provider; returning a value handles the
// request. Every non-nil error stops the chain without built-in fallback;
// BlockError identifies an intentional policy denial.
type Provider interface {
	Name() string
	ResolveAccount(ctx context.Context) (*Account, error)
	ResolveToken(ctx context.Context, spec TokenSpec) (*Token, error)
}
