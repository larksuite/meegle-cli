// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"golang.org/x/term"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

// normalizeHost extracts the host from a URL or returns the raw string.
func normalizeHost(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return u.Host
}

func NewAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}

	authCmd.AddCommand(newLoginCmd())
	authCmd.AddCommand(newLogoutCmd())
	authCmd.AddCommand(newStatusCmd())

	return authCmd
}

// resolveHost determines the host to use for auth, applying --host flag if provided.
// Priority: --host flag > config.host > interactive prompt > error.
// When --host is provided or selected interactively, it is saved to the profile config.
func resolveHost(profileName string, hostFlag string, cfg meegleConfig) (string, error) {
	if hostFlag != "" {
		host := normalizeHost(hostFlag)
		cfg.Host = host
		if err := saveProfileConfig(profileName, cfg); err != nil {
			return "", err
		}
		return host, nil
	}

	if cfg.Host != "" {
		return cfg.Host, nil
	}

	// Host not configured: prompt interactively or fail
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", meerrors.NewClientError("HOST_NOT_CONFIGURED", "host not configured").
			WithSuggestion(meerrors.HintNonInteractiveSetup)
	}

	host, err := promptHost()
	if err != nil {
		return "", err
	}
	cfg.Host = host
	if err := saveProfileConfig(profileName, cfg); err != nil {
		return "", err
	}
	fmt.Printf("\n  ✓ Host configured: %s\n\n", host)
	return host, nil
}

func newLoginCmd() *cobra.Command {
	var (
		deviceCode      bool
		hostFlag        string
		phase           string
		deviceCodeValue string
		clientID        string
		interval        string
		expiresIn       string
		once            bool
		format          string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Meegle",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}

			cfg, err := loadConfig(profileName)
			if err != nil {
				return err
			}

			var httpHeaders http.Header
			if cfg.Headers != nil {
				httpHeaders = make(http.Header)
				for k, v := range cfg.Headers {
					httpHeaders.Set(k, v)
				}
			}

			ctx := context.Background()

			// --phase init: initialize device code, output verification info
			if phase == "init" {
				if !deviceCode {
					return meerrors.NewClientError("INVALID_OPTION", "--phase is only available in --device-code mode")
				}
				host, err := resolveHost(profileName, hostFlag, cfg)
				if err != nil {
					return err
				}
				result, err := auth.StartDeviceCodeInit(ctx, host, httpHeaders)
				if err != nil {
					return err
				}
				text, err := renderPayload(deviceCodeInitToMap(result), format)
				if err != nil {
					return err
				}
				fmt.Println(text)
				return nil
			}

			// --phase poll: poll for authorization completion
			if phase == "poll" {
				if !deviceCode {
					return meerrors.NewClientError("INVALID_OPTION", "--phase is only available in --device-code mode")
				}
				if deviceCodeValue == "" || clientID == "" {
					return meerrors.NewClientError("MISSING_OPTION", "--phase poll requires --device-code-value and --client-id")
				}
				host, err := resolveHost(profileName, hostFlag, cfg)
				if err != nil {
					return err
				}

				// --once: single non-blocking attempt
				if once {
					result, err := auth.PollDeviceCodeOnce(ctx, host, deviceCodeValue, clientID, httpHeaders)
					if err != nil {
						return err
					}
					if result.Status == auth.PollStatusOK && result.TokenData != nil {
						store := auth.CreateTokenStore(profileName)
						tm := auth.NewTokenManager(store, host, httpHeaders)
						if err := tm.SaveToken(result.TokenData); err != nil {
							return err
						}
						text, err := renderPayload(map[string]any{"status": "ok", "message": "Login successful"}, format)
						if err != nil {
							return err
						}
						fmt.Println(text)
					} else {
						text, err := renderPayload(map[string]any{"status": string(result.Status)}, format)
						if err != nil {
							return err
						}
						fmt.Println(text)
					}
					return nil
				}

				// Default: blocking poll loop
				intervalSec, _ := strconv.Atoi(interval)
				if intervalSec <= 0 {
					intervalSec = 5
				}
				expiresInSec, _ := strconv.Atoi(expiresIn)
				if expiresInSec <= 0 {
					expiresInSec = 600
				}

				tokenData, err := auth.StartDeviceCodePoll(ctx, auth.DeviceCodePollOptions{
					Host:       host,
					DeviceCode: deviceCodeValue,
					ClientID:   clientID,
					Interval:   intervalSec,
					ExpiresIn:  expiresInSec,
					Headers:    httpHeaders,
				})
				if err != nil {
					return err
				}

				store := auth.CreateTokenStore(profileName)
				tm := auth.NewTokenManager(store, host, httpHeaders)
				if err := tm.SaveToken(tokenData); err != nil {
					return err
				}
				text, err := renderPayload(map[string]any{"status": "ok", "message": "Login successful"}, format)
				if err != nil {
					return err
				}
				fmt.Println(text)
				return nil
			}

			// No --phase: original full interactive flow
			return runInteractiveLogin(ctx, profileName, hostFlag, cfg, httpHeaders, deviceCode)
		},
	}
	cmd.Flags().BoolVar(&deviceCode, "device-code", false, "Use Device Code flow (for environments without a browser)")
	cmd.Flags().StringVar(&hostFlag, "host", "", "Specify host domain (skip interactive selection)")
	cmd.Flags().StringVar(&phase, "phase", "", "Device Code phase: init or poll")
	cmd.Flags().StringVar(&deviceCodeValue, "device-code-value", "", "device_code for the poll phase")
	cmd.Flags().StringVar(&clientID, "client-id", "", "client_id for the poll phase")
	cmd.Flags().StringVar(&interval, "interval", "5", "Poll interval in seconds")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "600", "Poll timeout in seconds")
	cmd.Flags().BoolVar(&once, "once", false, "Single non-blocking poll attempt, return result immediately")
	cmd.Flags().StringVar(&format, "format", "json", "Output format: json, table, ndjson")
	return cmd
}

// runInteractiveLogin handles the default (non --phase) login flow. It
// resolves the host, refuses the browser-based Authorization Code flow in
// non-interactive environments, drives either Device Code or Authorization
// Code, and persists the resulting token.
func runInteractiveLogin(ctx context.Context, profileName, hostFlag string, cfg meegleConfig, httpHeaders http.Header, deviceCode bool) error {
	host, err := resolveHost(profileName, hostFlag, cfg)
	if err != nil {
		return err
	}

	// The Authorization Code flow listens on a local HTTP callback the
	// browser must reach. In non-interactive environments (CI, Claude
	// Code, pipes) that callback is never delivered, so refuse early
	// and point the user at --device-code.
	if !deviceCode && !term.IsTerminal(int(os.Stdin.Fd())) {
		return meerrors.NewClientError("INTERACTIVE_BROWSER_REQUIRED",
			"the default Authorization Code flow requires an interactive browser callback").
			WithSuggestion(meerrors.HintDeviceCodeRequired)
	}

	var tokenData *auth.TokenData
	if deviceCode {
		tokenData, err = auth.StartDeviceCodeFlow(ctx, host, httpHeaders)
	} else {
		tokenData, err = auth.StartAuthCodeFlow(ctx, host, httpHeaders)
	}
	if err != nil {
		return err
	}

	store := auth.CreateTokenStore(profileName)
	tm := auth.NewTokenManager(store, host, httpHeaders)
	if err := tm.SaveToken(tokenData); err != nil {
		return err
	}

	fmt.Printf("✓ [%s] Login successful!\n", profileName)
	printCompletionHint()
	return nil
}

func deviceCodeInitToMap(r *auth.DeviceCodeInitResult) map[string]any {
	return map[string]any{
		"device_code":               r.DeviceCode,
		"user_code":                 r.UserCode,
		"verification_uri":          r.VerificationURI,
		"verification_uri_complete": r.VerificationURIComplete,
		"expires_in":                r.ExpiresIn,
		"interval":                  r.Interval,
		"client_id":                 r.ClientID,
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}
			store := auth.CreateTokenStore(profileName)
			if err := store.Clear(); err != nil {
				return err
			}
			fmt.Printf("✓ [%s] Logged out\n", profileName)
			return nil
		},
	}
}

// StatusResult represents the structured auth status output.
type StatusResult struct {
	Authenticated    bool    `json:"authenticated"`
	Host             *string `json:"host"`
	ExpiresInMinutes *int    `json:"expires_in_minutes,omitempty"`
}

func buildStatusResult(profileName string) (*StatusResult, error) {
	cfg, err := loadConfig(profileName)
	if err != nil {
		return nil, err
	}

	ident, err := meegle.ResolveIdentity(cfg, profileName)
	if err != nil {
		return nil, err
	}

	var host *string
	if ident.Host != "" {
		host = &ident.Host
	}

	if ident.Token == "" {
		return &StatusResult{
			Authenticated: false,
			Host:          host,
		}, nil
	}

	// ExpiresAt is only available for tokens stored via `auth login`.
	var expiresInMinutes *int
	if ident.Source == meegle.SourceStore && ident.Store != nil {
		data, err := ident.Store.Load()
		if err == nil && data != nil && data.ExpiresAt > 0 {
			remaining := time.Until(time.UnixMilli(data.ExpiresAt))
			mins := int(remaining.Minutes())
			if mins < 0 {
				mins = 0
			}
			expiresInMinutes = &mins
		}
	}

	return &StatusResult{
		Authenticated:    true,
		Host:             host,
		ExpiresInMinutes: expiresInMinutes,
	}, nil
}

func newStatusCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "View authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}

			result, err := buildStatusResult(profileName)
			if err != nil {
				return err
			}

			if format == "json" || format == "ndjson" {
				m := map[string]any{
					"authenticated": result.Authenticated,
					"host":          result.Host,
				}
				if result.Authenticated {
					m["expires_in_minutes"] = result.ExpiresInMinutes
				}
				text, err := renderPayload(m, format)
				if err != nil {
					return err
				}
				fmt.Println(text)
				if !result.Authenticated {
					os.Exit(1)
				}
				return nil
			}

			// Human-readable text output (table format)
			if !result.Authenticated {
				hostStr := ""
				if result.Host != nil {
					hostStr = fmt.Sprintf("\n  Host:    %s", *result.Host)
				}
				fmt.Printf("  Profile: %s%s\n  Status:  ✗ Not authenticated\n", profileName, hostStr)
				os.Exit(1)
				return nil
			}

			status := "✓ Authenticated"
			if result.ExpiresInMinutes != nil {
				if *result.ExpiresInMinutes <= 0 {
					status = "✗ Token expired"
				} else {
					status = fmt.Sprintf("✓ Authenticated (%d minutes remaining)", *result.ExpiresInMinutes)
				}
			}
			fmt.Printf("  Profile: %s\n  Status:  %s\n", profileName, status)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "Output format: json, table, ndjson")
	return cmd
}
