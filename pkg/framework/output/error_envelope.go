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

// ErrorMetadataBuilder exposes diagnostics that belong in envelope.meta rather
// than in the stable error record, for example a backend request logid.
type ErrorMetadataBuilder interface {
	ErrorMetadata() map[string]any
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
		if rec := safeErrorPayload(builder); rec != nil {
			return rec
		}
	}
	rec := map[string]any{
		"code":      "UNKNOWN",
		"message":   strings.TrimSpace(frameworkerrors.SafeMessage(err, "unexpected error")),
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
	if builder := asMetadataBuilder(err); builder != nil {
		for key, value := range safeErrorMetadata(builder) {
			// Explicit execution metadata wins if both sources provide a key.
			if _, exists := meta[key]; !exists {
				meta[key] = value
			}
		}
	}
	return map[string]any{
		"data":  nil,
		"meta":  meta,
		"error": BuildErrorRecord(err),
	}
}

// asMetadataBuilder walks err looking for the first implementation of
// ErrorMetadataBuilder, using the guarded SafeAs traversal so an untrusted
// As or Unwrap panic cannot escape the formatting boundary.
func asMetadataBuilder(err error) ErrorMetadataBuilder {
	var builder ErrorMetadataBuilder
	if frameworkerrors.SafeAs(err, &builder) {
		return builder
	}
	return nil
}

func safeErrorMetadata(builder ErrorMetadataBuilder) (metadata map[string]any) {
	defer func() {
		if recover() != nil {
			metadata = nil
		}
	}()
	return builder.ErrorMetadata()
}

// asPayloadBuilder safely walks err looking for the first implementation of
// ErrorPayloadBuilder. The guarded errors.As-style traversal keeps a wrapped
// product error reachable without letting an untrusted As or Unwrap panic
// escape the formatting boundary.
func asPayloadBuilder(err error) ErrorPayloadBuilder {
	var builder ErrorPayloadBuilder
	if frameworkerrors.SafeAs(err, &builder) {
		return builder
	}
	return nil
}

func safeErrorPayload(builder ErrorPayloadBuilder) (record map[string]any) {
	defer func() {
		if recover() != nil {
			record = nil
		}
	}()
	return builder.ErrorPayload()
}
