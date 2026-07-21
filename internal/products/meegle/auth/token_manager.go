// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// tokenRefreshWarnWriter receives a single line whenever a refreshed token
// could not be persisted. The caller is allowed to keep the in-memory token,
// so the failure is best-effort but no longer silent. Tests redirect this to
// a buffer; production code keeps it pointing at os.Stderr.
var tokenRefreshWarnWriter io.Writer = os.Stderr

// SetTokenRefreshWarnWriterForTesting redirects refresh-persist warnings to
// the given writer and returns a restore function for t.Cleanup.
func SetTokenRefreshWarnWriterForTesting(w io.Writer) (restore func()) {
	prev := tokenRefreshWarnWriter
	tokenRefreshWarnWriter = w
	return func() { tokenRefreshWarnWriter = prev }
}

type TokenManager struct {
	Store   TokenStore
	Host    string
	Headers http.Header
}

func NewTokenManager(store TokenStore, host string, headers ...http.Header) *TokenManager {
	tm := &TokenManager{Store: store, Host: host}
	if len(headers) > 0 {
		tm.Headers = headers[0]
	}
	return tm
}

func (tm *TokenManager) GetToken() (string, error) {
	data, err := tm.Store.Load()
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", nil
	}
	if data.ExpiresAt > 0 && time.Now().UnixMilli() > data.ExpiresAt {
		var token string
		err := tm.withRefreshLock(func() error {
			current, err := tm.Store.Load()
			if err != nil {
				return err
			}
			if current == nil {
				return nil
			}
			if current.ExpiresAt <= 0 || time.Now().UnixMilli() <= current.ExpiresAt {
				token = current.AccessToken
				return nil
			}
			refreshed, err := tm.RefreshToken(current)
			if err != nil {
				return fmt.Errorf("refresh token: %w", err)
			}
			if refreshed != nil {
				token = refreshed.AccessToken
			}
			return nil
		})
		return token, err
	}
	return data.AccessToken, nil
}

func (tm *TokenManager) RefreshToken(data *TokenData) (*TokenData, error) {
	if data.RefreshToken == "" || data.ClientID == "" || tm.Host == "" {
		return nil, nil // not refreshable — missing prerequisites
	}
	ctx := context.Background()
	metadata, err := FetchOAuthMetadata(ctx, tm.Host, tm.Headers)
	if err != nil {
		return nil, fmt.Errorf("fetch oauth metadata: %w", err)
	}
	params := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {data.RefreshToken},
		"client_id":     {data.ClientID},
	}
	tr, err := exchangeToken(ctx, metadata.TokenEndpoint, params, tm.Headers)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	if tr == nil || tr.AccessToken == "" {
		return nil, fmt.Errorf("token exchange response missing access_token")
	}
	newData := &TokenData{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ClientID:     data.ClientID,
		ExpiresAt:    expiresAtFromSeconds(tr.ExpiresIn),
	}
	if newData.RefreshToken == "" {
		newData.RefreshToken = data.RefreshToken
	}
	tm.persistRefreshedToken(newData)
	return newData, nil
}

// persistRefreshedToken writes newData to the underlying store on a best-effort
// basis. A store failure must not prevent the caller from using the freshly
// exchanged token — but the user (and any wrapping agent) deserves to know,
// since the next CLI invocation will otherwise reload the pre-refresh token
// and silently re-trigger a 401 / re-login loop. We therefore log the failure
// instead of swallowing it.
func (tm *TokenManager) persistRefreshedToken(newData *TokenData) {
	if err := tm.Store.Save(newData); err != nil {
		fmt.Fprintf(tokenRefreshWarnWriter, "warning: failed to persist refreshed token: %v\n", err)
	}
}

// TryRefresh forces a refresh of the stored token, regardless of local ExpiresAt.
// Used by MCP clients when a server 401 indicates the token is no longer accepted.
// Returns an error when no token is stored, the token is not refreshable
// (missing refresh_token/client_id/host), or the refresh exchange fails.
func (tm *TokenManager) TryRefresh() error {
	data, err := tm.Store.Load()
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if data == nil {
		return fmt.Errorf("no token stored")
	}
	snapshot := *data
	return tm.withRefreshLock(func() error {
		current, err := tm.Store.Load()
		if err != nil {
			return fmt.Errorf("load token after locking: %w", err)
		}
		if current == nil {
			return fmt.Errorf("no token stored")
		}
		if current.AccessToken != "" && tokenDataChanged(&snapshot, current) {
			return nil // another process refreshed while this process waited
		}
		refreshed, err := tm.RefreshToken(current)
		if err != nil {
			return err
		}
		if refreshed == nil {
			return fmt.Errorf("token not refreshable")
		}
		return nil
	})
}

func (tm *TokenManager) SaveToken(data *TokenData) error { return tm.Store.Save(data) }
func (tm *TokenManager) ClearToken() error               { return tm.Store.Clear() }

// ClearTokenIfCurrent prevents a process that received a late 401 with an old
// token from deleting credentials another process has already refreshed.
func (tm *TokenManager) ClearTokenIfCurrent(expectedAccessToken string) (bool, error) {
	if expectedAccessToken == "" {
		return false, nil
	}
	cleared := false
	err := tm.withRefreshLock(func() error {
		current, err := tm.Store.Load()
		if err != nil {
			return err
		}
		if current == nil || current.AccessToken != expectedAccessToken {
			return nil
		}
		if err := tm.Store.Clear(); err != nil {
			return err
		}
		cleared = true
		return nil
	})
	return cleared, err
}

func (tm *TokenManager) withRefreshLock(fn func() error) error {
	if locker, ok := tm.Store.(tokenRefreshLocker); ok {
		return locker.WithRefreshLock(fn)
	}
	return fn()
}

func tokenDataChanged(a, b *TokenData) bool {
	return a.AccessToken != b.AccessToken ||
		a.RefreshToken != b.RefreshToken ||
		a.ClientID != b.ClientID ||
		a.ExpiresAt != b.ExpiresAt
}

func (tm *TokenManager) IsAuthenticated() (bool, error) {
	token, err := tm.GetToken()
	return token != "", err
}
