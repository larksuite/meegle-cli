// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"
	"fmt"
	"net/http"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

// clientConfig collects options for NewClient.
type clientConfig struct {
	host    string
	token   string
	headers map[string]string
}

// directClientOption configures the direct MCP Client construction parameters.
type directClientOption func(*clientConfig)

func withDirectHost(host string) directClientOption {
	return func(c *clientConfig) { c.host = host }
}

func withDirectToken(token string) directClientOption {
	return func(c *clientConfig) { c.token = token }
}

func withDirectHeaders(headers map[string]string) directClientOption {
	return func(c *clientConfig) { c.headers = headers }
}

// Client is the programmatic facade for Meegle MCP calls.
type Client struct {
	mcp *mcpclient.Client
}

// NewClient creates a client from host + token, suitable for CI/script scenarios.
func NewClient(opts ...directClientOption) *Client {
	cfg := &clientConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	host := cfg.host
	if host == "" {
		host = "meegle.com"
	}
	baseURL := meegle.GetServerURL(meegle.MeegleConfig{Host: host})

	var mcpOpts []mcpclient.Option
	if cfg.token != "" {
		token := cfg.token
		mcpOpts = append(mcpOpts, mcpclient.WithToken(func() (string, error) {
			return token, nil
		}))
	}
	if len(cfg.headers) > 0 {
		h := make(http.Header)
		for k, v := range cfg.headers {
			h.Set(k, v)
		}
		mcpOpts = append(mcpOpts, mcpclient.WithHeaders(h))
	}

	return &Client{mcp: mcpclient.New(baseURL, mcpOpts...)}
}

// NewClientFromProfile creates a client from local profile configuration.
// Returns an error if the profile does not exist or has no valid token.
// Env vars (MEEGLE_HOST, MEEGLE_USER_ACCESS_TOKEN, MEEGLE_ACCESS_TOKEN_HEADER)
// take precedence over profile values, matching CLI pipeline behavior.
func NewClientFromProfile(profileName string) (*Client, error) {
	cfg, err := meegle.LoadConfig(profileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ident, err := meegle.ResolveIdentity(cfg, profileName)
	if err != nil {
		return nil, fmt.Errorf("resolve config env vars: %w", err)
	}
	if ident.Host == "" {
		return nil, fmt.Errorf("profile %q has no host configured, please run 'meegle config init' first", profileName)
	}
	if ident.Token == "" {
		return nil, fmt.Errorf("profile %q has no valid token, please run 'meegle auth login' first", profileName)
	}

	baseURL := meegle.GetServerURL(meegle.MeegleConfig{Host: ident.Host})

	httpHeaders := make(http.Header)
	for k, v := range ident.Headers {
		httpHeaders.Set(k, v)
	}

	token := ident.Token
	opts := []mcpclient.Option{
		mcpclient.WithToken(func() (string, error) { return token, nil }),
		mcpclient.WithHeaders(httpHeaders),
	}
	if ident.AccessTokenHeader != "" {
		opts = append(opts, mcpclient.WithAuthHeader(ident.AccessTokenHeader))
	}

	return &Client{mcp: mcpclient.New(baseURL, opts...)}, nil
}

// NewClientWithMcpClient creates a client using an existing mcpclient.Client, suitable for testing/mocking.
func NewClientWithMcpClient(client *mcpclient.Client) *Client {
	return &Client{mcp: client}
}

// CallTool calls the specified MCP tool and returns the parsed response data.
func (c *Client) CallTool(ctx context.Context, toolName string, params map[string]any) (any, error) {
	resp, err := c.mcp.CallTool(ctx, toolName, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return resp.Data, nil
}
