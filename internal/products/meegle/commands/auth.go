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
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

// statusVerifyTimeout caps how long `auth status` will wait on a single
// server-side token validation call. The probe is a single tools/list RPC,
// so 10s is generous; the upper bound keeps cron preflights from hanging
// when the host is unreachable.
const statusVerifyTimeout = 10 * time.Second

// Reason strings surfaced in StatusResult.Reason and the human-readable
// status text. Kept in one place so tests and docs agree.
const (
	statusReasonNoLocalToken      = "no local token"
	statusReasonTokenRejected     = "token rejected by server"
	statusReasonServerUnreachable = "server unreachable"
)

// tokenVerifier reports whether a resolved identity can actually authenticate
// against the Meegle server. Returns "" on success, otherwise one of the
// statusReason* constants (server-unreachable variants carry the underlying
// error as a suffix). Extracted as a function value so tests substitute a
// fake without standing up an HTTP server for every case.
type tokenVerifier func(ctx context.Context, ident meegle.ResolvedIdentity) string

// verifyTokenViaListTools is the production tokenVerifier. It builds an MCP
// client from the resolved identity and delegates to verifyMCPClient for
// the actual probe. Split so tests drive verifyMCPClient against an
// httptest server, bypassing GetServerURL's hardcoded https URL.
func verifyTokenViaListTools(ctx context.Context, ident meegle.ResolvedIdentity) string {
	client := meegle.NewMCPClientFromIdentity(ident, mcpclient.WithHTTPClient(auth.HTTPClient(ctx)))
	if client == nil {
		// Defensive: caller should not invoke the verifier without a token,
		// but if they do, surface a clear reason instead of a nil panic.
		return statusReasonNoLocalToken
	}
	return verifyMCPClient(ctx, client)
}

// verifyMCPClient issues a single tools/list call to validate that the
// token attached to client is still accepted by the server. Returns "" on
// success, statusReasonTokenRejected on terminal auth rejection (raw 401 or
// AUTH_EXPIRED after refresh fails), or a "server unreachable: <err>" wrapping
// for any other error so users can tell connection refused apart from timeout
// apart from 5xx without parsing logs.
func verifyMCPClient(ctx context.Context, client *mcpclient.Client) string {
	if _, err := client.ListTools(ctx); err != nil {
		if meerrors.IsUnauthorized(err) {
			return statusReasonTokenRejected
		}
		return statusReasonServerUnreachable + ": " + err.Error()
	}
	return ""
}

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

			ctx := cmd.Context()

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
						invalidateProfileCaches(profileName)
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
				invalidateProfileCaches(profileName)
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
	invalidateProfileCaches(profileName)

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
//
// Reason is populated only when Authenticated is false and distinguishes
// "no local token" (need login) from "token rejected by server" (need
// re-login — refresh exhausted) from "server unreachable: <err>" (network
// / timeout / 5xx — retryable). Cron preflights branch on this.
type StatusResult struct {
	Authenticated    bool    `json:"authenticated"`
	Host             *string `json:"host"`
	ExpiresInMinutes *int    `json:"expires_in_minutes,omitempty"`
	Reason           string  `json:"reason,omitempty"`
}

// buildStatusResult resolves the active identity and, when a token is
// present locally, calls verify to confirm the token is still accepted by
// the server. verify is injected so tests substitute a deterministic stub
// — production paths wire verifyTokenViaListTools.
func buildStatusResult(ctx context.Context, profileName string, verify tokenVerifier) (*StatusResult, error) {
	ident, hasSnapshot := meegle.CLIIdentityFromContext(ctx)
	if !hasSnapshot {
		cfg, err := loadConfig(profileName)
		if err != nil {
			return nil, err
		}
		resolved, resolveErr := meegle.ResolveCLIIdentity(ctx, cfg, profileName)
		if resolveErr != nil {
			return nil, resolveErr
		}
		ident = resolved
	}

	var host *string
	if ident.Host != "" {
		host = &ident.Host
	}

	if ident.Token == "" {
		return &StatusResult{
			Authenticated: false,
			Host:          host,
			Reason:        statusReasonNoLocalToken,
		}, nil
	}

	// Server-side validation: even if we hold a local token string, the
	// server may have expired or revoked it. Without this probe, cron
	// preflights see authenticated:true followed by a 401 on the next
	// business call (the bug this command was fixing).
	if reason := verify(ctx, ident); reason != "" {
		return &StatusResult{
			Authenticated: false,
			Host:          host,
			Reason:        reason,
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

// statusExitCode maps a StatusResult to the CLI exit code. Authenticated
// → 0. Missing or rejected token → 1 (re-login needed). Server unreachable
// → 2 (retryable; cron should retry rather than re-login).
func statusExitCode(r *StatusResult) int {
	if r.Authenticated {
		return 0
	}
	if strings.HasPrefix(r.Reason, statusReasonServerUnreachable) {
		return 2
	}
	return 1
}

// StatusExitError carries the already-rendered auth status exit code back to
// the public process entry without calling os.Exit from embeddable code.
type StatusExitError struct{ Code int }

func (e *StatusExitError) Error() string {
	return fmt.Sprintf("authentication status exit code %d", e.Code)
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

			ctx, cancel := context.WithTimeout(cmd.Context(), statusVerifyTimeout)
			defer cancel()

			result, err := buildStatusResult(ctx, profileName, verifyTokenViaListTools)
			if err != nil {
				return err
			}
			renderStatus(result, profileName, format)
			if code := statusExitCode(result); code != 0 {
				return &StatusExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "Output format: json, table, ndjson")
	return cmd
}

// renderStatus writes the status to stdout in the requested format. Split
// out so tests can assert the rendered output without intercepting os.Exit.
func renderStatus(result *StatusResult, profileName, format string) {
	if format == "json" || format == "ndjson" {
		m := map[string]any{
			"authenticated": result.Authenticated,
			"host":          result.Host,
		}
		if result.Authenticated {
			m["expires_in_minutes"] = result.ExpiresInMinutes
		}
		if result.Reason != "" {
			m["reason"] = result.Reason
		}
		text, err := renderPayload(m, format)
		if err != nil {
			// renderPayload only fails on encoder bugs; surface to stderr
			// instead of swallowing so the failure is at least visible.
			fmt.Fprintln(os.Stderr, err)
			return
		}
		fmt.Println(text)
		return
	}

	// Human-readable text output (table format)
	hostStr := ""
	if result.Host != nil {
		hostStr = fmt.Sprintf("\n  Host:    %s", *result.Host)
	}
	if !result.Authenticated {
		label := "✗ Not authenticated"
		switch {
		case result.Reason == statusReasonTokenRejected:
			label = "✗ Token rejected by server"
		case strings.HasPrefix(result.Reason, statusReasonServerUnreachable):
			// Reason carries the underlying error after the colon.
			label = "✗ Server unreachable: " + strings.TrimPrefix(result.Reason, statusReasonServerUnreachable+": ")
		}
		fmt.Printf("  Profile: %s%s\n  Status:  %s\n", profileName, hostStr, label)
		return
	}

	status := "✓ Authenticated"
	if result.ExpiresInMinutes != nil {
		if *result.ExpiresInMinutes <= 0 {
			status = "✗ Token expired"
		} else {
			status = fmt.Sprintf("✓ Authenticated (%d minutes remaining)", *result.ExpiresInMinutes)
		}
	}
	fmt.Printf("  Profile: %s%s\n  Status:  %s\n", profileName, hostStr, status)
}
