// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package encoders

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeJSON_Map(t *testing.T) {
	data := map[string]any{"id": 1, "name": "foo"}
	out, err := EncodeJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(string(out), `"id"`) || !strings.Contains(string(out), `"name"`) {
		t.Fatalf("got: %s", out)
	}
	if !strings.Contains(string(out), "  \"id\"") && !strings.Contains(string(out), "  \"name\"") {
		t.Fatalf("expected indent: %s", out)
	}
}

func TestEncodeJSON_RawMessagePassThrough(t *testing.T) {
	raw := json.RawMessage(`{"a":1}`)
	out, err := EncodeJSON(raw)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(string(out), `"a": 1`) {
		t.Fatalf("got: %s", out)
	}
}

func TestEncodeJSON_Nil(t *testing.T) {
	out, err := EncodeJSON(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.TrimSpace(string(out)) != "null" {
		t.Fatalf("got: %q", out)
	}
}

func TestEncodeJSON_EmptyArray(t *testing.T) {
	out, err := EncodeJSON([]any{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("got: %q", out)
	}
}
