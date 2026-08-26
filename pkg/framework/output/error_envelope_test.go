// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"errors"
	"strings"
	"testing"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

func TestBuildErrorRecord_CLIError(t *testing.T) {
	err := frameworkerrors.New(frameworkerrors.CategoryServer, "BACKEND_5XX", "upstream 503")
	err.Detail = "retry after 5s"
	rec := BuildErrorRecord(err)
	if rec["code"] != "BACKEND_5XX" {
		t.Fatalf("code=%v", rec["code"])
	}
	if rec["message"] != "upstream 503" {
		t.Fatalf("message=%v", rec["message"])
	}
	if rec["retryable"] != true {
		t.Fatalf("retryable=%v want true (CategoryServer)", rec["retryable"])
	}
	if rec["detail"] != "retry after 5s" {
		t.Fatalf("detail=%v", rec["detail"])
	}
}

func TestBuildErrorRecord_PlainError(t *testing.T) {
	rec := BuildErrorRecord(errors.New("raw boom"))
	if rec["code"] != "UNKNOWN" {
		t.Fatalf("code=%v", rec["code"])
	}
	if rec["message"] != "raw boom" {
		t.Fatalf("message=%v", rec["message"])
	}
	if rec["retryable"] != false {
		t.Fatalf("retryable=%v want false", rec["retryable"])
	}
}

func TestBuildErrorRecord_NilReturnsNil(t *testing.T) {
	if rec := BuildErrorRecord(nil); rec != nil {
		t.Fatalf("expected nil, got %v", rec)
	}
}

type stubPayloadErr struct {
	msg string
	rec map[string]any
}

func (e *stubPayloadErr) Error() string                { return e.msg }
func (e *stubPayloadErr) ErrorPayload() map[string]any { return e.rec }

func TestBuildErrorRecord_PrefersPayloadBuilder(t *testing.T) {
	custom := map[string]any{
		"code":       "CUSTOM",
		"message":    "from builder",
		"retryable":  true,
		"suggestion": "try --profile prod",
	}
	err := &stubPayloadErr{msg: "ignored", rec: custom}
	rec := BuildErrorRecord(err)
	if rec["code"] != "CUSTOM" || rec["suggestion"] != "try --profile prod" {
		t.Fatalf("builder output not used: %v", rec)
	}
}

type wrappedErr struct {
	inner error
	extra string
}

func (w *wrappedErr) Error() string { return w.extra + ": " + w.inner.Error() }
func (w *wrappedErr) Unwrap() error { return w.inner }

func TestBuildErrorRecord_WalksUnwrapChain(t *testing.T) {
	custom := map[string]any{"code": "INNER", "message": "inner", "retryable": false}
	inner := &stubPayloadErr{msg: "ignored", rec: custom}
	err := &wrappedErr{inner: inner, extra: "outer"}
	rec := BuildErrorRecord(err)
	if rec["code"] != "INNER" {
		t.Fatalf("chain walk failed: %v", rec)
	}
}

type panickingTraversalOutputError struct{ secret string }

func (e *panickingTraversalOutputError) Error() string { return "safe public message" }
func (e *panickingTraversalOutputError) Is(error) bool { panic(e.secret) }
func (e *panickingTraversalOutputError) As(any) bool   { panic(e.secret) }
func (e *panickingTraversalOutputError) Unwrap() error { panic(e.secret) }

func TestBuildErrorRecord_PanickingTraversalFallsBackWithoutPanic(t *testing.T) {
	err := &panickingTraversalOutputError{secret: "secret-output-traversal"}

	var rec map[string]any
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("BuildErrorRecord propagated untrusted traversal panic: %v", recovered)
			}
		}()
		rec = BuildErrorRecord(err)
	}()

	if rec["code"] != "UNKNOWN" || rec["message"] != "safe public message" {
		t.Fatalf("unexpected fallback record: %v", rec)
	}
}

func TestBuildErrorEnvelope_FullShape(t *testing.T) {
	err := frameworkerrors.New(frameworkerrors.CategoryUser, "BAD", "nope")
	env := BuildErrorEnvelope(err, map[string]any{"tool": "t", "duration_ms": 42})
	if env["data"] != nil {
		t.Fatalf("data must be nil: %v", env["data"])
	}
	meta, ok := env["meta"].(map[string]any)
	if !ok || meta["tool"] != "t" || meta["duration_ms"] != 42 {
		t.Fatalf("meta lost: %v", env["meta"])
	}
	record, ok := env["error"].(map[string]any)
	if !ok || record["code"] != "BAD" || record["retryable"] != false {
		t.Fatalf("error record shape: %v", env["error"])
	}
}

func TestBuildErrorEnvelope_NilMetaBecomesEmptyMap(t *testing.T) {
	env := BuildErrorEnvelope(errors.New("x"), nil)
	meta, ok := env["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta should be map, got %T", env["meta"])
	}
	if len(meta) != 0 {
		t.Fatalf("meta should be empty, got %v", meta)
	}
}

type captureFormatter struct {
	lastData any
	lastOpts *FormatOptions
	out      []byte
	err      error
}

func (c *captureFormatter) Format(data any, opts *FormatOptions) ([]byte, error) {
	c.lastData = data
	c.lastOpts = opts
	return c.out, c.err
}

func TestProcessorRenderError_SetsErrorEnvelopeFlag(t *testing.T) {
	capF := &captureFormatter{out: []byte(`{"ok":true}`)}
	p := &Processor{Formatter: capF, Options: &FormatOptions{Mode: "json"}}
	err := frameworkerrors.New(frameworkerrors.CategoryUser, "BAD", "nope")
	payload, renderErr := p.RenderError(nil, err)
	if renderErr != nil {
		t.Fatalf("RenderError: %v", renderErr)
	}
	if string(payload) != `{"ok":true}` {
		t.Fatalf("payload passthrough: %s", payload)
	}
	if capF.lastOpts == nil || !capF.lastOpts.ErrorEnvelope {
		t.Fatalf("ErrorEnvelope flag not propagated: %+v", capF.lastOpts)
	}
	if capF.lastOpts.Mode != "json" {
		t.Fatalf("mode lost: %q", capF.lastOpts.Mode)
	}
}

func TestProcessorRenderError_NilErrorReturnsNil(t *testing.T) {
	p := &Processor{Formatter: &captureFormatter{}}
	payload, err := p.RenderError(nil, nil)
	if err != nil || payload != nil {
		t.Fatalf("nil err should produce nil: payload=%v err=%v", payload, err)
	}
}

func TestProcessorRenderError_FallsBackToJSONMarshal(t *testing.T) {
	p := &Processor{Formatter: nil, Options: &FormatOptions{Mode: "json"}}
	err := frameworkerrors.New(frameworkerrors.CategoryServer, "BOOM", "explode")
	payload, renderErr := p.RenderError(nil, err)
	if renderErr != nil {
		t.Fatalf("RenderError: %v", renderErr)
	}
	got := string(payload)
	for _, fragment := range []string{`"data":null`, `"error"`, `"code":"BOOM"`, `"retryable":true`} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("missing %q in %q", fragment, got)
		}
	}
}

func TestProcessorRenderError_CarriesResultMetadataToEnvelope(t *testing.T) {
	capF := &captureFormatter{out: []byte(`ignored`)}
	p := &Processor{Formatter: capF, Options: &FormatOptions{Mode: "json"}}
	state := &Context{Result: &executor.RawResult{Metadata: map[string]any{"tool": "list_items"}}}
	_, _ = p.RenderError(state, errors.New("x"))
	env, ok := capF.lastData.(map[string]any)
	if !ok {
		t.Fatalf("expected envelope map, got %T", capF.lastData)
	}
	meta, ok := env["meta"].(map[string]any)
	if !ok || meta["tool"] != "list_items" {
		t.Fatalf("meta not propagated: %v", env["meta"])
	}
}

type stubMetadataErr struct{}

func (stubMetadataErr) Error() string { return "backend failed" }
func (stubMetadataErr) ErrorMetadata() map[string]any {
	return map[string]any{"logid": "trace-123"}
}

func TestBuildErrorEnvelopeCarriesErrorMetadata(t *testing.T) {
	env := BuildErrorEnvelope(stubMetadataErr{}, nil)
	meta, ok := env["meta"].(map[string]any)
	if !ok || meta["logid"] != "trace-123" {
		t.Fatalf("error metadata not propagated: %v", env["meta"])
	}
}

type panickingMetadataErr struct{}

func (panickingMetadataErr) Error() string { return "backend failed" }
func (panickingMetadataErr) ErrorMetadata() map[string]any {
	panic("secret metadata panic")
}

func TestBuildErrorEnvelopeHandlesPanickingMetadataBuilder(t *testing.T) {
	env := BuildErrorEnvelope(panickingMetadataErr{}, nil)
	meta, ok := env["meta"].(map[string]any)
	if !ok || len(meta) != 0 {
		t.Fatalf("metadata panic should produce empty metadata: %v", env["meta"])
	}
}

func TestProcessorRenderError_DerivesOptionsFromContext(t *testing.T) {
	capF := &captureFormatter{}
	p := &Processor{Formatter: capF}
	state := &Context{Parsed: &frameworkrouter.ParsedCommand{
		Flags: map[string]any{"format": "ndjson"},
	}}
	_, err := p.RenderError(state, frameworkerrors.New(frameworkerrors.CategoryUser, "X", "x"))
	if err != nil {
		t.Fatalf("RenderError: %v", err)
	}
	if capF.lastOpts == nil || capF.lastOpts.Mode != "ndjson" {
		t.Fatalf("mode from context lost: %+v", capF.lastOpts)
	}
}
