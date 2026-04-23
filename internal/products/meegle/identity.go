// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"net/http"
	"os"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
)

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

	// AccessTokenHeader is the HTTP header name that carries the token.
	// Empty = default "Authorization: Bearer <token>". Non-empty means
	// "<header>: <token>" with no prefix and Authorization suppressed —
	// used by backends that require a custom auth header.
	AccessTokenHeader string

	// UserAgentCaller is the caller suffix appended to the default
	// "meegle-cli[/version]" User-Agent. Empty means do not append.
	// Priority: MEEGLE_USER_AGENT env > config.user_agent (post ExpandEnv).
	UserAgentCaller string

	// TokenManager and Store are populated only when Source == SourceStore,
	// i.e. when the caller may legitimately refresh or clear the token.
	TokenManager *auth.TokenManager
	Store        auth.TokenStore
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
	expanded, err := cfg.ExpandEnv()
	if err != nil {
		return ResolvedIdentity{}, err
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
	default:
		store := auth.CreateTokenStore(profileName)
		tm := auth.NewTokenManager(store, out.Host, headerMapToHTTP(cfg.Headers))
		if token, _ := tm.GetToken(); token != "" {
			out.Token = token
			out.Source = SourceStore
			out.TokenManager = tm
			out.Store = store
		}
	}
	return out, nil
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
