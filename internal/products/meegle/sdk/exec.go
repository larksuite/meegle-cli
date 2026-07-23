// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// CommandClient is the programmatic entry point for executing Meegle command strings.
// It is concurrency-safe and can be shared across goroutines.
type CommandClient struct {
	app *cliapp.App
}

// ClientConfig holds all configuration for NewCommandClient.
// Host is required; everything else is optional.
// Add new fields here — they propagate to both discovery and execution automatically.
type ClientConfig struct {
	// Host is the server domain (e.g. "meegle.com", "staging.example.com").
	// Set via the positional argument of NewCommandClient.
	Host string

	// Token is the user access token (without "Bearer " prefix).
	// Mutually exclusive with Authorization in Headers.
	Token string

	// Headers are extra HTTP headers sent with every MCP request (e.g. X-Env).
	// Do NOT put Authorization here when using WithToken.
	Headers map[string]string

	// UserAgent is an optional caller identifier appended to the base User-Agent.
	// e.g. "dola" → "meegle-cli dola"
	UserAgent string

	// Query are extra query parameters appended to the MCP server URL.
	Query map[string]string
}

// CommandClientOption configures optional parameters for NewCommandClient.
type CommandClientOption func(*ClientConfig)

// WithToken sets the user access token (without "Bearer " prefix).
// Mutually exclusive with passing Authorization in WithHeaders.
func WithToken(token string) CommandClientOption {
	return func(c *ClientConfig) { c.Token = token }
}

// WithHeaders sets extra HTTP headers for MCP requests (e.g. X-Env).
// Do NOT include Authorization here when using WithToken.
func WithHeaders(headers map[string]string) CommandClientOption {
	return func(c *ClientConfig) { c.Headers = headers }
}

// WithUserAgent appends a caller identifier to the User-Agent header.
// Format: "meegle-cli[/version] caller"  (RFC 7231 compliant)
// e.g. WithUserAgent("dola") → "meegle-cli dola"
func WithUserAgent(ua string) CommandClientOption {
	return func(c *ClientConfig) { c.UserAgent = ua }
}

// WithQuery appends query parameters to the MCP server URL.
func WithQuery(query map[string]string) CommandClientOption {
	return func(c *ClientConfig) { c.Query = query }
}

// validate checks for configuration conflicts.
func (c *ClientConfig) validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Token != "" && c.Headers != nil {
		for k := range c.Headers {
			if strings.EqualFold(k, "Authorization") {
				return fmt.Errorf("WithToken and Authorization header are mutually exclusive, use one or the other")
			}
		}
	}
	return nil
}

// serverURL builds the full MCP server URL with optional query parameters.
func (c *ClientConfig) serverURL() string {
	host := c.Host
	if strings.Contains(host, "://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Host
		}
	}
	base := fmt.Sprintf("https://%s/mcp_server/v1", host)
	if len(c.Query) == 0 {
		return base
	}
	q := url.Values{}
	for k, v := range c.Query {
		q.Set(k, v)
	}
	return base + "?" + q.Encode()
}

// resolveToken returns the token from WithToken or falls back to Headers.
func (c *ClientConfig) resolveToken() string {
	if c.Token != "" {
		return c.Token
	}
	for k, v := range c.Headers {
		if strings.EqualFold(k, "Authorization") {
			return strings.TrimPrefix(v, "Bearer ")
		}
	}
	return ""
}

// httpHeaders builds http.Header from Headers, excluding Authorization
// (which is handled separately via resolveToken + WithToken).
func (c *ClientConfig) httpHeaders() http.Header {
	h := make(http.Header)
	for k, v := range c.Headers {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		h.Set(k, v)
	}
	return h
}

// buildUserAgent returns the full User-Agent string.
// Format: "meegle-cli[/version] [caller]"
//   - no caller: "meegle-cli" or "meegle-cli/0.1.0" (DefaultUserAgent)
//   - with caller: "meegle-cli dola" or "meegle-cli/0.1.0 dola"
//
// Delegates to mcpclient.BuildUserAgent to keep a single source of truth.
func (c *ClientConfig) buildUserAgent() string {
	return mcpclient.BuildUserAgent(c.UserAgent)
}

// mcpClientOpts converts ClientConfig to mcpclient.Option slice.
// Single source of truth: used by both discovery and execution.
func (c *ClientConfig) mcpClientOpts() []mcpclient.Option {
	var opts []mcpclient.Option
	if token := c.resolveToken(); token != "" {
		opts = append(opts, mcpclient.WithToken(func() (string, error) {
			return token, nil
		}))
	}
	if h := c.httpHeaders(); len(h) > 0 {
		opts = append(opts, mcpclient.WithHeaders(h))
	}
	if c.UserAgent != "" {
		// Caller specified — build full UA with version + caller
		opts = append(opts, mcpclient.WithUserAgent(c.buildUserAgent()))
	}
	return opts
}

// NewCommandClient creates a command string execution client.
// host is the server domain (e.g. "meegle.com"); use opts for token, headers, etc.
func NewCommandClient(host string, opts ...CommandClientOption) (*CommandClient, error) {
	cfg := &ClientConfig{Host: host}
	for _, o := range opts {
		o(cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	serverURL := cfg.serverURL()
	mcpOpts := cfg.mcpClientOpts()

	client := mcpclient.New(serverURL, mcpOpts...)
	registrySetup := meegle.NewDynamicRegistrySetup(client, nil,
		meegle.WithGlobalFlags(meegle.MeegleGlobalFlags),
	)

	placeholderExec := executor.Func(func(_ context.Context, _ *executor.Request) (*executor.RawResult, error) {
		return nil, fmt.Errorf("executor not implemented: please execute commands through pipeline steps")
	})

	app, err := cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithSetup(registrySetup),
		cliapp.WithExecutor(placeholderExec),
		cliapp.WithPipelineFactory(newSDKPipelineFactory(cfg, registrySetup)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build CLI app: %w", err)
	}

	return &CommandClient{app: app}, nil
}

// Execute executes a command string and returns the formatted output bytes.
// cmdStr example: "meegle workitem get-brief --work-item-id 123"
func (c *CommandClient) Execute(ctx context.Context, cmdStr string) ([]byte, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("client not initialized")
	}
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return nil, fmt.Errorf("empty command string")
	}
	if err := rejectShellOperators(cmdStr); err != nil {
		return nil, err
	}

	args := parseArgs(c.app, cmdStr)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := c.app.ExecuteWithIO(ctx, args, stdout, stderr); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// ExecuteCommand is a package-level convenience function that executes a command with all parameters at once.
func ExecuteCommand(ctx context.Context, host string, cmdStr string, opts ...CommandClientOption) ([]byte, error) {
	client, err := NewCommandClient(host, opts...)
	if err != nil {
		return nil, err
	}
	return client.Execute(ctx, cmdStr)
}

// shellOperators lists shell operators not supported by the SDK.
// Detection runs on the unquoted portion of the command string, so operators
// inside "..." (e.g. MQL `>= / <= / >` in a --mql value) are not flagged.
var shellOperators = []struct {
	op   string
	hint string
}{
	{"||", "SDK does not support shell conditional execution (||), please split into separate calls"},
	{"&&", "SDK does not support shell conditional execution (&&), please split into separate calls"},
	{">>", "SDK does not support output redirection (>>), please use the returned []byte directly"},
	{"|", "SDK does not support pipes (|), please process the returned []byte directly"},
	{">", "SDK does not support output redirection (>), please use the returned []byte directly"},
	{";", "SDK does not support command chaining (;), please split into separate calls"},
}

// rejectShellOperators checks whether the command string contains shell
// operators outside of double-quoted segments.
func rejectShellOperators(cmdStr string) error {
	unquoted := stripQuoted(cmdStr)
	for _, op := range shellOperators {
		if strings.Contains(unquoted, op.op) {
			return fmt.Errorf("%s", op.hint)
		}
	}
	return nil
}

// stripQuoted returns cmdStr with characters inside "..." replaced by spaces,
// so operator detection only inspects unquoted regions. Double quotes group
// values, and backslashes keep the following byte out of operator detection.
func stripQuoted(cmdStr string) string {
	b := make([]byte, len(cmdStr))
	inQuotes := false
	escapeNext := false
	for i := 0; i < len(cmdStr); i++ {
		ch := cmdStr[i]
		switch {
		case escapeNext:
			b[i] = ' '
			escapeNext = false
		case ch == '\\':
			b[i] = ' '
			escapeNext = true
		case ch == '"':
			b[i] = ' '
			inQuotes = !inQuotes
		case inQuotes:
			b[i] = ' '
		default:
			b[i] = ch
		}
	}
	return string(b)
}

// parseArgs parses a command string into an args slice, stripping the binary prefix.
func parseArgs(app *cliapp.App, cmdStr string) []string {
	cmdStr = strings.TrimSpace(cmdStr)

	// Strip binary prefix
	prefixes := []string{"meegle"}
	if app != nil && app.RootCommand() != nil {
		use := strings.TrimSpace(app.RootCommand().Use)
		if use != "" {
			name := strings.Fields(use)[0]
			found := false
			for _, p := range prefixes {
				if p == name {
					found = true
					break
				}
			}
			if !found {
				prefixes = append([]string{name}, prefixes...)
			}
		}
	}
	for _, prefix := range prefixes {
		if cmdStr == prefix {
			return nil
		}
		if strings.HasPrefix(cmdStr, prefix+" ") {
			cmdStr = strings.TrimPrefix(cmdStr, prefix+" ")
			break
		}
	}

	return cliapp.ParseCommandStringArgs(cmdStr)
}
