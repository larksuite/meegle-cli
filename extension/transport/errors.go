// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport

import (
	"errors"
	"fmt"
)

// ErrAborted identifies an extension-triggered round-trip rejection.
var ErrAborted = errors.New("round trip aborted by extension")

// AbortError records which transport extension rejected a request.
type AbortError struct {
	Extension string
	Reason    error
}

func (e *AbortError) Error() string {
	if e == nil {
		return "transport extension aborted round trip"
	}
	if e.Extension == "" {
		return "transport extension aborted round trip"
	}
	return fmt.Sprintf("transport extension %q aborted round trip", e.Extension)
}

func (e *AbortError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Reason
}
func (e *AbortError) Is(target error) bool { return target == ErrAborted }

// ErrorPayload exposes a stable, non-secret rejection contract. Reason is
// deliberately retained only in the Go error chain because extensions may
// accidentally include credentials or request data in it.
func (e *AbortError) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"code":      "CLIENT_EXTENSION_ABORTED",
		"message":   e.Error(),
		"retryable": false,
		"detail": map[string]any{
			"extension": e.Extension,
		},
	}
}
