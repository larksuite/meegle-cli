// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/executor"
)

func TestEnvelopeHook_PassThroughWithoutFlag(t *testing.T) {
	h := EnvelopeHook{}
	ctx := &Context{
		Result:  &executor.RawResult{Data: map[string]any{"id": 1}},
		Options: &FormatOptions{},
	}
	out, err := h.Process(ctx, ctx.Result.Data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected pass-through map, got %T", out)
	}
	if m["id"] != 1 {
		t.Fatalf("want pass-through, got %v", out)
	}
}

func TestEnvelopeHook_WrapsWhenFlagSet(t *testing.T) {
	h := EnvelopeHook{}
	ctx := &Context{
		Result: &executor.RawResult{
			Data:     map[string]any{"id": 1},
			Metadata: map[string]any{"tool": "t", "duration_ms": int64(12)},
		},
		Options: &FormatOptions{Envelope: true},
	}
	out, err := h.Process(ctx, ctx.Result.Data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	env, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("not a map: %T", out)
	}
	if env["data"].(map[string]any)["id"] != 1 {
		t.Fatalf("data wrong: %v", env)
	}
	if env["error"] != nil {
		t.Fatalf("error should be nil on success: %v", env)
	}
	meta := env["meta"].(map[string]any)
	if meta["tool"] != "t" || meta["duration_ms"] != int64(12) {
		t.Fatalf("meta incomplete: %v", meta)
	}
}

func TestEnvelopeHook_NilContextSafe(t *testing.T) {
	h := EnvelopeHook{}
	out, err := h.Process(nil, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "x" {
		t.Fatalf("should pass through, got %v", out)
	}
}
