// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/httptransport"
	telemetryevent "github.com/larksuite/meegle-cli/internal/products/meegle/telemetry"
	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	"github.com/larksuite/meegle-cli/pkg/framework/observability"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type captureTelemetryWire struct{ body []byte }

type telemetryOutputError struct{}

func TestTelemetryPaginationFailureUsesCurrentRequestStage(t *testing.T) {
	for _, pagination := range []string{"token", "page-num"} {
		t.Run(pagination, func(t *testing.T) {
			setupTelemetryCommandRuntime(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls > 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				data := map[string]any{"list": []any{"first"}}
				if pagination == "token" {
					data["next_page_token"] = "next"
				} else {
					data["pagination"] = map[string]any{"has_more": true, "total": 2}
				}
				_ = json.NewEncoder(w).Encode(mcpJSONResponse(t, data))
			}))
			defer server.Close()
			tracker, queue := newQueueBackedTelemetryTracker(t, t.TempDir(), "pagination-"+pagination, "workitem get")
			ctx := tracker.Context(context.Background())
			state := &pipeline.PipelineContext{
				Parsed: &router.ParsedCommand{
					FullPath: []string{"workitem", "get"},
					Node:     &registry.CommandNode{HandlerRef: "list_todo", Meta: registry.NodeMeta{Tags: map[string]string{"mcp_param_types": `{"page-num":"integer"}`}}},
					Flags:    map[string]any{"auto-paginate": true},
				},
				OutputConfig: map[string]any{"mcp.host": "ignored.example.com", "mcp.server_url": server.URL, "mcp.token": "test-token", "mcp.headers": map[string]string{}},
			}
			err := (&MCPRuntime{}).Execute(ctx, state)
			if err == nil || calls != 2 {
				t.Fatalf("pagination calls=%d err=%v", calls, err)
			}
			// Verify stage propagation through the reporter/queue boundary.
			recordCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			recorded, recordErr := tracker.reporter.RecordContext(recordCtx, tracker.eventFromResult(err, 1, false))
			cancel()
			if recordErr != nil || !recorded {
				t.Fatalf("record pagination failure: recorded=%v err=%v", recorded, recordErr)
			}
			items, err := queue.List()
			if err != nil || len(items) != 1 || items[0].Event.FailureStage != telemetryevent.StageResponseWait {
				t.Fatalf("pagination events=%#v err=%v", items, err)
			}
		})
	}
}

func (telemetryOutputError) Write(*frameworkoutput.Context, []byte) error {
	return errors.New("output unavailable")
}

func TestProductPipelineFailureStageReachesQueue(t *testing.T) {
	for _, test := range []struct {
		name        string
		query       string
		token       string
		status      int
		outputError bool
		wantStage   string
	}{
		{"validation", "", "test-token", 200, false, telemetryevent.StagePreflight},
		{"session", "test-query", "", 200, false, telemetryevent.StageAuthPrepare},
		{"execution", "test-query", "test-token", 502, false, telemetryevent.StageRequestSend},
		{"output", "test-query", "test-token", 200, true, telemetryevent.StageLocalOutput},
		{"success", "test-query", "test-token", 200, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTelemetryCommandRuntime(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(`{"available":true,"url":"https://meegle.com/link","expires_at":2000000000}`))
			}))
			defer server.Close()
			tracker, queue := newQueueBackedTelemetryTracker(t, t.TempDir(), "stage-"+test.name, "ai-handoff create-link")
			ctx := tracker.Context(context.Background())
			values := map[string]any{}
			if test.query != "" {
				values["query"] = test.query
			}
			state := localPipelineState(t, "ai-handoff create-link", values)
			state.Parsed.ExplicitFlags = state.Parsed.Flags
			state.Input = &frameworkadapter.RawInput{Context: ctx}
			identity := &ResolvedIdentity{Host: server.URL, Token: test.token}
			pipe, err := newPipelineFactory(&DynamicRegistrySetup{}, identity, server.Client())(cliapp.Config{})
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range pipe.Steps {
				if session, ok := step.(*RuntimeSessionStep); ok {
					runtime, resolveErr := session.Resolver.Resolve(state)
					if resolveErr != nil {
						t.Fatal(resolveErr)
					}
					runtime.(*CLIAPIRuntime).Session.isInteractive = func() bool { return false }
				}
				if output, ok := step.(*pipeline.OutputStep); ok {
					output.Processor = &frameworkoutput.Processor{}
					if test.outputError {
						output.Processor.Writer = telemetryOutputError{}
					}
				}
			}
			err = pipe.Execute(ctx, state)
			exitCode := 0
			if test.wantStage != "" {
				if err == nil {
					t.Fatal("expected pipeline failure")
				}
				exitCode = 1
			} else if err != nil {
				t.Fatal(err)
			}
			recordFinalTelemetryForTest(t, tracker, err, exitCode, false)
			items, listErr := queue.List()
			if listErr != nil {
				t.Fatal(listErr)
			}
			if test.wantStage == "" {
				if len(items) != 0 {
					t.Fatal("successful pipeline queued a failure")
				}
			} else if len(items) != 1 || items[0].Event.FailureStage != test.wantStage {
				t.Fatalf("queued events = %#v, want failure stage %s", items, test.wantStage)
			}
		})
	}
}

func (capture *captureTelemetryWire) Do(_ context.Context, request httptransport.Request) (*httptransport.Response, error) {
	capture.body = append([]byte(nil), request.Body...)
	return &httptransport.Response{StatusCode: http.StatusOK, Body: []byte(`{"failed_call_ids":[]}`)}, nil
}

func newQueueBackedTelemetryTracker(t *testing.T, root, callID, command string) (*CommandTelemetry, *telemetryevent.Queue) {
	t.Helper()
	queue := telemetryevent.NewQueue(root, telemetryevent.QueueOptions{})
	settings := telemetryevent.NewSettingsStore(root)
	tracker := &CommandTelemetry{
		callID: callID, started: time.Now(), fallbackCommand: command,
		hostClass: telemetryevent.HostClassMeegle, credentialSource: telemetryevent.CredentialSourceStore,
		trace: observability.NewTrace(),
		reporter: telemetryevent.NewReporter(telemetryevent.ReporterConfig{
			Queue: queue, Settings: settings, Policies: telemetryevent.NewPolicyStore(root),
		}),
		collectionAllowed: true,
		localEnabled:      true,
	}
	return tracker, queue
}

func recordFinalTelemetryForTest(t *testing.T, tracker *CommandTelemetry, err error, exitCode int, panicked bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, recordErr := tracker.reporter.RecordContext(ctx, tracker.eventFromResult(err, exitCode, panicked)); recordErr != nil {
		t.Fatal(recordErr)
	}
}

func TestHandoffResponseLogIDFlowsThroughFinalEventQueueAndWire(t *testing.T) {
	const secretQuery = "never-send-this-query-in-telemetry"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Tt-Logid", "handoff-log-123")
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	root := t.TempDir()
	tracker, queue := newQueueBackedTelemetryTracker(t, root, "handoff-e2e", "ai-handoff create-link")
	ctx := tracker.Context(context.Background())
	state := localPipelineState(t, "ai-handoff create-link", map[string]any{"query": secretQuery})
	state.Input = &frameworkadapter.RawInput{Context: ctx}
	state.OutputConfig = map[string]any{
		"session.host": server.URL, "session.token": "business-token",
		"session.http_client": server.Client(),
	}
	err := (&CLIAPIRuntime{}).Execute(ctx, state)
	if err == nil {
		t.Fatal("expected handoff HTTP failure")
	}
	recordFinalTelemetryForTest(t, tracker, err, 1, false)
	items, listErr := queue.List()
	if listErr != nil || len(items) != 1 {
		t.Fatalf("final queue = %#v, %v", items, listErr)
	}
	event := items[0].Event
	if len(event.BackendRefs) != 1 || event.BackendRefs[0].Service != "meegle_api" ||
		event.BackendRefs[0].Operation != "handoff_create_link" || event.BackendRefs[0].LogID != "handoff-log-123" {
		t.Fatalf("queued handoff refs = %#v", event.BackendRefs)
	}

	wire := &captureTelemetryWire{}
	uploader := telemetryevent.NewUploader(telemetryevent.UploaderConfig{
		Queue: queue, Settings: telemetryevent.NewSettingsStore(root),
		Transport: wire,
	})
	if err := uploader.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire.body), `"logid":"handoff-log-123"`) {
		t.Fatalf("wire body lost handoff logid: %s", wire.body)
	}
	for _, forbidden := range []string{secretQuery, "business-token"} {
		if strings.Contains(string(wire.body), forbidden) {
			t.Fatalf("wire body contains %q: %s", forbidden, wire.body)
		}
	}
}

func TestBackendFailureQueuesStableAllowlistedFingerprint(t *testing.T) {
	root := t.TempDir()
	tracker, queue := newQueueBackedTelemetryTracker(t, root, "backend-fingerprint", "workitem get")
	ctx := tracker.Context(context.Background())
	tracker.SetStage(ctx, telemetryevent.StageResponseWait)
	beforeFinish := time.Now().UnixMilli()
	err := meerrors.NewServerError("SERVER_CALL_FAILED", "first sensitive backend message").WithHTTPStatus(http.StatusBadGateway)
	recordFinalTelemetryForTest(t, tracker, err, 2, false)
	afterFinish := time.Now().UnixMilli()

	items, listErr := queue.List()
	if listErr != nil || len(items) != 1 {
		t.Fatalf("queue = %#v, %v", items, listErr)
	}
	if got, want := items[0].Event.Fingerprint, "ef0594034231070762f2462111c38ff41b1a3e5bc1ec644d386d0a376691eaa8"; got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
	if strings.Contains(items[0].Event.Fingerprint, "sensitive") {
		t.Fatalf("fingerprint leaked raw error text: %q", items[0].Event.Fingerprint)
	}
	event := items[0].Event
	if event.OccurredAtMS < beforeFinish || event.OccurredAtMS > afterFinish || event.ClientVersion == "" || event.OS != runtime.GOOS || event.Arch != runtime.GOARCH {
		t.Fatalf("CLI metadata = %#v", event)
	}
	if !event.Retryable || event.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("error metadata = retryable:%v http_status:%d", event.Retryable, event.HTTPStatus)
	}
}

func TestFailureFingerprintUsesNormalizedWireFields(t *testing.T) {
	root := t.TempDir()
	tracker, queue := newQueueBackedTelemetryTracker(t, root, "normalized-fingerprint", "  WorkItem   GET  ")
	ctx := tracker.Context(context.Background())
	tracker.SetStage(ctx, telemetryevent.StageResponseWait)
	recordFinalTelemetryForTest(t, tracker, meerrors.NewServerError("SERVER_CALL_FAILED", "sensitive backend message"), 1, false)

	items, err := queue.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("queue = %#v, %v", items, err)
	}
	event := items[0].Event
	if got, want := event.Command, "workitem get"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if got, want := event.Fingerprint, "ef0594034231070762f2462111c38ff41b1a3e5bc1ec644d386d0a376691eaa8"; got != want {
		t.Fatalf("fingerprint = %q, want normalized-wire fingerprint %q", got, want)
	}
}

func TestEveryCommandFailureClassQueuesFingerprint(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		panicked   bool
		retryable  bool
		httpStatus int
	}{
		{name: "user", err: meerrors.NewClientError("CLIENT_BAD_INPUT", "sensitive input")},
		{name: "auth", err: meerrors.NewClientError("AUTH_EXPIRED", "sensitive credential state")},
		{name: "transport", err: meerrors.NewClientError("TRANSPORT_TIMEOUT", "sensitive host"), retryable: true},
		{name: "backend", err: meerrors.NewServerError("SERVER_CALL_FAILED", "sensitive response").WithHTTPStatus(http.StatusBadGateway), retryable: true, httpStatus: http.StatusBadGateway},
		{name: "MCP backend", err: meerrors.NewServerError("SERVER_CALL_FAILED", "sensitive response").WithHTTPStatus(http.StatusBadGateway).WithRetryable(false), httpStatus: http.StatusBadGateway},
		{name: "postprocess", err: meerrors.NewClientError("CLIENT_FILE_SIGN_MISMATCH", "sensitive path")},
		{name: "canceled", err: context.Canceled},
		{name: "panic", err: errors.New("sensitive panic"), panicked: true},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			tracker, queue := newQueueBackedTelemetryTracker(t, root, fmt.Sprintf("failure-%d", index), "workitem get")
			ctx := tracker.Context(context.Background())
			tracker.SetStage(ctx, telemetryevent.StageResponseWait)
			recordFinalTelemetryForTest(t, tracker, test.err, 1, test.panicked)
			items, err := queue.List()
			if err != nil || len(items) != 1 {
				t.Fatalf("queue = %#v, %v", items, err)
			}
			if len(items[0].Event.Fingerprint) != 64 {
				t.Fatalf("fingerprint = %q", items[0].Event.Fingerprint)
			}
			if items[0].Event.Retryable != test.retryable || items[0].Event.HTTPStatus != test.httpStatus {
				t.Fatalf("error metadata = retryable:%v http_status:%d", items[0].Event.Retryable, items[0].Event.HTTPStatus)
			}
		})
	}
}

func TestTelemetryExplicitRetryAdvice(t *testing.T) {
	for _, status := range []int{0, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		for _, retryable := range []bool{false, true} {
			err := meerrors.NewServerError("SERVER_CALL_FAILED", "backend failure").
				WithHTTPStatus(status).WithRetryable(retryable)
			for _, outcome := range []string{telemetryevent.OutcomeTransportError, telemetryevent.OutcomeBackendError} {
				got, gotStatus := telemetryErrorMetadata(fmt.Errorf("wrapped: %w", err), outcome)
				if got != retryable || gotStatus != status || got != err.ErrorPayload()["retryable"] {
					t.Fatalf("status=%d outcome=%s: retryable=%v HTTP=%d, want %v/%d", status, outcome, got, gotStatus, retryable, status)
				}
			}
		}
	}
}
