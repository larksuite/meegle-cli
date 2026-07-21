// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// FallbackStore composes a primary TokenStore (typically the OS-native
// credential store: macOS keychain, Linux secret-service, Windows DPAPI) with
// an encrypted-file fallback. Save and Load both try the primary first; on
// failure they transparently fall back to the secondary store. A primary
// failure flips a process-local sticky bit so subsequent operations skip the
// primary entirely — this avoids paying the subprocess-spawn cost (and any
// keychain-unlock dialog risk) on every call once we already know the primary
// is unusable in this process.
//
// On a successful Save through one side, FallbackStore best-effort clears the
// other side so a single token never persists in two stores at once. This
// keeps the on-disk surface clean when the environment changes (e.g. a user
// who first logged in over SSH later runs the CLI on the same machine with
// the keychain unlocked).
type FallbackStore struct {
	primary       TokenStore
	fallback      TokenStore
	primaryFailed atomic.Bool
}

func NewFallbackStore(primary, fallback TokenStore) *FallbackStore {
	return &FallbackStore{primary: primary, fallback: fallback}
}

func (s *FallbackStore) Save(data *TokenData) error {
	if !s.primaryFailed.Load() {
		if err := s.primary.Save(data); err == nil {
			_ = s.fallback.Clear()
			return nil
		}
		s.primaryFailed.Store(true)
	}
	if err := s.fallback.Save(data); err != nil {
		return fmt.Errorf("save token: primary unavailable, fallback also failed: %w", err)
	}
	return nil
}

func (s *FallbackStore) Load() (*TokenData, error) {
	if !s.primaryFailed.Load() {
		data, err := s.primary.Load()
		if err == nil {
			if data != nil {
				return data, nil
			}
		} else {
			s.primaryFailed.Store(true)
		}
	}
	return s.fallback.Load()
}

func (s *FallbackStore) Clear() error {
	errPrimary := s.primary.Clear()
	errFallback := s.fallback.Clear()
	switch {
	case errPrimary != nil && errFallback != nil:
		return fmt.Errorf("clear token: %w", errors.Join(errPrimary, errFallback))
	case errPrimary != nil:
		return fmt.Errorf("clear primary token store (fallback cleared): %w", errPrimary)
	case errFallback != nil:
		return fmt.Errorf("clear fallback token store (primary cleared): %w", errFallback)
	}
	return nil
}

// WithRefreshLock delegates to the file fallback, whose lock path is stable
// across processes regardless of which native credential backend is active.
func (s *FallbackStore) WithRefreshLock(fn func() error) error {
	if locker, ok := s.fallback.(tokenRefreshLocker); ok {
		return locker.WithRefreshLock(fn)
	}
	return fn()
}
