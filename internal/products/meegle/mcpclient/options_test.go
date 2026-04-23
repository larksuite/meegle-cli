// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mcpclient

import (
	"strings"
	"testing"
)

// BuildUserAgent must produce DefaultUserAgent() unchanged when the caller is
// empty, and "default caller" (space-separated, RFC 7231) when non-empty.
// This is the single source of truth for the UA format — both SDK and CLI
// code paths delegate here.
func TestBuildUserAgent(t *testing.T) {
	def := DefaultUserAgent()
	if def == "" {
		t.Fatal("DefaultUserAgent returned empty string")
	}
	if !strings.HasPrefix(def, "meegle-cli") {
		t.Fatalf("DefaultUserAgent must start with meegle-cli, got %q", def)
	}

	cases := []struct {
		name   string
		caller string
		want   string
	}{
		{"empty caller returns default unchanged", "", def},
		{"non-empty caller is appended with single space", "my-svc/1.0", def + " my-svc/1.0"},
		{"caller with spaces passes through verbatim", "a b c", def + " a b c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildUserAgent(tc.caller)
			if got != tc.want {
				t.Errorf("BuildUserAgent(%q) = %q, want %q", tc.caller, got, tc.want)
			}
		})
	}
}
