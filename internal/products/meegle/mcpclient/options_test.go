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

// SetVersion must take precedence over debug.ReadBuildInfo() so the UA
// reports the semantic version injected via ldflags rather than Go's
// pseudo-version (which leaks "+dirty" suffixes in CI builds).
func TestSetVersionOverridesBuildInfo(t *testing.T) {
	saved := injectedVersion
	t.Cleanup(func() { injectedVersion = saved })

	injectedVersion = ""
	SetVersion("1.0.1")
	if got := DefaultUserAgent(); got != "meegle-cli/1.0.1" {
		t.Errorf("after SetVersion(1.0.1): got %q, want %q", got, "meegle-cli/1.0.1")
	}

	// BuildUserAgent must compose on top of the injected version.
	if got := BuildUserAgent("mira"); got != "meegle-cli/1.0.1 mira" {
		t.Errorf("BuildUserAgent with injected version: got %q, want %q", got, "meegle-cli/1.0.1 mira")
	}
}

// SetVersion must ignore empty / "dev" inputs so local `go build` invocations
// (which leave `version = "dev"` unchanged) keep the debug.ReadBuildInfo()
// fallback instead of reporting "meegle-cli/dev".
func TestSetVersionIgnoresPlaceholders(t *testing.T) {
	saved := injectedVersion
	t.Cleanup(func() { injectedVersion = saved })

	for _, v := range []string{"", "dev"} {
		injectedVersion = ""
		SetVersion(v)
		if injectedVersion != "" {
			t.Errorf("SetVersion(%q) set injectedVersion to %q, want empty", v, injectedVersion)
		}
	}
}
