// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

func StartAuthCodeFlow(ctx context.Context, host string, headers ...http.Header) (*TokenData, error) {
	metadata, err := FetchOAuthMetadata(ctx, host, headers...)
	if err != nil {
		return nil, err
	}
	server, err := StartCallbackServer()
	if err != nil {
		return nil, err
	}
	defer server.Close()
	creds, err := RegisterClient(ctx, metadata.RegistrationEndpoint, server.RedirectURI, headers...)
	if err != nil {
		return nil, err
	}
	codeVerifier := GenerateCodeVerifier()
	codeChallenge := GenerateCodeChallenge(codeVerifier)
	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {creds.ClientID},
		"redirect_uri":          {server.RedirectURI},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"channel":               {"meegle-cli"},
	}
	authURL := metadata.AuthorizationEndpoint + "?" + params.Encode()
	fmt.Println("  Opening browser for authorization...")
	fmt.Printf("  Authorization URL: %s\n", authURL)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Println("  Could not open browser automatically, please visit the URL above manually")
	}
	result, err := server.WaitForCallback(state)
	if err != nil {
		return nil, err
	}
	tokenResp, err := exchangeToken(ctx, metadata.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {result.Code},
		"redirect_uri":  {server.RedirectURI},
		"client_id":     {creds.ClientID},
		"code_verifier": {codeVerifier},
	}, headers...)
	if err != nil {
		return nil, err
	}
	return &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     creds.ClientID,
		ExpiresAt:    expiresAtFromSeconds(tokenResp.ExpiresIn),
	}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func exchangeToken(ctx context.Context, endpoint string, params url.Values, headers ...http.Header) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, headers...)
	resp, err := HTTPClient(ctx).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, meerrors.NewServerError("TOKEN_EXCHANGE_FAILED", fmt.Sprintf("Token exchange failed: %d", resp.StatusCode))
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	if tr.AccessToken == "" {
		return nil, meerrors.NewServerError("TOKEN_EXCHANGE_FAILED",
			"Token exchange response did not include access_token")
	}
	return &tr, nil
}

func expiresAtFromSeconds(expiresIn int64) int64 {
	if expiresIn <= 0 {
		return 0
	}
	return time.Now().UnixMilli() + expiresIn*1000
}
