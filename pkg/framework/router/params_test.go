// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package router

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"

	"os"
)

func TestMergeStructuredParamsInlineJSON(t *testing.T) {
	flags := map[string]any{"params": `{"a":1,"b":"x"}`}
	explicit := map[string]any{}
	merged, err := MergeStructuredParams(flags, explicit)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got, want := merged["a"], float64(1); got != want {
		t.Fatalf("a = %v, want %v", got, want)
	}
	if got, want := merged["b"], "x"; got != want {
		t.Fatalf("b = %v, want %v", got, want)
	}
	if _, exists := merged["params"]; exists {
		t.Fatalf("params key should be deleted after merge")
	}
}

func TestMergeStructuredParamsFileSyntaxRelative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"fields":[{"field_key":"name","field_value":"hello"}]}`), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	flags := map[string]any{"params": "@" + path}
	explicit := map[string]any{}
	merged, err := MergeStructuredParams(flags, explicit)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	fields, ok := merged["fields"].([]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("fields = %#v", merged["fields"])
	}
}

func TestMergeStructuredParamsFileSyntaxAbsolute(t *testing.T) {
	dir := t.TempDir()
	path, err := filepath.Abs(filepath.Join(dir, "abs.json"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"k":"v"}`), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	flags := map[string]any{"params": "@" + path}
	explicit := map[string]any{}
	merged, err := MergeStructuredParams(flags, explicit)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got, want := merged["k"], "v"; got != want {
		t.Fatalf("k = %v, want %v", got, want)
	}
}

func TestMergeStructuredParamsFileMissing(t *testing.T) {
	flags := map[string]any{"params": "@/no/such/file/__definitely__missing__.json"}
	explicit := map[string]any{}
	_, err := MergeStructuredParams(flags, explicit)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	var fe *frameworkerrors.CLIError
	if !errors.As(err, &fe) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if fe.Code != frameworkerrors.CodeParamInvalid {
		t.Fatalf("code = %s, want %s", fe.Code, frameworkerrors.CodeParamInvalid)
	}
	if !strings.Contains(fe.Message, "cannot read file") {
		t.Fatalf("message = %q", fe.Message)
	}
}

func TestMergeStructuredParamsEmptyAtPath(t *testing.T) {
	flags := map[string]any{"params": "@"}
	explicit := map[string]any{}
	_, err := MergeStructuredParams(flags, explicit)
	if err == nil {
		t.Fatalf("expected error for bare @")
	}
	var fe *frameworkerrors.CLIError
	if !errors.As(err, &fe) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if fe.Code != frameworkerrors.CodeParamInvalid {
		t.Fatalf("code = %s, want %s", fe.Code, frameworkerrors.CodeParamInvalid)
	}
}

func TestMergeStructuredParamsFileWithInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	flags := map[string]any{"params": "@" + path}
	explicit := map[string]any{}
	_, err := MergeStructuredParams(flags, explicit)
	if err == nil {
		t.Fatalf("expected error for non-JSON file")
	}
	var fe *frameworkerrors.CLIError
	if !errors.As(err, &fe) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if fe.Code != frameworkerrors.CodeInvalidParams {
		t.Fatalf("code = %s, want %s", fe.Code, frameworkerrors.CodeInvalidParams)
	}
}

func TestMergeStructuredParamsFileMergesWithSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	flags := map[string]any{
		"params": "@" + path,
		"set":    []string{"b=2", "c.d=3"},
	}
	explicit := map[string]any{}
	merged, err := MergeStructuredParams(flags, explicit)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := merged["a"]; got != float64(1) {
		t.Fatalf("a = %v", got)
	}
	if got := merged["b"]; got != int64(2) {
		t.Fatalf("b = %v", got)
	}
	nested, ok := merged["c"].(map[string]any)
	if !ok || nested["d"] != int64(3) {
		t.Fatalf("c.d = %#v", merged["c"])
	}
}
