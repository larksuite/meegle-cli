// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import (
	stderrs "errors"
	"strings"
	"testing"
)

func TestCLIErrorExitCode(t *testing.T) {
	err := New(CategoryAuth, CodeInternal, "auth failed")
	if got, want := err.ExitCode(), 3; got != want {
		fatalf(t, "exit code = %d, want %d", got, want)
	}
}

func TestExitCodeFallback(t *testing.T) {
	if got, want := ExitCode(stderrs.New("boom")), 1; got != want {
		fatalf(t, "exit code = %d, want %d", got, want)
	}
}

func TestRenderVerbose(t *testing.T) {
	err := New(CategoryUser, CodeParamInvalid, "invalid value")
	err.Detail = "expected one of: a, b"
	err.Cause = stderrs.New("bad enum")
	rendered := Render(err, true)
	for _, fragment := range []string{"invalid value", "code: PARAM_INVALID", "detail: expected one of: a, b", "cause: bad enum"} {
		if !strings.Contains(rendered, fragment) {
			fatalf(t, "expected %q in %q", fragment, rendered)
		}
	}
}

func TestRenderNonVerbose(t *testing.T) {
	err := New(CategoryUser, CodeParamInvalid, "invalid value")
	err.Detail = "expected one of: a, b"
	if got, want := Render(err, false), "invalid value"; got != want {
		fatalf(t, "render = %q, want %q", got, want)
	}
}

func TestIsRetryableDerivesFromCategory(t *testing.T) {
	cases := []struct {
		name string
		cat  Category
		want bool
	}{
		{"user", CategoryUser, false},
		{"server", CategoryServer, true},
		{"auth", CategoryAuth, false},
		{"config", CategoryConfig, false},
		{"internal", CategoryInternal, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New(tc.cat, CodeInternal, "boom")
			if got := err.IsRetryable(); got != tc.want {
				fatalf(t, "IsRetryable()=%v want %v", got, tc.want)
			}
		})
	}
	if (*CLIError)(nil).IsRetryable() {
		fatalf(t, "nil receiver must report not retryable")
	}
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
