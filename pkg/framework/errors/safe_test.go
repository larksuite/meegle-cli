// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

type panickingTraversalError struct{ secret string }

func (e *panickingTraversalError) Error() string { return e.secret }
func (e *panickingTraversalError) Is(error) bool { panic(e.secret) }
func (e *panickingTraversalError) As(any) bool   { panic(e.secret) }
func (e *panickingTraversalError) Unwrap() error { panic(e.secret) }

func TestGuardCause_ContainsPanickingTraversalAndPreservesSafeMatches(t *testing.T) {
	malicious := &panickingTraversalError{secret: "secret-error-traversal"}
	guarded := GuardCause(malicious)

	if SafeIs(malicious, stderrors.New("target")) {
		t.Fatal("SafeIs matched panicking error")
	}
	var target interface{ Marker() }
	if SafeAs(malicious, &target) {
		t.Fatal("SafeAs matched panicking error")
	}
	if stderrors.Is(guarded, stderrors.New("target")) || stderrors.As(guarded, &target) {
		t.Fatal("guarded malicious cause unexpectedly matched")
	}

	sentinel := stderrors.New("sentinel")
	guarded = GuardCause(fmt.Errorf("wrapped: %w", sentinel))
	if !stderrors.Is(guarded, sentinel) {
		t.Fatal("GuardCause lost safe errors.Is match")
	}
}

type panickingMessageError struct{ secret string }

func (e *panickingMessageError) Error() string { panic(e.secret) }

func TestSafeMessage_ContainsPanickingErrorMethod(t *testing.T) {
	if got := SafeMessage(&panickingMessageError{secret: "secret-error-message"}, "safe fallback"); got != "safe fallback" {
		t.Fatalf("SafeMessage() = %q, want fallback", got)
	}
}
