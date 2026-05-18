// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"net/http"
	"runtime/debug"
)

type Option func(*Client)

func WithToken(fn func() (string, error)) Option {
	return func(c *Client) { c.tokenFunc = fn }
}

func WithHeaders(h http.Header) Option {
	return func(c *Client) { c.headers = h }
}

func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithRefreshFunc sets a function called on 401 to refresh the token.
func WithRefreshFunc(fn func() error) Option {
	return func(c *Client) {
		c.refreshFunc = fn
	}
}

// WithAuthHeader sets the HTTP header name that carries the access token.
// When non-empty, the token is sent as "<name>: <token>" (raw, no prefix)
// and the default "Authorization: Bearer <token>" is suppressed. Use this
// for backends that require a custom auth header instead of Bearer.
func WithAuthHeader(name string) Option {
	return func(c *Client) { c.authHeader = name }
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// injectedVersion is the version string set by main packages via SetVersion.
// When non-empty, DefaultUserAgent prefers it over debug.ReadBuildInfo().
// Reason: the npm release build injects the semantic version through
// `-ldflags "-X main.version=..."`, and that value is the one product/users
// identify releases by — not Go's pseudo-version (which carries commit hashes
// and a "+dirty" suffix when the build tree has uncommitted files).
var injectedVersion string

// SetVersion records the semantic version reported in User-Agent. Main
// packages should call this before constructing the CLI app so the first
// outbound request already carries the correct UA. Empty / "dev" inputs are
// ignored to preserve the debug.ReadBuildInfo() fallback in local builds.
func SetVersion(v string) {
	if v == "" || v == "dev" {
		return
	}
	injectedVersion = v
}

// DefaultUserAgent returns "meegle-cli" with version appended if available.
// e.g. "meegle-cli" or "meegle-cli/1.0.1"
//
// Version source priority:
//  1. injectedVersion (set via SetVersion, populated from `-ldflags "-X main.version=..."`)
//  2. debug.ReadBuildInfo().Main.Version (Go module version — SDK consumers
//     and local `go build` invocations that bypass the Makefile)
func DefaultUserAgent() string {
	base := "meegle-cli"
	if injectedVersion != "" {
		return base + "/" + injectedVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if v != "" && v != "(devel)" {
			base += "/" + v
		}
	}
	return base
}

// BuildUserAgent returns the full User-Agent string to use for outbound MCP
// requests. caller is the optional caller identifier appended after a single
// space (RFC 7231). caller == "" returns DefaultUserAgent() unchanged.
//
// This is the single source of truth for the UA format: both the SDK
// (sdk.ClientConfig.buildUserAgent) and the CLI startup / runtime paths
// (buildMcpClient, SessionStep) delegate here.
func BuildUserAgent(caller string) string {
	base := DefaultUserAgent()
	if caller == "" {
		return base
	}
	return base + " " + caller
}
