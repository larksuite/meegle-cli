// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport

import (
	"errors"
	"strings"
	"testing"
)

func TestAbortError_IsNilSafeAndDoesNotExposeReason(t *testing.T) {
	var nilError *AbortError
	if got := nilError.Error(); got == "" {
		t.Fatal("nil AbortError returned an empty message")
	}
	if nilError.Unwrap() != nil || nilError.ErrorPayload() != nil {
		t.Fatal("nil AbortError accessors must return nil")
	}

	secret := errors.New("secret-transport-token")
	err := &AbortError{Extension: "corp-network", Reason: secret}
	if strings.Contains(err.Error(), secret.Error()) {
		t.Fatalf("AbortError leaked reason: %v", err)
	}
	if !errors.Is(err, ErrAborted) || !errors.Is(err, secret) {
		t.Fatalf("AbortError chain does not preserve sentinel and cause: %v", err)
	}
	payload := err.ErrorPayload()
	if payload["code"] != "CLIENT_EXTENSION_ABORTED" || strings.Contains(payload["message"].(string), secret.Error()) {
		t.Fatalf("AbortError payload = %#v", payload)
	}
}
