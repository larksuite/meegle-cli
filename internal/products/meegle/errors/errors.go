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

// ErrorPayload implements frameworkoutput.ErrorPayloadBuilder so the unified
// format layer picks up MeegleError's suggestion and exit-code-derived
// retryable flag without a separate conversion helper. Retryable is derived
// from ExitCode: 2 (server) → true, anything else → false, matching the
// CLIError category rule. Suggestion is surfaced as an optional field so
// agents still receive the human hint alongside the required triple.
func (e *MeegleError) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	rec := map[string]any{
		"code":      strings.TrimSpace(e.Code),
		"message":   strings.TrimSpace(e.Message),
		"retryable": e.ExitCode == 2,
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
