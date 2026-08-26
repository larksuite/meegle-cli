// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	credential "github.com/larksuite/meegle-cli/extension/credential"
	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

const credentialCallbackTimeout = 30 * time.Second

// cliConfigResolutionError identifies a built-in profile/config failure that
// recovery commands may defer until a command actually needs credentials.
// Extension callback failures deliberately use their existing fail-closed
// startup path and are never wrapped as this type.
type cliConfigResolutionError struct {
	cause error
}

func (e *cliConfigResolutionError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *cliConfigResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.cause)
}

func isCLIConfigResolutionError(err error) bool {
	var configErr *cliConfigResolutionError
	return frameworkerrors.SafeAs(err, &configErr)
}

// IdentitySource indicates where the active user_access_token was sourced
// from. Callers use it to branch 401 hints and logging: the rotation knob
// differs depending on whether the token came from the environment, the
// profile file, or the managed keychain.
type IdentitySource int

const (
	// SourceUnset means no token is available from any source. Host may still
	// be set (e.g. zero-config CI exported only MEEGLE_HOST).
	SourceUnset IdentitySource = iota
	// SourceEnv means the token came from MEEGLE_USER_ACCESS_TOKEN.
	SourceEnv
	// SourceConfig means the token came from the profile's user_access_token
	// field (post ${VAR} expansion).
	SourceConfig
	// SourceStore means the token was retrieved from the keychain / encrypted
	// file maintained by `meegle auth login`. TokenManager is attached so the
	// caller may refresh or clear it.
	SourceStore
	// SourceExtension means a registered CLI credential Provider supplied the
	// active token. SDK callers never consult the CLI extension registry.
	SourceExtension
)

// String is for debug logging only — do not parse.
func (s IdentitySource) String() string {
	switch s {
	case SourceEnv:
		return "env"
	case SourceConfig:
		return "config"
	case SourceStore:
		return "store"
	case SourceExtension:
		return "extension"
	default:
		return "unset"
	}
}

// ResolvedIdentity is the single authoritative output of identity resolution.
// Callers must not re-read env/config/store once they hold a ResolvedIdentity:
// all inputs are already folded in by priority.
type ResolvedIdentity struct {
	Host    string
	Headers map[string]string
	Token   string
	Source  IdentitySource
	// ProfileName is the effective profile after CLI flags and credential
	// provider account selection have been applied.
	ProfileName string

	// AccessTokenHeader is the HTTP header name that carries the token.
	// Empty = default "Authorization: Bearer <token>". Non-empty means
	// "<header>: <token>" with no prefix and Authorization suppressed —
	// used by backends that require a custom auth header.
	AccessTokenHeader string

	// UserAgentCaller is the caller suffix appended to the default
	// "meegle-cli[/version]" User-Agent. Empty means do not append.
	// Priority: MEEGLE_USER_AGENT env > config.user_agent (post ExpandEnv).
	UserAgentCaller string
	// MCPUserAgent is the fully assembled CLI User-Agent. CLI construction sets
	// it from the explicit distribution version so SDK defaults remain isolated.
	MCPUserAgent string

	// TokenManager and Store are populated only when Source == SourceStore,
	// i.e. when the caller may legitimately refresh or clear the token.
	TokenManager *auth.TokenManager
	Store        auth.TokenStore

	// CredentialProvider is the non-secret name of the extension that supplied
	// the active account or token. It is intended for diagnostics only.
	CredentialProvider string
	// CredentialTokenSource is a provider-supplied, non-secret source label
	// used only by diagnostics (for example "oidc" or "workload-identity").
	CredentialTokenSource string
}

type cliIdentityContextKey struct{}
type cliProfileContextKey struct{}

func withCLIProfile(ctx context.Context, profileName string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cliProfileContextKey{}, profileName)
}

func cliProfileFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	profileName, ok := ctx.Value(cliProfileContextKey{}).(string)
	return profileName, ok && profileName != ""
}

// WithCLIIdentity attaches the already-resolved CLI identity to a terminal
// execution context. Static commands reuse this snapshot instead of invoking
// credential providers a second time during the same process execution.
func WithCLIIdentity(ctx context.Context, identity ResolvedIdentity) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cliIdentityContextKey{}, identity)
}

// CLIIdentityFromContext returns the terminal CLI identity snapshot, when one
// was attached by NewCLIApp. SDK/programmatic contexts intentionally omit it.
func CLIIdentityFromContext(ctx context.Context) (ResolvedIdentity, bool) {
	if ctx == nil {
		return ResolvedIdentity{}, false
	}
	identity, ok := ctx.Value(cliIdentityContextKey{}).(ResolvedIdentity)
	return identity, ok
}

// ResolveCLIIdentity resolves the process-wide CLI credential extension chain
// in front of the existing env > config > store identity. SDK code continues
// to call ResolveIdentity directly and is therefore isolated from init-time
// CLI registrations.
func ResolveCLIIdentity(ctx context.Context, cfg MeegleConfig, profileName string) (ResolvedIdentity, error) {
	if err := credential.Validate(); err != nil {
		return ResolvedIdentity{}, err
	}
	registrations := credential.Registrations()
	accountProfile := profileName
	selectedConfig := cfg
	accountHost := ""
	accountProvider := ""
	var configLoadErr error
	for _, registration := range registrations {
		provider := registration.Provider
		providerName := registration.Name
		account, resolveErr := resolveExtensionAccount(ctx, providerName, provider)
		if resolveErr != nil {
			return ResolvedIdentity{}, resolveErr
		}
		if account == nil {
			continue
		}
		if account.ProfileName != "" {
			accountProfile = account.ProfileName
			selectedConfig, configLoadErr = LoadConfig(accountProfile)
		}
		if account.Host != "" {
			accountHost = sanitizeHost(account.Host)
		}
		accountProvider = providerName
		break
	}

	var (
		resolved   ResolvedIdentity
		expanded   MeegleConfig
		builtInErr error
	)
	if configLoadErr != nil {
		builtInErr = configLoadErr
	} else {
		resolved, expanded, builtInErr = resolveIdentityWithoutStore(selectedConfig)
	}
	if accountHost != "" {
		resolved.Host = accountHost
	}
	resolved.CredentialProvider = accountProvider

	spec := credential.TokenSpec{Host: resolved.Host, ProfileName: accountProfile}
	extensionTokenResolved := false
	for _, registration := range registrations {
		provider := registration.Provider
		providerName := registration.Name
		token, resolveErr := resolveExtensionToken(ctx, providerName, provider, spec)
		if resolveErr != nil {
			return ResolvedIdentity{}, resolveErr
		}
		if token == nil {
			continue
		}
		if token.Value == "" {
			return ResolvedIdentity{}, fmt.Errorf("credential provider %q returned an empty token", providerName)
		}
		resolved.Token = token.Value
		resolved.Source = SourceExtension
		resolved.TokenManager = nil
		resolved.Store = nil
		resolved.CredentialProvider = providerName
		resolved.CredentialTokenSource = safeCredentialLabel(token.Source, "extension")
		if token.Header == "" || http.CanonicalHeaderKey(token.Header) == "Authorization" {
			resolved.AccessTokenHeader = ""
		} else {
			if !validHTTPHeaderName(token.Header) {
				return ResolvedIdentity{}, fmt.Errorf("credential provider %q returned an invalid token header", providerName)
			}
			resolved.AccessTokenHeader = token.Header
		}
		extensionTokenResolved = true
		break
	}
	// An extension may take over a broken built-in configuration only when it
	// supplies a complete usable identity. A token alone must not hide the
	// original error and leave callers with an empty Host.
	if builtInErr != nil && (!extensionTokenResolved || resolved.Host == "") {
		return ResolvedIdentity{}, &cliConfigResolutionError{cause: builtInErr}
	}
	if !extensionTokenResolved && resolved.Token == "" {
		resolved = attachStoredToken(resolved, accountProfile, expanded, auth.HTTPClient(ctx))
	}
	resolved.ProfileName = accountProfile

	return resolved, nil
}

func resolveExtensionAccount(ctx context.Context, providerName string, provider credential.Provider) (*credential.Account, error) {
	ctx, cancel := withCredentialTimeout(ctx)
	defer cancel()
	result := make(chan accountResolution, 1)
	go func() {
		value := accountResolution{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.err = credentialPanicCause(recovered)
			}
			result <- value
		}()
		value.account, value.err = provider.ResolveAccount(ctx)
	}()
	select {
	case value := <-result:
		if value.err != nil {
			return nil, fmt.Errorf("credential provider %q resolve account: %w", providerName, frameworkerrors.GuardCause(value.err))
		}
		return value.account, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("credential provider %q resolve account: %w", providerName, ctx.Err())
	}
}

func resolveExtensionToken(ctx context.Context, providerName string, provider credential.Provider, spec credential.TokenSpec) (*credential.Token, error) {
	ctx, cancel := withCredentialTimeout(ctx)
	defer cancel()
	result := make(chan tokenResolution, 1)
	go func() {
		value := tokenResolution{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.err = credentialPanicCause(recovered)
			}
			result <- value
		}()
		value.token, value.err = provider.ResolveToken(ctx, spec)
	}()
	select {
	case value := <-result:
		if value.err != nil {
			return nil, fmt.Errorf("credential provider %q resolve token: %w", providerName, frameworkerrors.GuardCause(value.err))
		}
		return value.token, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("credential provider %q resolve token: %w", providerName, ctx.Err())
	}
}

type accountResolution struct {
	account *credential.Account
	err     error
}

type tokenResolution struct {
	token *credential.Token
	err   error
}

func withCredentialTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= credentialCallbackTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, credentialCallbackTimeout)
}

func credentialPanicCause(recovered any) error {
	if err, ok := recovered.(error); ok {
		return &credentialPanicError{cause: err}
	}
	return fmt.Errorf("panic: %v", recovered)
}

type credentialPanicError struct{ cause error }

func (*credentialPanicError) Error() string { return "credential callback panicked with an error" }
func (e *credentialPanicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.cause)
}

func safeCredentialLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if len(value) > 64 {
		return "<invalid>"
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return "<invalid>"
	}
	return value
}

func validHTTPHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

// ResolveIdentity consolidates config expansion, env override, host
// sanitization and token lookup into a single call. It is the single source
// of truth for identity state across startup (buildMcpClient) and per-command
// execution (SessionStep).
//
// Priority:
//  1. MEEGLE_HOST / MEEGLE_USER_ACCESS_TOKEN environment variables
//  2. Profile fields after ${VAR} expansion (host, user_access_token, headers)
//  3. TokenStore for the given profile (token only, via `auth login`)
//
// Returns a non-nil error only when ExpandEnv fails — i.e. a ${VAR}
// placeholder references an unset environment variable. A missing token is
// not an error: callers inspect Source == SourceUnset to decide whether to
// degrade silently (startup) or surface a user-facing error (per-command).
func ResolveIdentity(cfg MeegleConfig, profileName string) (ResolvedIdentity, error) {
	resolved, expanded, err := resolveIdentityWithoutStore(cfg)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if resolved.Token == "" {
		resolved = attachStoredToken(resolved, profileName, expanded, nil)
	}
	resolved.ProfileName = profileName
	return resolved, nil
}

func resolveIdentityWithoutStore(cfg MeegleConfig) (ResolvedIdentity, MeegleConfig, error) {
	expanded, err := cfg.ExpandEnv()
	if err != nil {
		return ResolvedIdentity{}, MeegleConfig{}, err
	}
	cfg = expanded

	out := ResolvedIdentity{
		Host:              cfg.Host,
		Headers:           cfg.Headers,
		AccessTokenHeader: cfg.AccessTokenHeader,
		UserAgentCaller:   cfg.UserAgent,
	}

	if h := os.Getenv("MEEGLE_HOST"); h != "" {
		out.Host = h
	}
	if ah := os.Getenv("MEEGLE_ACCESS_TOKEN_HEADER"); ah != "" {
		out.AccessTokenHeader = ah
	}
	if ua := os.Getenv("MEEGLE_USER_AGENT"); ua != "" {
		out.UserAgentCaller = ua
	}
	out.Host = sanitizeHost(out.Host)

	switch {
	case os.Getenv("MEEGLE_USER_ACCESS_TOKEN") != "":
		out.Token = os.Getenv("MEEGLE_USER_ACCESS_TOKEN")
		out.Source = SourceEnv
	case cfg.AccessToken != "":
		out.Token = cfg.AccessToken
		out.Source = SourceConfig
	}
	return out, cfg, nil
}

func attachStoredToken(out ResolvedIdentity, profileName string, cfg MeegleConfig, httpClient *http.Client) ResolvedIdentity {
	store := auth.CreateTokenStore(profileName)
	tm := auth.NewTokenManager(store, out.Host, headerMapToHTTP(cfg.Headers)).WithHTTPClient(httpClient)
	if token, _ := tm.GetToken(); token != "" {
		out.Token = token
		out.Source = SourceStore
		out.TokenManager = tm
		out.Store = store
	}
	return out
}

func headerMapToHTTP(m map[string]string) http.Header {
	if len(m) == 0 {
		return nil
	}
	h := make(http.Header, len(m))
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}
