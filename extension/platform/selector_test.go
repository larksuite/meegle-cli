// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"errors"
	"strings"
	"testing"
)

type selectorCommand string

func (c selectorCommand) Path() string                   { return string(c) }
func (selectorCommand) Domain() string                   { return "" }
func (selectorCommand) Risk() (Risk, bool)               { return "", false }
func (selectorCommand) Identities() []Identity           { return nil }
func (selectorCommand) Annotation(string) (string, bool) { return "", false }

func TestByCommandPath_SupportsGlobStarAtAnySegment(t *testing.T) {
	selector := ByCommandPath("workitem/**/get")
	for _, command := range []selectorCommand{"workitem/get", "workitem/nested/get", "workitem/a/b/get"} {
		if !selector(command) {
			t.Errorf("pattern did not match %q", command)
		}
	}
	if selector(selectorCommand("workitem/a/create")) {
		t.Fatal("pattern matched unrelated command")
	}
}

func TestAbortError_HasStableSecretFreePayloadAndPreservesCause(t *testing.T) {
	cause := errors.New("backend rejected secret-abort-token")
	err := &AbortError{HookName: "change-ticket", Reason: "write requires approval", Cause: cause}
	if strings.Contains(err.Error(), "secret-abort-token") || !strings.Contains(err.Error(), "write requires approval") {
		t.Fatalf("AbortError public message = %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("AbortError lost internal cause")
	}
	if code := err.ErrorPayload()["code"]; code != "CLIENT_EXTENSION_ABORTED" {
		t.Fatalf("AbortError code = %#v", code)
	}
}

func TestAbortError_NilReceiverIsSafe(t *testing.T) {
	var err *AbortError
	if got := err.Error(); got != "CLI extension aborted" {
		t.Fatalf("nil AbortError message = %q", got)
	}
	if err.Unwrap() != nil || err.ErrorPayload() != nil {
		t.Fatal("nil AbortError exposed state")
	}
}
