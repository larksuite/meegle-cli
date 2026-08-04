// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package encoders

import (
	"errors"
	"strings"
	"testing"
)

type failingJSONMarshaler struct{}

func (failingJSONMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("encode failed")
}

func TestEncodeNDJSON_ArrayOfObjects(t *testing.T) {
	data := []any{
		map[string]any{"id": 1},
		map[string]any{"id": 2},
	}
	out, err := EncodeNDJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	const want = "{\"id\":1}\n{\"id\":2}\n"
	if string(out) != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestEncodeNDJSON_SingleObject(t *testing.T) {
	data := map[string]any{"id": 1}
	out, err := EncodeNDJSON(data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	const want = "{\"id\":1}\n"
	if string(out) != want {
		t.Fatalf("want %q, got %q", want, out)
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
	if len(out) != 0 {
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

func TestEncodeNDJSON_URLRemainsCopyable(t *testing.T) {
	const authURL = "https://meego.example.com/b/auth/mcp?channel=meegle-cli&mode=device&usercode=ABCD-1234"

	out, err := EncodeNDJSON(map[string]any{"verification_uri_complete": authURL})
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

func TestEncodeNDJSON_ArrayURLRemainsCopyable(t *testing.T) {
	const authURL = "https://meego.example.com/b/auth/mcp?channel=meegle-cli&mode=device&usercode=ABCD-1234"

	out, err := EncodeNDJSON([]any{map[string]any{"verification_uri_complete": authURL}})
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

func TestEncodeNDJSON_ArrayEncodingErrorReturnsNoOutput(t *testing.T) {
	out, err := EncodeNDJSON([]any{map[string]any{"id": 1}, failingJSONMarshaler{}})
	if err == nil {
		t.Fatal("expected encoding error")
	}
	if out != nil {
		t.Fatalf("expected nil output on error, got: %q", out)
	}
}

func BenchmarkEncodeNDJSON_Array1000(b *testing.B) {
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
		out, err := EncodeNDJSON(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(out) == 0 {
			b.Fatal("expected output")
		}
	}
}
