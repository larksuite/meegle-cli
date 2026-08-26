// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"runtime"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

func getClientName() string {
	var parts []string
	if u, err := user.Current(); err == nil && u.Username != "" {
		parts = append(parts, u.Username)
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		parts = append(parts, h)
	}
	var name string
	if len(parts) > 1 {
		name = parts[0] + "@" + parts[1]
	} else if len(parts) == 1 {
		name = parts[0]
	}
	sys := runtime.GOOS + "/" + runtime.GOARCH
	if name != "" {
		return fmt.Sprintf("%s (%s)", name, sys)
	}
	return fmt.Sprintf("meegle-cli (%s)", sys)
}

type ClientCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func RegisterClient(ctx context.Context, endpoint, redirectURI string, headers ...http.Header) (*ClientCredentials, error) {
	grantTypes := []string{"authorization_code", "refresh_token"}
	if redirectURI == "" {
		grantTypes = []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"}
	}
	regBody := map[string]any{
		"client_name":                getClientName(),
		"grant_types":                grantTypes,
		"token_endpoint_auth_method": "none",
	}
	if redirectURI != "" {
		regBody["redirect_uris"] = []string{redirectURI}
		regBody["response_types"] = []string{"code"}
	}
	body, _ := json.Marshal(regBody)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyHeaders(req, headers...)
	resp, err := HTTPClient(ctx).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, meerrors.NewClientError("CLIENT_REGISTRATION_FAILED",
			fmt.Sprintf("Client registration failed: %d", resp.StatusCode))
	}
	var rawResp json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawResp); err != nil {
		return nil, err
	}

	// Try direct decode first
	var creds ClientCredentials
	if err := json.Unmarshal(rawResp, &creds); err != nil {
		return nil, err
	}

	// Handle wrapped response format: { code, data: { client_id, ... }, msg }
	if creds.ClientID == "" {
		var wrapped struct {
			Data ClientCredentials `json:"data"`
			Msg  string            `json:"msg"`
		}
		if err := json.Unmarshal(rawResp, &wrapped); err == nil && wrapped.Data.ClientID != "" {
			creds = wrapped.Data
		}
		if creds.ClientID == "" {
			msg := wrapped.Msg
			if msg == "" {
				msg = string(rawResp)
			}
			return nil, meerrors.NewClientError("CLIENT_REGISTRATION_FAILED",
				fmt.Sprintf("Client registration failed: missing client_id in response (%s)", msg))
		}
	}

	return &creds, nil
}
