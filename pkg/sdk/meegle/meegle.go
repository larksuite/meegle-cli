// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package meegle provides Go SDK access to the Meegle CLI.
// Other RPC services can invoke CLI capabilities via command string + host/headers.
//
// Quick start:
//
//	c, err := meegle.NewClient("meegle.com",
//	    meegle.WithToken("xxx"),
//	)
//	out, err := c.Execute(ctx, "meegle workitem get-brief --work-item-id 123")
//
//	// With custom headers and User-Agent:
//	c, err := meegle.NewClient("staging.example.com",
//	    meegle.WithToken("xxx"),
//	    meegle.WithHeaders(map[string]string{"X-Env": "staging"}),
//	    meegle.WithUserAgent("my-service/1.0"),
//	)
package meegle

import (
	"context"

	sdk "github.com/larksuite/meegle-cli/internal/products/meegle/sdk"
)

// Option is an alias for sdk.CommandClientOption.
// All options are defined once in the sdk package.
type Option = sdk.CommandClientOption

// DiscoveryIssue describes one invalid or conflicting tools/list entry that
// was isolated while the remaining SDK registry stayed available.
type DiscoveryIssue = sdk.DiscoveryIssue

// Re-export option constructors so callers use meegle.WithXXX directly.
var (
	WithToken     = sdk.WithToken
	WithHeaders   = sdk.WithHeaders
	WithUserAgent = sdk.WithUserAgent
	WithQuery     = sdk.WithQuery
)

// Client is the programmatic entry point for executing Meegle command strings.
// It is concurrency-safe and can be shared across goroutines.
type Client struct {
	inner *sdk.CommandClient
}

// NewClient creates a command string execution client.
// host is the server domain (e.g. "meegle.com"); use opts for token, headers, etc.
func NewClient(host string, opts ...Option) (*Client, error) {
	inner, err := sdk.NewCommandClient(host, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{inner: inner}, nil
}

// Execute executes a command string and returns the formatted output bytes.
// cmdStr example: "meegle workitem get-brief --work-item-id 123"
func (c *Client) Execute(ctx context.Context, cmdStr string) ([]byte, error) {
	return c.inner.Execute(ctx, cmdStr)
}

// DiscoveryIssues returns deterministic, non-secret diagnostics for dynamic
// tools that were skipped when this client was constructed.
func (c *Client) DiscoveryIssues() []DiscoveryIssue {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.DiscoveryIssues()
}

// Execute is a package-level convenience function.
func Execute(ctx context.Context, host string, cmdStr string, opts ...Option) ([]byte, error) {
	return sdk.ExecuteCommand(ctx, host, cmdStr, opts...)
}
