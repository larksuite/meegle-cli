// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

type coordinatedTokenStore struct {
	dataMu    sync.Mutex
	refreshMu sync.Mutex
	data      *TokenData
	loads     atomic.Int32
	thirdLoad chan struct{}
	loadOnce  sync.Once
}

func (s *coordinatedTokenStore) Load() (*TokenData, error) {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	count := s.loads.Add(1)
	if count >= 3 {
		s.loadOnce.Do(func() { close(s.thirdLoad) })
	}
	if s.data == nil {
		return nil, nil
	}
	cloned := *s.data
	return &cloned, nil
}

func (s *coordinatedTokenStore) Save(data *TokenData) error {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	cloned := *data
	s.data = &cloned
	return nil
}

func (s *coordinatedTokenStore) Clear() error {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	s.data = nil
	return nil
}

func (s *coordinatedTokenStore) WithRefreshLock(fn func() error) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return fn()
}

func TestTokenManager_TryRefreshSerializesAndReloadsSharedCredentials(t *testing.T) {
	store := &coordinatedTokenStore{
		data: &TokenData{
			AccessToken:  "stale-access",
			RefreshToken: "shared-refresh",
			ClientID:     "client-id",
		},
		thirdLoad: make(chan struct{}),
	}

	var tokenCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = w.Write([]byte(`{"issuer":"https://issuer.example","authorization_endpoint":"https://issuer.example/auth","registration_endpoint":"https://issuer.example/register","token_endpoint":"` + server.URL + `/token"}`))
		case "/token":
			tokenCalls.Add(1)
			<-store.thirdLoad
			_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"shared-refresh","expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previousClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = previousClient })
	host := strings.TrimPrefix(server.URL, "https://")

	managers := []*TokenManager{NewTokenManager(store, host), NewTokenManager(store, host)}
	errs := make(chan error, len(managers))
	for _, manager := range managers {
		go func(tm *TokenManager) { errs <- tm.TryRefresh() }(manager)
	}
	for range managers {
		if err := <-errs; err != nil {
			t.Fatalf("TryRefresh failed: %v", err)
		}
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token exchange, got %d", got)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.AccessToken != "fresh-access" {
		t.Fatalf("expected fresh credentials, got %+v", loaded)
	}
}

func TestTokenManager_InvalidRefreshResponsePreservesStoredCredentials(t *testing.T) {
	store := &coordinatedTokenStore{
		data: &TokenData{
			AccessToken:  "still-valid-locally",
			RefreshToken: "refresh-token",
			ClientID:     "client-id",
		},
		thirdLoad: make(chan struct{}),
	}

	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = w.Write([]byte(`{"issuer":"https://issuer.example","authorization_endpoint":"https://issuer.example/auth","registration_endpoint":"https://issuer.example/register","token_endpoint":"` + server.URL + `/token"}`))
		case "/token":
			_, _ = w.Write([]byte(`{"code":10021,"error":{"id":1000050646},"data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previousClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = previousClient })

	tm := NewTokenManager(store, strings.TrimPrefix(server.URL, "https://"))
	original, _ := store.Load()
	if _, err := tm.RefreshToken(original); err == nil {
		t.Fatal("expected invalid refresh response to fail")
	}
	loaded, _ := store.Load()
	if loaded == nil || loaded.AccessToken != "still-valid-locally" || loaded.RefreshToken != "refresh-token" {
		t.Fatalf("invalid refresh response overwrote credentials: %+v", loaded)
	}
}

func TestTokenManager_ClearTokenIfCurrent(t *testing.T) {
	store := &coordinatedTokenStore{
		data:      &TokenData{AccessToken: "fresh"},
		thirdLoad: make(chan struct{}),
	}
	tm := NewTokenManager(store, "example.com")

	cleared, err := tm.ClearTokenIfCurrent("stale")
	if err != nil {
		t.Fatal(err)
	}
	if cleared {
		t.Fatal("stale process cleared newer credentials")
	}
	loaded, _ := store.Load()
	if loaded == nil || loaded.AccessToken != "fresh" {
		t.Fatalf("newer credentials were not preserved: %+v", loaded)
	}

	cleared, err = tm.ClearTokenIfCurrent("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("current rejected token was not cleared")
	}
}
