// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

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
		refreshed, err := tm.RefreshToken(data)
		if err != nil {
			return "", fmt.Errorf("refresh token: %w", err)
		}
		if refreshed == nil {
			return "", nil // not refreshable (missing refresh_token/client_id/host)
		}
		return refreshed.AccessToken, nil
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
	newData := &TokenData{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ClientID:     data.ClientID,
		ExpiresAt:    expiresAtFromSeconds(tr.ExpiresIn),
	}
	if newData.RefreshToken == "" {
		newData.RefreshToken = data.RefreshToken
	}
	// Persist best-effort; a store failure must not prevent the caller from using
	// the freshly exchanged token.
	_ = tm.Store.Save(newData)
	return newData, nil
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
	refreshed, err := tm.RefreshToken(data)
	if err != nil {
		return err
	}
	if refreshed == nil {
		return fmt.Errorf("token not refreshable")
	}
	return nil
}

func (tm *TokenManager) SaveToken(data *TokenData) error { return tm.Store.Save(data) }
func (tm *TokenManager) ClearToken() error               { return tm.Store.Clear() }
func (tm *TokenManager) IsAuthenticated() (bool, error) {
	token, err := tm.GetToken()
	return token != "", err
}
