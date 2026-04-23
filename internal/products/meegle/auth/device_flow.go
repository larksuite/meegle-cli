// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

// DeviceCodeInitResult contains the result of device code initialization.
type DeviceCodeInitResult struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	ClientID                string `json:"client_id"`
}

// DeviceCodePollOptions contains options for polling a device code.
type DeviceCodePollOptions struct {
	Host       string
	DeviceCode string
	ClientID   string
	Interval   int
	ExpiresIn  int
	Headers    http.Header
}

// PollOnceStatus represents the status of a single poll attempt.
type PollOnceStatus string

const (
	PollStatusOK                   PollOnceStatus = "ok"
	PollStatusAuthorizationPending PollOnceStatus = "authorization_pending"
	PollStatusSlowDown             PollOnceStatus = "slow_down"
	PollStatusExpiredToken         PollOnceStatus = "expired_token"
)

// PollOnceResult contains the result of a single poll attempt.
type PollOnceResult struct {
	Status    PollOnceStatus `json:"status"`
	TokenData *TokenData     `json:"token_data,omitempty"`
}

// StartDeviceCodeInit performs the initialization phase of the device code flow:
// discovers OAuth metadata, registers a client, and obtains a device code.
func StartDeviceCodeInit(ctx context.Context, host string, headers ...http.Header) (*DeviceCodeInitResult, error) {
	metadata, err := FetchOAuthMetadata(ctx, host, headers...)
	if err != nil {
		return nil, err
	}
	if metadata.DeviceAuthEndpoint == "" {
		return nil, meerrors.NewClientError("DEVICE_FLOW_UNSUPPORTED", "This site does not support the Device Code flow")
	}

	// Dynamic client registration
	creds, err := RegisterClient(ctx, metadata.RegistrationEndpoint, "", headers...)
	if err != nil {
		return nil, err
	}

	params := url.Values{"client_id": {creds.ClientID}}
	req, _ := http.NewRequest("POST", metadata.DeviceAuthEndpoint, strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, headers...)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Handle wrapped response format: {code, data: {device_code, ...}, msg}
	var rawResp json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawResp); err != nil {
		return nil, err
	}

	var dcr deviceCodeResponse
	if err := json.Unmarshal(rawResp, &dcr); err != nil {
		return nil, err
	}
	// Check for wrapped format
	if dcr.DeviceCode == "" {
		var wrapped struct {
			Data deviceCodeResponse `json:"data"`
		}
		if err := json.Unmarshal(rawResp, &wrapped); err == nil && wrapped.Data.DeviceCode != "" {
			dcr = wrapped.Data
		}
	}

	if dcr.DeviceCode == "" {
		return nil, meerrors.NewServerError("DEVICE_CODE_FAILED", "Failed to obtain device code")
	}

	// Rewrite verification_uri_complete: rename user_code → usercode, tag channel
	verificationURIComplete := dcr.VerificationURIComplete
	if verificationURIComplete != "" {
		if u, err := url.Parse(verificationURIComplete); err == nil {
			q := u.Query()
			if uc := q.Get("user_code"); uc != "" {
				q.Del("user_code")
				q.Set("usercode", uc)
			}
			q.Set("channel", "meegle-cli")
			u.RawQuery = q.Encode()
			verificationURIComplete = u.String()
		}
	}

	return &DeviceCodeInitResult{
		DeviceCode:              dcr.DeviceCode,
		UserCode:                dcr.UserCode,
		VerificationURI:         dcr.VerificationURI,
		VerificationURIComplete: verificationURIComplete,
		ExpiresIn:               dcr.ExpiresIn,
		Interval:                dcr.Interval,
		ClientID:                creds.ClientID,
	}, nil
}

// deviceTokenResponse extends tokenResponse with an error field for device code polling.
type deviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// PollDeviceCodeOnce performs a single non-blocking poll for device code authorization.
// Unlike auth code flow, device code polling must inspect the response `error` field
// rather than relying on HTTP status codes, since the server returns 200 for pending states.
func PollDeviceCodeOnce(ctx context.Context, host, deviceCode, clientID string, headers ...http.Header) (*PollOnceResult, error) {
	metadata, err := FetchOAuthMetadata(ctx, host, headers...)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", metadata.TokenEndpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, headers...)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// Try direct decode, then wrapped format {code, data: {...}}
	var data deviceTokenResponse
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.AccessToken == "" && data.Error == "" {
		var wrapped struct {
			Data deviceTokenResponse `json:"data"`
		}
		if err := json.Unmarshal(raw, &wrapped); err == nil {
			data = wrapped.Data
		}
	}

	// Check for successful token
	if data.AccessToken != "" {
		return &PollOnceResult{
			Status: PollStatusOK,
			TokenData: &TokenData{
				AccessToken:  data.AccessToken,
				RefreshToken: data.RefreshToken,
				ClientID:     clientID,
				ExpiresAt:    expiresAtFromSeconds(data.ExpiresIn),
			},
		}, nil
	}

	// Check error field
	switch data.Error {
	case "authorization_pending":
		return &PollOnceResult{Status: PollStatusAuthorizationPending}, nil
	case "slow_down":
		return &PollOnceResult{Status: PollStatusSlowDown}, nil
	case "expired_token":
		return &PollOnceResult{Status: PollStatusExpiredToken}, nil
	default:
		return nil, meerrors.NewServerError("DEVICE_CODE_FAILED",
			fmt.Sprintf("Device code authorization failed: %s", data.Error))
	}
}

// StartDeviceCodePoll polls for device code authorization in a blocking loop.
func StartDeviceCodePoll(ctx context.Context, opts DeviceCodePollOptions) (*TokenData, error) {
	interval := time.Duration(opts.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(opts.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		result, err := PollDeviceCodeOnce(ctx, opts.Host, opts.DeviceCode, opts.ClientID, opts.Headers)
		if err != nil {
			return nil, err
		}

		switch result.Status {
		case PollStatusOK:
			return result.TokenData, nil
		case PollStatusSlowDown:
			interval += 5 * time.Second
			continue
		case PollStatusAuthorizationPending:
			continue
		case PollStatusExpiredToken:
			return nil, meerrors.NewClientError("DEVICE_CODE_EXPIRED", "Authorization timed out, please re-run meegle auth login --device-code")
		}
	}

	return nil, meerrors.NewClientError("DEVICE_CODE_EXPIRED", "Device code has expired, please try again")
}

// StartDeviceCodeFlow runs the full interactive OAuth Device Code flow with QR code display.
func StartDeviceCodeFlow(ctx context.Context, host string, headers ...http.Header) (*TokenData, error) {
	initResult, err := StartDeviceCodeInit(ctx, host, headers...)
	if err != nil {
		return nil, err
	}

	// Display verification info — prefer the complete URI with query params
	displayURI := initResult.VerificationURIComplete
	if displayURI == "" {
		displayURI = initResult.VerificationURI
	}
	fmt.Printf("\n  Please scan the QR code with your phone, or open the following URL in a browser:\n")
	fmt.Printf("  URL: %s\n", displayURI)
	fmt.Printf("  Authorization code: %s\n", initResult.UserCode)

	// Display QR code if verification_uri_complete is available
	qrURI := initResult.VerificationURIComplete
	if qrURI == "" {
		qrURI = initResult.VerificationURI
	}
	if qrURI != "" {
		fmt.Println()
		qrterminal.GenerateHalfBlock(qrURI, qrterminal.L, os.Stdout)
		fmt.Println()
	}

	var h http.Header
	if len(headers) > 0 {
		h = headers[0]
	}

	return StartDeviceCodePoll(ctx, DeviceCodePollOptions{
		Host:       host,
		DeviceCode: initResult.DeviceCode,
		ClientID:   initResult.ClientID,
		Interval:   int(initResult.Interval),
		ExpiresIn:  int(initResult.ExpiresIn),
		Headers:    h,
	})
}

// applyHeaders adds optional HTTP headers to a request.
func applyHeaders(req *http.Request, headers ...http.Header) {
	if len(headers) > 0 && headers[0] != nil {
		for k, vs := range headers[0] {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
}
