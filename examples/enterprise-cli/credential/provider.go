// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package credential registers a sample enterprise credential provider.
package credential

import (
	"context"
	"os"

	credentialapi "github.com/larksuite/meegle-cli/extension/credential"
)

type provider struct{}

func (provider) Name() string  { return "corp-sso" }
func (provider) Priority() int { return 1 }

func (provider) ResolveAccount(context.Context) (*credentialapi.Account, error) {
	host := os.Getenv("CORP_MEEGLE_HOST")
	if host == "" {
		return nil, nil
	}
	return &credentialapi.Account{Host: host, ProfileName: "corp"}, nil
}

func (provider) ResolveToken(context.Context, credentialapi.TokenSpec) (*credentialapi.Token, error) {
	if os.Getenv("CORP_DEVICE_TRUST") == "denied" {
		return nil, &credentialapi.BlockError{
			Provider: "corp-sso",
			Reason:   "device is not trusted",
		}
	}
	token := os.Getenv("CORP_MEEGLE_TOKEN")
	if token == "" {
		return nil, nil
	}
	return &credentialapi.Token{
		Value:  token,
		Header: "Authorization",
		Source: "corp-sso",
	}, nil
}

func init() {
	credentialapi.Register(provider{})
}
