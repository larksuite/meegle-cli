// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import (
	"encoding/json"
	"strings"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

// MeegleError is the base error type for all CLI errors.
type MeegleError struct {
	Code       string `json:"error"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
	LogID      string `json:"-"`
	ExitCode   int    `json:"-"`
	HTTPStatus int    `json:"-"`
	Cause      error  `json:"-"`
	// Retryable overrides the legacy exit-category default when a caller has
	// an explicit retry decision. MCP tool failures use the backend's advice,
	// defaulting to false when that advice is unavailable or malformed.
	Retryable *bool `json:"-"`
}

// Unwrap preserves the original extension/provider error for errors.Is/As
// while the public payload remains the stable Meegle error contract.
func (e *MeegleError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.Cause)
}

func (e *MeegleError) Error() string {
	return e.Message
}

// MarshalJSON implements custom JSON serialization to omit empty suggestion.
func (e *MeegleError) MarshalJSON() ([]byte, error) {
	m := map[string]string{
		"error":   e.Code,
		"message": e.Message,
	}
	if e.Suggestion != "" {
		m["suggestion"] = e.Suggestion
	}
	return json.Marshal(m)
}

// IsRetryable reports the explicit retry decision, preserving the legacy
// exit-category default for callers that do not supply one.
func (e *MeegleError) IsRetryable() bool {
	if e == nil {
		return false
	}
	if e.Retryable != nil {
		return *e.Retryable
	}
	return e.ExitCode == 2
}

// WithRetryable sets retry advice independently of the error code and exit code.
func (e *MeegleError) WithRetryable(retryable bool) *MeegleError {
	e.Retryable = &retryable
	return e
}

// ErrorPayload supplies the stable error record and optional suggestion to
// the unified format layer.
func (e *MeegleError) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	rec := map[string]any{
		"code":      strings.TrimSpace(e.Code),
		"message":   strings.TrimSpace(e.Message),
		"retryable": e.IsRetryable(),
	}
	if hint := strings.TrimSpace(e.Suggestion); hint != "" {
		rec["suggestion"] = hint
	}
	return rec
}

// ErrorMetadata lets the shared output layer surface transport diagnostics in
// envelope.meta without changing the stable error record. LogID is deliberately
// metadata rather than part of Message/Suggestion so agents can consume it
// reliably and normal human-readable output stays concise.
func (e *MeegleError) ErrorMetadata() map[string]any {
	if e == nil || strings.TrimSpace(e.LogID) == "" {
		return nil
	}
	return map[string]any{"logid": strings.TrimSpace(e.LogID)}
}

// WithSuggestion returns the error with a suggestion set.
func (e *MeegleError) WithSuggestion(s string) *MeegleError {
	e.Suggestion = s
	return e
}

// WithHTTPStatus returns the error with an HTTP status code set.
func (e *MeegleError) WithHTTPStatus(status int) *MeegleError {
	e.HTTPStatus = status
	return e
}

// WithLogID attaches the backend request trace identifier to this error.
func (e *MeegleError) WithLogID(logID string) *MeegleError {
	e.LogID = strings.TrimSpace(logID)
	return e
}

// WithCause attaches an internal cause without serializing it.
func (e *MeegleError) WithCause(cause error) *MeegleError {
	if e != nil {
		e.Cause = cause
	}
	return e
}

// BackendLogID returns the structured backend trace id carried by err or any
// wrapped MeegleError. It never parses free-form error text.
func BackendLogID(err error) string {
	var me *MeegleError
	if !frameworkerrors.SafeAs(err, &me) || me == nil {
		return ""
	}
	return strings.TrimSpace(me.LogID)
}

// NewClientError creates a client-side error (exit code 1).
func NewClientError(code, message string) *MeegleError {
	return &MeegleError{Code: code, Message: message, ExitCode: 1}
}

// NewServerError creates a server-side error (exit code 2).
func NewServerError(code, message string) *MeegleError {
	return &MeegleError{Code: code, Message: message, ExitCode: 2}
}

// IsUnauthorized reports whether err represents a terminal authentication
// rejection from the Meegle server. mcpclient may surface this either as the
// original 401 response or as AUTH_EXPIRED after a store-token refresh fails.
func IsUnauthorized(err error) bool {
	var me *MeegleError
	if !frameworkerrors.SafeAs(err, &me) {
		return false
	}
	if me.HTTPStatus == 401 {
		return true
	}
	return me.Code == "AUTH_EXPIRED"
}

// FormatText renders an error for human-readable output. When the error is
// (or wraps) a MeegleError carrying a Suggestion, the suggestion is appended
// on subsequent lines so users see actionable next steps. For all other
// errors it falls back to err.Error().
func FormatText(err error) string {
	if err == nil {
		return ""
	}
	var me *MeegleError
	if !frameworkerrors.SafeAs(err, &me) {
		return frameworkerrors.SafeMessage(err, "unexpected error")
	}
	msg := strings.TrimSpace(me.Message)
	hint := strings.TrimSpace(me.Suggestion)
	if hint == "" {
		return msg
	}
	return msg + "\n\n" + hint
}
