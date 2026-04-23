// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import "runtime"

type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
}

type TokenStore interface {
	Load() (*TokenData, error)
	Save(data *TokenData) error
	Clear() error
}

// tokenStoreFactory is the pluggable constructor used by CreateTokenStore.
// Tests swap it via SetTokenStoreFactory so ResolveIdentity (and anything
// else routing through CreateTokenStore) talks to an isolated store instead
// of the developer's real keychain / secret-service.
var tokenStoreFactory = defaultTokenStoreFactory

func CreateTokenStore(profile string) TokenStore {
	return tokenStoreFactory(profile)
}

func defaultTokenStoreFactory(profile string) TokenStore {
	switch runtime.GOOS {
	case "darwin":
		store := NewKeychainStore(profile)
		if store.IsAvailable() {
			return store
		}
	case "linux":
		store := NewSecretToolStore(profile)
		if store.IsAvailable() {
			return store
		}
	case "windows":
		if store := newWindowsStore(profile); store != nil {
			return store
		}
	}
	return NewFileStore("", profile)
}

// SetTokenStoreFactory replaces the TokenStore constructor used by
// CreateTokenStore and returns a restore function that reinstates the prior
// factory. Intended for test isolation: callers pair it with t.Cleanup so the
// production default is reinstated when the test finishes. Passing nil
// restores the default factory directly.
func SetTokenStoreFactory(factory func(profile string) TokenStore) (restore func()) {
	prev := tokenStoreFactory
	if factory == nil {
		tokenStoreFactory = defaultTokenStoreFactory
	} else {
		tokenStoreFactory = factory
	}
	return func() { tokenStoreFactory = prev }
}
