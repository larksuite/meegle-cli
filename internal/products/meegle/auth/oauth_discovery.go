// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

type OAuthMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	DeviceAuthEndpoint    string   `json:"device_authorization_endpoint"`
	ResponseTypes         []string `json:"response_types_supported"`
	GrantTypes            []string `json:"grant_types_supported"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

func FetchOAuthMetadata(ctx context.Context, host string, headers ...http.Header) (*OAuthMetadata, error) {
	url := fmt.Sprintf("https://%s/.well-known/oauth-authorization-server", host)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	applyHeaders(req, headers...)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, meerrors.NewClientError("OAUTH_DISCOVERY_FAILED",
			fmt.Sprintf("Failed to fetch OAuth configuration: %d", resp.StatusCode)).
			WithSuggestion(fmt.Sprintf("Please verify that the host %s is correct", host))
	}
	var meta OAuthMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	// Validate required endpoints
	requiredFields := map[string]string{
		"issuer":                 meta.Issuer,
		"authorization_endpoint": meta.AuthorizationEndpoint,
		"token_endpoint":         meta.TokenEndpoint,
		"registration_endpoint":  meta.RegistrationEndpoint,
	}
	for field, value := range requiredFields {
		if value == "" || !strings.HasPrefix(value, "https://") {
			return nil, meerrors.NewClientError("OAUTH_DISCOVERY_INVALID",
				fmt.Sprintf("Invalid OAuth configuration field: %s", field))
		}
	}

	return &meta, nil
}
