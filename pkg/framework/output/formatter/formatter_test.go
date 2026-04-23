// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatter

import (
	"strings"
	"testing"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

func TestStandard_JSONDefault(t *testing.T) {
	out, err := Standard{}.Format(map[string]any{"a": 1}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(string(out), `"a"`) {
		t.Fatalf("got: %s", out)
	}
}

func TestStandard_JSONExplicit(t *testing.T) {
	out, err := Standard{}.Format(map[string]any{"a": 1}, &frameworkoutput.FormatOptions{Mode: "json"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("empty json output")
	}
}

func TestStandard_NDJSON(t *testing.T) {
	out, err := Standard{}.Format(
		[]any{map[string]any{"a": 1}, map[string]any{"a": 2}},
		&frameworkoutput.FormatOptions{Mode: "ndjson"},
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
}

func TestStandard_TableRejected(t *testing.T) {
	_, err := Standard{}.Format(nil, &frameworkoutput.FormatOptions{Mode: "table"})
	if err == nil {
		t.Fatal("expected error for table in framework standard")
	}
}

func TestStandard_UnknownRejected(t *testing.T) {
	_, err := Standard{}.Format(nil, &frameworkoutput.FormatOptions{Mode: "yaml"})
	if err == nil {
		t.Fatal("expected error for unsupported yaml in framework standard")
	}
}
