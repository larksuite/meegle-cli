// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

// fastRetryConfig returns a Config with a tiny backoff so retry tests run
// quickly without changing the retry semantics under test.
func fastRetryConfig(baseURL string) Config {
	return Config{BaseURL: baseURL, RetryBackoff: time.Millisecond}
}

func TestCreateLinkSendsStableIdempotencyKeyAcrossRetries(t *testing.T) {
	var attempts int32
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		keys = append(keys, request.Header.Get(idempotencyHeader))
		if n < 3 {
			writer.WriteHeader(http.StatusBadGateway) // retryable 5xx
			return
		}
		_, _ = writer.Write([]byte(`{"available":true,"url":"https://example.internal/x"}`))
	}))
	defer server.Close()

	client := New(fastRetryConfig(server.URL))
	response, err := client.CreateLink(context.Background(), CreateLinkRequest{UserQuery: "hi"})
	if err != nil {
		t.Fatalf("CreateLink() error = %v", err)
	}
	if !response.Available || response.URL == "" {
		t.Fatalf("response = %#v", response)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if len(keys) != 3 {
		t.Fatalf("captured %d keys, want 3", len(keys))
	}
	if keys[0] == "" {
		t.Fatalf("idempotency key is empty")
	}
	for i, key := range keys {
		if key != keys[0] {
			t.Fatalf("idempotency key changed on attempt %d: %q != %q", i, key, keys[0])
		}
	}
}

func TestGetRequestsCarryNoIdempotencyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if key := request.Header.Get(idempotencyHeader); key != "" {
			t.Fatalf("unexpected idempotency key on GET: %q", key)
		}
		_, _ = writer.Write([]byte(`{"available":true}`))
	}))
	defer server.Close()

	if _, err := New(fastRetryConfig(server.URL)).Availability(context.Background()); err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
}

func TestUpdatePreferenceCarriesNoIdempotencyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if key := request.Header.Get(idempotencyHeader); key != "" {
			t.Fatalf("unexpected idempotency key on preference update: %q", key)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := New(fastRetryConfig(server.URL)).UpdatePreference(context.Background(), "auto"); err != nil {
		t.Fatalf("UpdatePreference() error = %v", err)
	}
}

func TestRetryCapsAtMaxRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writer.WriteHeader(http.StatusServiceUnavailable) // always retryable
	}))
	defer server.Close()

	client := New(fastRetryConfig(server.URL))
	_, err := client.Availability(context.Background())
	if err == nil {
		t.Fatal("Availability() error = nil, want failure after retries exhausted")
	}
	// DefaultMaxRetries retries + 1 initial attempt.
	if got := atomic.LoadInt32(&attempts); got != int32(DefaultMaxRetries+1) {
		t.Fatalf("attempts = %d, want %d", got, DefaultMaxRetries+1)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writer.WriteHeader(http.StatusBadRequest) // 4xx: deterministic, not retryable
	}))
	defer server.Close()

	client := New(fastRetryConfig(server.URL))
	if _, err := client.Availability(context.Background()); err == nil {
		t.Fatal("Availability() error = nil, want 4xx failure")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestRetryDisabledWithNegativeMaxRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, MaxRetries: -1, RetryBackoff: time.Millisecond})
	if _, err := client.Availability(context.Background()); err == nil {
		t.Fatal("Availability() error = nil, want failure")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (retries disabled)", got)
	}
}

func TestRetryEventuallySucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(`{"available":true}`))
	}))
	defer server.Close()

	response, err := New(fastRetryConfig(server.URL)).Availability(context.Background())
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if !response.Available {
		t.Fatalf("response = %#v", response)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestPerAttemptTimeoutIsRetryable(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			time.Sleep(120 * time.Millisecond) // exceed the per-attempt timeout
			return
		}
		_, _ = writer.Write([]byte(`{"available":true}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Timeout: 40 * time.Millisecond, RetryBackoff: time.Millisecond})
	response, err := client.Availability(context.Background())
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if !response.Available {
		t.Fatalf("response = %#v", response)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2 (first attempt timed out and was retried)", got)
	}
}

func TestParentContextCancellationStopsRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first attempt has certainly run.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	// Large backoff so cancellation, not backoff completion, ends the wait.
	client := New(Config{BaseURL: server.URL, RetryBackoff: 10 * time.Second})
	if _, err := client.Availability(ctx); err == nil {
		t.Fatal("Availability() error = nil, want failure on cancellation")
	}
	// Should stop well before exhausting DefaultMaxRetries+1 attempts.
	if got := atomic.LoadInt32(&attempts); got > 1 {
		t.Fatalf("attempts = %d, want 1 (cancelled during backoff)", got)
	}
}

func TestManagedHeadersAreSingleValuedDespiteCallerHeaders(t *testing.T) {
	var accept, contentType, idem []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		accept = request.Header.Values("Accept")
		contentType = request.Header.Values("Content-Type")
		idem = request.Header.Values(idempotencyHeader)
		_, _ = writer.Write([]byte(`{"available":true,"url":"https://example.internal/x"}`))
	}))
	defer server.Close()

	// Caller headers deliberately collide with managed keys; managed values
	// must win as single values rather than duplicate on the wire.
	client := New(Config{
		BaseURL: server.URL,
		Headers: http.Header{
			"Accept":          {"caller-accept"},
			"Content-Type":    {"caller-ct"},
			idempotencyHeader: {"caller-idem"},
		},
		RetryBackoff: time.Millisecond,
	})
	if _, err := client.CreateLink(context.Background(), CreateLinkRequest{UserQuery: "hi"}); err != nil {
		t.Fatalf("CreateLink() error = %v", err)
	}
	if len(accept) != 1 || accept[0] != "application/json" {
		t.Fatalf("Accept = %v, want [application/json]", accept)
	}
	if len(contentType) != 1 || contentType[0] != "application/json" {
		t.Fatalf("Content-Type = %v, want [application/json]", contentType)
	}
	if len(idem) != 1 || idem[0] == "caller-idem" || idem[0] == "" {
		t.Fatalf("Idempotency-Key = %v, want a single generated value", idem)
	}
}

func TestIsRetryableClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"timeout", transportError(context.DeadlineExceeded), true},
		{"cancelled", transportError(context.Canceled), false},
		{"http_500", meerrors.NewServerError("HANDOFF_API_HTTP_ERROR", "boom").WithHTTPStatus(http.StatusInternalServerError), true},
		{"http_429", meerrors.NewServerError("HANDOFF_API_HTTP_ERROR", "slow down").WithHTTPStatus(http.StatusTooManyRequests), true},
		{"http_400", meerrors.NewServerError("HANDOFF_API_HTTP_ERROR", "bad").WithHTTPStatus(http.StatusBadRequest), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.want {
				t.Fatalf("isRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
