// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapiclient

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

const (
	configPath = "/goapi/v5/meeglecli/config"
	linksPath  = "/goapi/v5/meeglecli/handoff/link"
	// preferencePath is a generic user-preference endpoint, not handoff-scoped.
	// The preference object is selected by the request body's `type` field
	// (e.g. "handoff_suggestions") and the type-specific value by `payload`, so future preferences
	// (notification, ai-summary, ...) reuse the same route.
	preferencePath = "/goapi/v5/meeglecli/preference"

	// preferenceTypeHandoff selects the AI handoff recommendation preference.
	preferenceTypeHandoff = "handoff_suggestions"

	// idempotencyHeader carries a client-generated key so the server can
	// de-duplicate a create-link request that is retried after a network
	// timeout. Only non-idempotent writes (create-link) send it.
	idempotencyHeader = "Idempotency-Key"

	// facadeInvalidParamCode is the stable errgen code embedded by Facade in
	// invalid-parameter responses. The gateway currently wraps it in outer
	// code 50000, so classification must also inspect the nested error string.
	facadeInvalidParamCode = int64(20006)

	handoffInvalidParamMessage = "handoff request contains invalid parameters"
	handoffQueryLimitMessage   = "query exceeds the current max_query_bytes limit"
	handoffContextLimitMessage = "related context exceeds the current max_related_context_items limit"
	handoffInvalidParamHint    = "run 'meegle ai-handoff availability' to check max_query_bytes and max_related_context_items"
)

const (
	// DefaultTimeout bounds a single attempt (dial + write + response read).
	// The design targets a 5-10s client timeout; 8s sits in that band. It is
	// per-attempt, not total, so it composes with the retry backoff below.
	DefaultTimeout = 8 * time.Second

	// DefaultMaxRetries is the number of retries after the first attempt, so
	// the worst case is 4 attempts. Only retryable transport failures consume
	// this budget; policy, payload, and auth errors fail fast.
	DefaultMaxRetries = 3

	// defaultRetryBackoff is the base delay before the first retry. Each
	// subsequent retry doubles the base (capped) and adds jitter, so parallel
	// callers do not resynchronise into a retry storm.
	defaultRetryBackoff = 200 * time.Millisecond

	// maxRetryBackoff caps a single backoff interval regardless of attempt.
	maxRetryBackoff = 5 * time.Second
)

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      func() (string, error)
	Refresh    func() error
	Headers    http.Header
	AuthHeader string
	UserAgent  string

	// Timeout bounds each individual attempt. Zero or negative uses
	// DefaultTimeout. It is applied through the request context, so it also
	// cancels a slow response-body read, and it must not be duplicated on
	// HTTPClient.Timeout (which would bound the whole client, not one attempt).
	Timeout time.Duration

	// MaxRetries is the number of retries after the first attempt. The zero
	// value uses DefaultMaxRetries; a negative value disables retries. Retries
	// only fire for retryable transport failures.
	MaxRetries int

	// RetryBackoff overrides the base backoff delay. Zero or negative uses
	// defaultRetryBackoff. Mainly a test seam for fast, deterministic runs.
	RetryBackoff time.Duration
}

type Client struct {
	config Config
}

// preferenceRequest is the generic user-preference envelope. `type` selects
// the preference object and `payload` carries type-owned JSON. Keeping the
// payload opaque lets the endpoint add preference types without changing its
// public envelope.
type preferenceItem struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

type preferenceRequest struct {
	Preferences []preferenceItem `json:"preferences"`
}

type handoffPreferencePayload struct {
	Mode string `json:"mode"`
}

// callOptions carries per-call transport metadata that is not part of the
// request body, such as the idempotency key.
type callOptions struct {
	idempotencyKey string
}

func New(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.Headers == nil {
		config.Headers = make(http.Header)
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	return &Client{config: config}
}

func (client *Client) Config(ctx context.Context) (*CLIConfig, error) {
	var response CLIConfig
	if err := client.call(ctx, http.MethodGet, configPath, nil, callOptions{}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Availability is kept as a small compatibility helper for SDK callers. New
// command flows should fetch Config once and select its Handoff section.
func (client *Client) Availability(ctx context.Context) (*Availability, error) {
	var response Availability
	if err := client.call(ctx, http.MethodGet, configPath, nil, callOptions{}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) CreateLink(ctx context.Context, request CreateLinkRequest) (*CreateLinkResponse, error) {
	var response CreateLinkResponse
	// Generate the idempotency key once per logical create-link call and keep
	// it stable across retries so a retried request cannot create a second
	// handoff link.
	opts := callOptions{idempotencyKey: newIdempotencyKey()}
	if err := client.call(ctx, http.MethodPost, linksPath, request, opts, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *Client) UpdatePreference(ctx context.Context, mode string) (*PreferenceUpdateResult, error) {
	// Mode updates are idempotent by value (they set, not toggle), so they
	// need no idempotency key. The handoff handler owns this payload schema.
	payload, err := json.Marshal(handoffPreferencePayload{Mode: mode})
	if err != nil {
		return nil, fmt.Errorf("marshal handoff preference payload: %w", err)
	}
	body := preferenceRequest{Preferences: []preferenceItem{{Type: preferenceTypeHandoff, Payload: string(payload)}}}
	response := &PreferenceUpdateResult{Success: true}
	if err := client.call(ctx, http.MethodPost, preferencePath, body, callOptions{}, response); err != nil {
		return nil, err
	}
	return response, nil
}

// call orchestrates bounded retries around callWithRefresh. Retries fire only
// for retryable transport failures and stop as soon as the parent context is
// cancelled. The idempotency key (when set) is reused across attempts.
func (client *Client) call(ctx context.Context, method, path string, body any, opts callOptions, target any) error {
	maxRetries := client.maxRetries()
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := client.waitBackoff(ctx, attempt); err != nil {
				// Parent context ended during backoff: surface the transport
				// failure that triggered the retry, which is more actionable
				// than the bare context error.
				return lastErr
			}
		}
		lastErr = client.callWithRefresh(ctx, method, path, body, opts, target)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil || !isRetryable(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// callWithRefresh performs one logical request, transparently refreshing the
// token and retrying once on a 401. This 401 retry is independent of the
// transport retry budget in call.
func (client *Client) callWithRefresh(ctx context.Context, method, path string, body any, opts callOptions, target any) error {
	err := client.doCall(ctx, method, path, body, opts, target)
	if !isUnauthorized(err) || client.config.Refresh == nil {
		return err
	}
	if refreshErr := client.config.Refresh(); refreshErr != nil {
		return meerrors.NewClientError("AUTH_EXPIRED", "authentication expired, please log in again").
			WithSuggestion("meegle auth login")
	}
	return client.doCall(ctx, method, path, body, opts, target)
}

func (client *Client) doCall(ctx context.Context, method, path string, body any, opts callOptions, target any) error {
	// Bound this attempt through the context so a stalled dial, write, or
	// response-body read is cancelled. The retry loop's parent-context guard
	// distinguishes an attempt timeout (retryable) from parent cancellation.
	attemptCtx := ctx
	if client.config.Timeout > 0 {
		var cancel context.CancelFunc
		attemptCtx, cancel = context.WithTimeout(ctx, client.config.Timeout)
		defer cancel()
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal handoff request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(attemptCtx, method, strings.TrimRight(client.config.BaseURL, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("create handoff request: %w", err)
	}
	// Apply caller-supplied extra headers first, then Set the managed headers
	// below so they win as single values. Using Add here and Set afterwards
	// mirrors how the auth block (further down) avoids duplicate Authorization
	// values: a managed key that also appears in config.Headers must not end up
	// with two wire values (critical for Idempotency-Key).
	for key, values := range client.config.Headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if opts.idempotencyKey != "" {
		request.Header.Set(idempotencyHeader, opts.idempotencyKey)
	}
	if client.config.UserAgent != "" {
		request.Header.Set("User-Agent", client.config.UserAgent)
	}
	if client.config.Token != nil {
		token, err := client.config.Token()
		if err != nil {
			return fmt.Errorf("get handoff token: %w", err)
		}
		if token != "" {
			if client.config.AuthHeader != "" {
				request.Header.Set(client.config.AuthHeader, token)
				request.Header.Del("Authorization")
			} else {
				request.Header.Set("Authorization", "Bearer "+token)
			}
		}
	}

	response, err := client.config.HTTPClient.Do(request)
	if err != nil {
		return transportError(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return transportError(err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return meerrors.NewServerError("HANDOFF_API_HTTP_ERROR",
			fmt.Sprintf("handoff API returned HTTP %d", response.StatusCode)).
			WithHTTPStatus(response.StatusCode).
			WithLogID(response.Header.Get("X-Tt-Logid"))
	}
	if err := decodeResponse(payload, target); err != nil {
		if isInvalidParamEnvelope(err) {
			return meerrors.NewClientError("HANDOFF_API_INVALID_PARAM", invalidParamUserMessage(err)).
				WithSuggestion(handoffInvalidParamHint).
				WithLogID(response.Header.Get("X-Tt-Logid"))
		}
		return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", err.Error()).
			WithLogID(response.Header.Get("X-Tt-Logid"))
	}
	setResponseLogID(target, response.Header.Get("X-Tt-Logid"))
	return nil
}

func setResponseLogID(target any, logID string) {
	logID = strings.TrimSpace(logID)
	switch response := target.(type) {
	case *CLIConfig:
		response.LogID = logID
		response.HandoffSuggestion.LogID = logID
	case *Availability:
		response.LogID = logID
	case *CreateLinkResponse:
		response.LogID = logID
	case *PreferenceUpdateResult:
		response.LogID = logID
	}
}

// transportError classifies a failure from http.Client.Do or the body read.
// Timeouts and generic network failures are marked retryable; an explicit
// parent-context cancellation is not (the caller gave up).
func transportError(err error) error {
	if stderrors.Is(err, context.Canceled) {
		return meerrors.NewClientError("HANDOFF_API_CANCELLED", "handoff API request cancelled")
	}
	if stderrors.Is(err, context.DeadlineExceeded) {
		return meerrors.NewServerError("HANDOFF_API_TIMEOUT",
			"handoff API request timed out").WithSuggestion("check your network or try again later")
	}
	return meerrors.NewServerError("HANDOFF_API_UNAVAILABLE", "handoff API request failed: "+err.Error())
}

// isRetryable reports whether a failed attempt is worth retrying. Only
// transient transport failures qualify; policy, payload, decode, and auth
// errors are deterministic and must fail fast.
func isRetryable(err error) bool {
	var me *meerrors.MeegleError
	if !stderrors.As(err, &me) {
		return false
	}
	switch me.Code {
	case "HANDOFF_API_TIMEOUT", "HANDOFF_API_UNAVAILABLE":
		return true
	case "HANDOFF_API_HTTP_ERROR":
		return me.HTTPStatus >= http.StatusInternalServerError || me.HTTPStatus == http.StatusTooManyRequests
	default:
		return false
	}
}

// maxRetries resolves the configured retry count: zero uses the default, a
// negative value disables retries.
func (client *Client) maxRetries() int {
	if client.config.MaxRetries < 0 {
		return 0
	}
	if client.config.MaxRetries == 0 {
		return DefaultMaxRetries
	}
	return client.config.MaxRetries
}

// waitBackoff sleeps before retry number `attempt` (1-based) using an
// exponentially growing base with equal jitter, capped at maxRetryBackoff.
// It returns early if the parent context ends.
func (client *Client) waitBackoff(ctx context.Context, attempt int) error {
	base := client.config.RetryBackoff
	if base <= 0 {
		base = defaultRetryBackoff
	}
	backoff := base
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= maxRetryBackoff {
			backoff = maxRetryBackoff
			break
		}
	}
	if backoff > maxRetryBackoff {
		backoff = maxRetryBackoff
	}
	// Equal jitter: keep half the interval fixed and randomise the other half
	// so retries spread out instead of firing in lockstep.
	half := backoff / 2
	wait := half + time.Duration(mrand.Int63n(int64(half)+1))

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// newIdempotencyKey returns a random 128-bit hex key. On the practically
// impossible event that the system RNG fails, it returns an empty string and
// the header is simply omitted rather than sending a weak or fixed key.
func newIdempotencyKey() string {
	buffer := make([]byte, 16)
	if _, err := crand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}

func decodeResponse(payload []byte, target any) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return fmt.Errorf("decode handoff response: %w", err)
	}
	if err := checkEnvelopeError(raw); err != nil {
		return err
	}
	if data, ok := raw["data"]; ok && len(data) > 0 && string(data) != "null" {
		payload = data
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode handoff payload: %w", err)
	}
	return nil
}

type envelopeError struct {
	code    int64
	message string
}

func (err *envelopeError) Error() string {
	if err == nil {
		return ""
	}
	return err.message
}

func isInvalidParamEnvelope(err error) bool {
	var envelopeErr *envelopeError
	if !stderrors.As(err, &envelopeErr) {
		return false
	}
	if envelopeErr.code == facadeInvalidParamCode {
		return true
	}
	return strings.Contains(envelopeErr.message, fmt.Sprintf("code=%d", facadeInvalidParamCode))
}

func invalidParamUserMessage(err error) string {
	var envelopeErr *envelopeError
	if !stderrors.As(err, &envelopeErr) {
		return handoffInvalidParamMessage
	}
	message := strings.ToLower(envelopeErr.message)
	switch {
	case strings.Contains(message, "user_query exceeds"):
		return handoffQueryLimitMessage
	case strings.Contains(message, "related_context exceeds"):
		return handoffContextLimitMessage
	default:
		return handoffInvalidParamMessage
	}
}

func checkEnvelopeError(raw map[string]json.RawMessage) error {
	if codeRaw, ok := raw["code"]; ok {
		code := parseCode(codeRaw)
		if code != 0 {
			message := firstMessage(raw, "msg", "message")
			return &envelopeError{
				code:    code,
				message: fmt.Sprintf("handoff API error %d: %s", code, message),
			}
		}
	}
	for _, key := range []string{"base_resp", "BaseResp"} {
		baseRaw, ok := raw[key]
		if !ok {
			continue
		}
		var base map[string]json.RawMessage
		if json.Unmarshal(baseRaw, &base) != nil {
			continue
		}
		for _, codeKey := range []string{"status_code", "StatusCode"} {
			if code := parseCode(base[codeKey]); code != 0 {
				return &envelopeError{
					code: code,
					message: fmt.Sprintf("handoff API status %d: %s", code,
						firstMessage(base, "status_message", "StatusMessage")),
				}
			}
		}
	}
	return nil
}

func parseCode(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var number int64
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.ParseInt(text, 10, 64)
	}
	return number
}

func firstMessage(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var message string
		if json.Unmarshal(raw[key], &message) == nil && message != "" {
			return message
		}
	}
	return "unknown error"
}

func isUnauthorized(err error) bool {
	var me *meerrors.MeegleError
	if !stderrors.As(err, &me) {
		return false
	}
	return me.HTTPStatus == http.StatusUnauthorized
}
