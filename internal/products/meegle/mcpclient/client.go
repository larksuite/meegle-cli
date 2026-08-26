// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
)

const (
	maxToolsListResponseBytes = 8 << 20
	maxRPCResponseBytes       = 32 << 20
	maxCredentialRedirects    = 10
	maxToolDefinitionBytes    = 256 << 10
	maxDiscoveredTools        = 2048
	maxToolParameters         = 512
	maxToolTextBytes          = 16 << 10
)

var requestID atomic.Int64

var errUnsupportedSchemaUnion = errors.New("unsupported schema union")

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
	if c.authHeader != "" {
		// Custom auth mode has one credential source: tokenFunc. Static
		// headers may come from a profile created before the extension selected
		// its own header, so remove both possible credential headers before
		// writing the current token below.
		req.Header.Del("Authorization")
		req.Header.Del(c.authHeader)
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

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if req.Header.Get("Authorization") != "" || (c.authHeader != "" && req.Header.Get(c.authHeader) != "") {
		httpClient = withCredentialRedirectGuard(httpClient, snapshotOrigin(req.URL))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		var redirectErr *crossOriginRedirectError
		if errors.As(err, &redirectErr) {
			return nil, fmt.Errorf("http request: %w", redirectErr)
		}
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

	maxResponseBytes := int64(maxRPCResponseBytes)
	if method == "tools/list" {
		maxResponseBytes = maxToolsListResponseBytes
	}
	rpcResp, err := decodeRPCResponse(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, meerrors.NewServerError("SERVER_CALL_FAILED", rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func decodeRPCResponse(reader io.Reader, maxBytes int64) (jsonRPCResponse, error) {
	var response jsonRPCResponse
	if maxBytes <= 0 {
		return response, json.NewDecoder(reader).Decode(&response)
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return response, err
	}
	if int64(len(payload)) > maxBytes {
		return response, fmt.Errorf("response exceeds %d-byte limit", maxBytes)
	}
	return response, json.Unmarshal(payload, &response)
}

type crossOriginRedirectError struct {
	from string
	to   string
}

func (e *crossOriginRedirectError) Error() string {
	return fmt.Sprintf("credential security: cross-origin redirect from %s to %s rejected", e.from, e.to)
}

func withCredentialRedirectGuard(base *http.Client, source redirectOrigin) *http.Client {
	clone := *base
	baseRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxCredentialRedirects {
			return fmt.Errorf("credential security: stopped after %d redirects", maxCredentialRedirects)
		}
		if err := rejectCrossOriginRedirect(request, source); err != nil {
			return err
		}
		if baseRedirect != nil {
			if err := baseRedirect(request, via); err != nil {
				return err
			}
		}
		return rejectCrossOriginRedirect(request, source)
	}
	return &clone
}

type redirectOrigin struct {
	scheme   string
	hostname string
	port     string
	label    string
	valid    bool
}

func snapshotOrigin(value *url.URL) redirectOrigin {
	if value == nil {
		return redirectOrigin{}
	}
	return redirectOrigin{
		scheme:   strings.ToLower(value.Scheme),
		hostname: strings.ToLower(value.Hostname()),
		port:     effectivePort(value),
		label:    originLabel(value),
		valid:    true,
	}
}

func rejectCrossOriginRedirect(request *http.Request, source redirectOrigin) error {
	if request == nil || request.URL == nil || !source.valid {
		return nil
	}
	if sameOrigin(source, request.URL) {
		return nil
	}
	return &crossOriginRedirectError{from: source.label, to: originLabel(request.URL)}
}

func sameOrigin(left redirectOrigin, right *url.URL) bool {
	if !left.valid || right == nil || left.scheme != strings.ToLower(right.Scheme) || left.hostname != strings.ToLower(right.Hostname()) {
		return false
	}
	return left.port == effectivePort(right)
}

func originLabel(value *url.URL) string {
	if value == nil {
		return "<unknown>"
	}
	return strings.ToLower(value.Scheme) + "://" + value.Host
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (c *Client) ListTools(ctx context.Context) ([]types.ToolDefinition, error) {
	raw, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	toolCount := len(result.Tools)
	if toolCount > maxDiscoveredTools {
		toolCount = maxDiscoveredTools
	}
	tools := make([]types.ToolDefinition, 0, toolCount+1)
	for index, rawTool := range result.Tools[:toolCount] {
		if len(rawTool) > maxToolDefinitionBytes {
			name, path := malformedToolIdentity(rawTool, index)
			tools = append(tools, types.ToolDefinition{
				Name: name, Issue: &types.ToolDefinitionIssue{Code: "tool_definition_too_large", Path: path},
			})
			continue
		}
		tool, parseErr := parseToolDefinition(rawTool)
		if parseErr != nil {
			name, path := malformedToolIdentity(rawTool, index)
			issueCode := "invalid_tool_definition"
			if errors.Is(parseErr, errUnsupportedSchemaUnion) {
				issueCode = "unsupported_schema_union"
			}
			tools = append(tools, types.ToolDefinition{
				Name:  name,
				Issue: &types.ToolDefinitionIssue{Code: issueCode, Path: path},
			})
			continue
		}
		tools = append(tools, tool)
	}
	if len(result.Tools) > toolCount {
		tools = append(tools, types.ToolDefinition{
			Name:  fmt.Sprintf("tools[%d:]", toolCount),
			Issue: &types.ToolDefinitionIssue{Code: "tool_count_limit_exceeded"},
		})
	}
	return tools, nil
}

func parseToolDefinition(raw json.RawMessage) (types.ToolDefinition, error) {
	var wire struct {
		Name        string              `json:"name"`
		Description string              `json:"description"`
		Metadata    *types.ToolMetadata `json:"metadata,omitempty"`
		InputSchema *struct {
			Type       json.RawMessage            `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return types.ToolDefinition{}, err
	}
	if strings.TrimSpace(wire.Name) == "" {
		return types.ToolDefinition{}, errors.New("tool name is required")
	}
	if len(wire.Name) > maxToolTextBytes || len(wire.Description) > maxToolTextBytes {
		return types.ToolDefinition{}, errors.New("tool name or description exceeds size limit")
	}

	tool := types.ToolDefinition{Name: wire.Name, Description: wire.Description, Metadata: wire.Metadata}
	if wire.InputSchema == nil {
		return tool, nil
	}
	schemaType, err := normalizeSchemaType(wire.InputSchema.Type, "object")
	if err != nil || schemaType != "object" {
		if err == nil {
			err = fmt.Errorf("input schema type must be object, got %q", schemaType)
		}
		return types.ToolDefinition{}, err
	}
	if len(wire.InputSchema.Properties) > maxToolParameters || len(wire.InputSchema.Required) > maxToolParameters {
		return types.ToolDefinition{}, errors.New("tool parameter count exceeds limit")
	}
	required := make(map[string]bool, len(wire.InputSchema.Required))
	for _, name := range wire.InputSchema.Required {
		required[name] = true
	}
	propertyNames := make([]string, 0, len(wire.InputSchema.Properties))
	for name := range wire.InputSchema.Properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	for _, name := range propertyNames {
		if len(name) > maxToolTextBytes {
			return types.ToolDefinition{}, errors.New("tool parameter name exceeds size limit")
		}
		rawProperty := wire.InputSchema.Properties[name]
		var property *struct {
			Type        json.RawMessage `json:"type"`
			Description string          `json:"description"`
			Items       *struct {
				Type json.RawMessage `json:"type"`
			} `json:"items,omitempty"`
		}
		if err := json.Unmarshal(rawProperty, &property); err != nil || property == nil {
			if err == nil {
				err = errors.New("property schema is null")
			}
			return types.ToolDefinition{}, fmt.Errorf("parse property %q: %w", name, err)
		}
		if len(property.Description) > maxToolTextBytes {
			return types.ToolDefinition{}, fmt.Errorf("property %q description exceeds size limit", name)
		}
		parameterType, err := normalizeSchemaType(property.Type, "string")
		if err != nil {
			return types.ToolDefinition{}, fmt.Errorf("parse property %q type: %w", name, err)
		}
		parameter := types.ToolParameter{
			Name: name, Type: parameterType, Description: property.Description, Required: required[name],
		}
		if property.Items != nil {
			itemsType, itemsErr := normalizeSchemaType(property.Items.Type, "")
			if itemsErr != nil {
				return types.ToolDefinition{}, fmt.Errorf("parse property %q items type: %w", name, itemsErr)
			}
			parameter.Items = &types.ParameterItems{Type: itemsType}
		}
		tool.Parameters = append(tool.Parameters, parameter)
	}
	return tool, nil
}

func normalizeSchemaType(raw json.RawMessage, defaultType string) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return defaultType, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return "", errors.New("schema type must not be empty")
		}
		return single, nil
	}
	var union []string
	if err := json.Unmarshal(raw, &union); err != nil {
		return "", errors.New("schema type must be a string or string array")
	}
	nonNull := ""
	for _, candidate := range union {
		if candidate == "null" {
			continue
		}
		if candidate == "" || (nonNull != "" && candidate != nonNull) {
			return "", fmt.Errorf("%w %q", errUnsupportedSchemaUnion, union)
		}
		nonNull = candidate
	}
	if nonNull == "" {
		return "", fmt.Errorf("%w %q", errUnsupportedSchemaUnion, union)
	}
	return nonNull, nil
}

func malformedToolIdentity(raw json.RawMessage, index int) (string, string) {
	name := fmt.Sprintf("tools[%d]", index)
	var identity struct {
		Name     string `json:"name"`
		Metadata *struct {
			Resource string `json:"resource"`
			Method   string `json:"method"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return name, ""
	}
	if strings.TrimSpace(identity.Name) != "" {
		name = identity.Name
	}
	if identity.Metadata == nil {
		return name, ""
	}
	return name, strings.Trim(identity.Metadata.Resource+"/"+identity.Metadata.Method, "/")
}

func (c *Client) CallTool(ctx context.Context, name string, params map[string]any) (*Response, error) {
	raw, err := c.Call(ctx, "tools/call", map[string]any{"name": name, "arguments": params})
	if err != nil {
		return nil, err
	}
	return unwrapResponse(raw)
}
