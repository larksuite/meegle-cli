// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

// TestMapDownloadError pins the contract the +download integrity guard exposes
// to users: the attachment package's internal sentinels must surface as the
// documented CLIENT_* codes and carry a retry hint. Unrelated errors must pass
// through untouched. The mismatch case is fed through a wrap to prove errors.Is
// still matches across the multipart error wrapping ExecuteDownload applies.
func TestMapDownloadError(t *testing.T) {
	passthrough := errors.New("some unrelated download failure")

	cases := []struct {
		name     string
		in       error
		wantCode string // "" → expect the original error back, unchanged
	}{
		{
			name:     "mismatch maps to CLIENT_FILE_SIGN_MISMATCH (through wrap)",
			in:       fmt.Errorf("download chunk 1: %w", attachment.ErrFileSignMismatch),
			wantCode: "CLIENT_FILE_SIGN_MISMATCH",
		},
		{
			name:     "missing header maps to CLIENT_FILE_SIGN_UNVERIFIED",
			in:       attachment.ErrFileSignMissing,
			wantCode: "CLIENT_FILE_SIGN_UNVERIFIED",
		},
		{
			name:     "unrelated error passes through unchanged",
			in:       passthrough,
			wantCode: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapDownloadError(c.in)

			if c.wantCode == "" {
				if got != c.in {
					t.Fatalf("passthrough: got %v, want the original error unchanged", got)
				}
				return
			}

			var me *meerrors.MeegleError
			if !errors.As(got, &me) {
				t.Fatalf("want *MeegleError, got %T (%v)", got, got)
			}
			if me.Code != c.wantCode {
				t.Errorf("Code = %q, want %q", me.Code, c.wantCode)
			}
			if !strings.Contains(strings.ToLower(me.Suggestion), "retry") {
				t.Errorf("Suggestion = %q, want it to prompt a retry", me.Suggestion)
			}
		})
	}
}

// TestDownloadGETHeaders pins the safety contract of the download GET header
// forwarding: configured lane/env headers flow through, but auth-bearing
// headers are stripped so the Meegle token never reaches the object-storage
// host.
func TestDownloadGETHeaders(t *testing.T) {
	state := &pipeline.PipelineContext{
		OutputConfig: map[string]any{
			"mcp.headers": map[string]string{
				"x-custom-env":  "lane-1",
				"x-route":       "1",
				"Authorization": "Bearer super-secret",
				"X-Token":       "tok-123",
			},
			"mcp.access_token_header": "X-Token",
		},
	}

	got := downloadGETHeaders(state)

	if got["x-custom-env"] != "lane-1" || got["x-route"] != "1" {
		t.Errorf("custom headers should be forwarded, got %v", got)
	}
	if _, ok := got["Authorization"]; ok {
		t.Error("Authorization must be stripped (token leak)")
	}
	if _, ok := got["X-Token"]; ok {
		t.Error("the configured access-token header must be stripped (token leak)")
	}
}

func TestDownloadGETHeaders_NilWhenEmpty(t *testing.T) {
	if got := downloadGETHeaders(&pipeline.PipelineContext{OutputConfig: map[string]any{}}); got != nil {
		t.Errorf("want nil when no headers configured, got %v", got)
	}
	// Only auth headers configured → nothing forwardable → nil.
	state := &pipeline.PipelineContext{OutputConfig: map[string]any{
		"mcp.headers":             map[string]string{"Authorization": "Bearer x"},
		"mcp.access_token_header": "",
	}}
	if got := downloadGETHeaders(state); got != nil {
		t.Errorf("want nil when only auth headers present, got %v", got)
	}
}
