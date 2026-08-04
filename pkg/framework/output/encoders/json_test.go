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

func TestEncodeJSON_URLRemainsCopyable(t *testing.T) {
	const authURL = "https://meego.example.com/b/auth/mcp?channel=meegle-cli&mode=device&usercode=ABCD-1234"

	out, err := EncodeJSON(map[string]any{"verification_uri_complete": authURL})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(string(out), authURL) {
		t.Fatalf("expected literal URL in output, got: %s", out)
	}
	if strings.Contains(string(out), `\u0026`) {
		t.Fatalf("expected ampersands to remain unescaped, got: %s", out)
	}
}

func BenchmarkEncodeJSON_Array1000(b *testing.B) {
	data := make([]any, 1000)
	for i := range data {
		data[i] = map[string]any{
			"id":   i,
			"name": "benchmark-record",
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := EncodeJSON(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(out) == 0 {
			b.Fatal("expected output")
		}
	}
}
