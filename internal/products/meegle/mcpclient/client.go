// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

var requestID atomic.Int64

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	baseURL     string
	httpClient  *http.Client
	tokenFunc   func() (string, error)
	refreshFunc func() error // called on 401 to refresh token
	headers     http.Header
	userAgent   string
	authHeader  string // custom header name for the token (empty = "Authorization: Bearer ...")
}

func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
		headers:    make(http.Header),
		userAgent:  DefaultUserAgent(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	result, err := c.doCall(ctx, method, params)
	if err != nil {
		// 401 -> try refresh + retry once
		if me, ok := err.(*meerrors.MeegleError); ok &&
			me.Code == "SERVER_HTTP_ERROR" &&
			me.HTTPStatus == 401 &&
			c.refreshFunc != nil {
			if refreshErr := c.refreshFunc(); refreshErr == nil {
				return c.doCall(ctx, method, params)
			}
			return nil, meerrors.NewClientError("AUTH_EXPIRED", "authentication expired, please log in again").
				WithSuggestion("meegle auth login")
		}
		return nil, err
	}
	return result, nil
}

func (c *Client) doCall(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := requestID.Add(1)
	body := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if c.tokenFunc != nil {
		token, err := c.tokenFunc()
		if err != nil {
			return nil, fmt.Errorf("get token: %w", err)
		}
		if token != "" {
			if c.authHeader != "" {
				// Custom auth header mode: raw token, no Bearer prefix, and
				// importantly no Authorization header — some backends reject
				// requests that carry both headers.
				req.Header.Set(c.authHeader, token)
			} else {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if os.IsTimeout(err) || strings.Contains(err.Error(), "context deadline exceeded") {
			return nil, meerrors.NewServerError("SERVER_TIMEOUT",
				"request timed out, please check your network connection and retry").
				WithSuggestion("check your network or try again later")
		}
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, meerrors.NewServerError("SERVER_HTTP_ERROR",
			fmt.Sprintf("server returned error (%d)", resp.StatusCode)).
			WithHTTPStatus(resp.StatusCode)
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, meerrors.NewServerError("SERVER_CALL_FAILED", rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *Client) ListTools(ctx context.Context) ([]types.ToolDefinition, error) {
	raw, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema *struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	var tools []types.ToolDefinition
	for _, t := range result.Tools {
		td := types.ToolDefinition{Name: t.Name, Description: t.Description}
		if t.InputSchema != nil {
			reqSet := make(map[string]bool)
			for _, r := range t.InputSchema.Required {
				reqSet[r] = true
			}
			for name, rawProp := range t.InputSchema.Properties {
				var prop struct {
					Type        string `json:"type"`
					Description string `json:"description"`
					Items       *struct {
						Type string `json:"type"`
					} `json:"items,omitempty"`
				}
				json.Unmarshal(rawProp, &prop) //nolint:errcheck
				tp := prop.Type
				if tp == "" {
					tp = "string"
				}
				param := types.ToolParameter{
					Name: name, Type: tp, Description: prop.Description, Required: reqSet[name],
				}
				if prop.Items != nil {
					param.Items = &types.ParameterItems{Type: prop.Items.Type}
				}
				td.Parameters = append(td.Parameters, param)
			}
		}
		tools = append(tools, td)
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, params map[string]any) (*Response, error) {
	raw, err := c.Call(ctx, "tools/call", map[string]any{"name": name, "arguments": params})
	if err != nil {
		return nil, err
	}
	return unwrapResponse(raw)
}
