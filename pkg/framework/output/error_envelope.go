// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"strings"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

// ErrorPayloadBuilder lets domain-specific error types (e.g. product-level
// MeegleError) plug their own {code, message, retryable, ...} shape into the
// unified envelope. When the implementation returns a non-nil map, its
// contents replace the default record built from CLIError fields; the caller
// MUST include at least "code", "message", and "retryable" keys so agents
// parsing the envelope can rely on the documented contract.
type ErrorPayloadBuilder interface {
	ErrorPayload() map[string]any
}

// BuildErrorRecord returns the {code, message, retryable, ...} triple for err,
// suitable for embedding under the "error" key of the envelope. Resolution
// order:
//  1. If err (or any error in its chain) implements ErrorPayloadBuilder, use
//     its payload verbatim.
//  2. Else if err is a *frameworkerrors.CLIError, derive code/message from its
//     fields and retryable from IsRetryable().
//  3. Fallback for plain errors: code="UNKNOWN", message=err.Error(),
//     retryable=false.
func BuildErrorRecord(err error) map[string]any {
	if err == nil {
		return nil
	}
	if builder := asPayloadBuilder(err); builder != nil {
		if rec := builder.ErrorPayload(); rec != nil {
			return rec
		}
	}
	rec := map[string]any{
		"code":      "UNKNOWN",
		"message":   strings.TrimSpace(err.Error()),
		"retryable": false,
	}
	if cliErr := frameworkerrors.As(err); cliErr != nil {
		if cliErr.Code != "" {
			rec["code"] = cliErr.Code
		}
		if msg := strings.TrimSpace(cliErr.Message); msg != "" {
			rec["message"] = msg
		}
		rec["retryable"] = cliErr.IsRetryable()
		if detail := strings.TrimSpace(cliErr.Detail); detail != "" {
			rec["detail"] = detail
		}
	}
	return rec
}

// BuildErrorEnvelope returns the full {data:null, meta, error} envelope for
// err. Used by json/ndjson error rendering; table mode uses BuildErrorRecord
// directly and renders the inner record as a KV table.
func BuildErrorEnvelope(err error, meta map[string]any) map[string]any {
	if meta == nil {
		meta = map[string]any{}
	}
	return map[string]any{
		"data":  nil,
		"meta":  meta,
		"error": BuildErrorRecord(err),
	}
}

// asPayloadBuilder unwraps err looking for the first implementation of
// ErrorPayloadBuilder in the error chain. Using errors.As-style walk via the
// Unwrap() chain keeps product-level MeegleError reachable even when wrapped
// by a framework CLIError.
func asPayloadBuilder(err error) ErrorPayloadBuilder {
	for err != nil {
		if b, ok := err.(ErrorPayloadBuilder); ok {
			return b
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = u.Unwrap()
	}
	return nil
}
