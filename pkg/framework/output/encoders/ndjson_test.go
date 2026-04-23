// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package encoders

import (
	"strings"
	"testing"
)

func TestEncodeNDJSON_ArrayOfObjects(t *testing.T) {
	data := []any{
		map[string]any{"id": 1},
		map[string]any{"id": 2},
	}
	out, err := EncodeNDJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"id":1`) || !strings.Contains(lines[1], `"id":2`) {
		t.Fatalf("lines: %q", lines)
	}
}

func TestEncodeNDJSON_SingleObject(t *testing.T) {
	data := map[string]any{"id": 1}
	out, err := EncodeNDJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(lines), out)
	}
}

func TestEncodeNDJSON_Primitive(t *testing.T) {
	out, err := EncodeNDJSON("hello")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.TrimSpace(string(out)) != `"hello"` {
		t.Fatalf("got: %q", out)
	}
}

func TestEncodeNDJSON_EmptyArray(t *testing.T) {
	out, err := EncodeNDJSON([]any{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("want empty, got: %q", out)
	}
}

func TestEncodeNDJSON_StringNewlineEscaped(t *testing.T) {
	data := []any{map[string]any{"msg": "line1\nline2"}}
	out, err := EncodeNDJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	trimmed := strings.TrimSuffix(string(out), "\n")
	if strings.Count(trimmed, "\n") != 0 {
		t.Fatalf("unescaped newline: %q", out)
	}
	if !strings.Contains(trimmed, `\n`) {
		t.Fatalf("newline not escaped: %q", out)
	}
}
