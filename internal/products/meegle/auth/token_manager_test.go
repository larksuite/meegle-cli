// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failingSaveStore struct{ err error }

func (f *failingSaveStore) Load() (*TokenData, error) { return nil, nil }
func (f *failingSaveStore) Save(*TokenData) error     { return f.err }
func (f *failingSaveStore) Clear() error              { return nil }

// TestTokenManager_PersistRefreshedTokenWarnsOnFailure guards the regression
// where token_manager.go silently dropped Save errors after a successful
// refresh — leaving the user with an in-memory-only token that quietly
// disappears on the next CLI invocation.
func TestTokenManager_PersistRefreshedTokenWarnsOnFailure(t *testing.T) {
	var buf bytes.Buffer
	restore := SetTokenRefreshWarnWriterForTesting(&buf)
	t.Cleanup(restore)

	tm := NewTokenManager(&failingSaveStore{err: errors.New("disk full")}, "example.com")
	tm.persistRefreshedToken(&TokenData{AccessToken: "fresh"})

	out := buf.String()
	if !strings.Contains(out, "failed to persist refreshed token") {
		t.Errorf("expected warning prefix in output, got %q", out)
	}
	if !strings.Contains(out, "disk full") {
		t.Errorf("expected underlying error in output, got %q", out)
	}
}

func TestTokenManager_PersistRefreshedTokenSilentOnSuccess(t *testing.T) {
	var buf bytes.Buffer
	restore := SetTokenRefreshWarnWriterForTesting(&buf)
	t.Cleanup(restore)

	tm := NewTokenManager(NewFileStore(t.TempDir(), "test"), "example.com")
	tm.persistRefreshedToken(&TokenData{AccessToken: "fresh"})

	if buf.Len() != 0 {
		t.Errorf("expected no warning on successful persist, got %q", buf.String())
	}
}
