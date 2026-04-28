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

// defaultTokenStoreFactory returns a FallbackStore that prefers the OS-native
// credential store and transparently falls back to an encrypted file when the
// native store rejects the operation at runtime (sandboxed macOS, locked
// keychain over SSH, headless Linux without a secret-service daemon, etc.).
// Trying the primary on first use — instead of probing it ahead of time —
// avoids leaving sentinel entries in the user's keychain just to determine
// availability.
func defaultTokenStoreFactory(profile string) TokenStore {
	file := NewFileStore("", profile)
	switch runtime.GOOS {
	case "darwin":
		return NewFallbackStore(NewKeychainStore(profile), file)
	case "linux":
		return NewFallbackStore(NewSecretToolStore(profile), file)
	case "windows":
		if win := newWindowsStore(profile); win != nil {
			return NewFallbackStore(win, file)
		}
	}
	return file
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
