// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	exttransport "github.com/larksuite/meegle-cli/extension/transport"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

type providerFunc struct {
	name        string
	interceptor exttransport.Interceptor
}

type panicProvider struct{}

func (panicProvider) Name() string { return "provider-panic" }
func (panicProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	panic("factory unavailable")
}

var errProviderNamePanicSentinel = errors.New("secret-provider-name-panic")

type panicNameProvider struct{}

func (panicNameProvider) Name() string { panic(errProviderNamePanicSentinel) }
func (panicNameProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	return passInterceptor{}
}

type invalidNameProvider struct{}

func (invalidNameProvider) Name() string { return "secret\ntransport: forged" }
func (invalidNameProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	return passInterceptor{}
}

type blockingNameProvider struct{ release <-chan struct{} }

func (p blockingNameProvider) Name() string {
	<-p.release
	return "blocking-name"
}
func (blockingNameProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	return passInterceptor{}
}

func (p providerFunc) Name() string { return p.name }
func (p providerFunc) ResolveInterceptor(context.Context) exttransport.Interceptor {
	return p.interceptor
}

type typedNilInterceptor struct{}

func (*typedNilInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	panic("typed-nil interceptor was called")
}

type postPanicInterceptor struct{}

func (postPanicInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(*http.Response, error) { panic("post unavailable") }
}

type prePanicInterceptor struct{}

func (prePanicInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	panic("pre unavailable")
}

type blockingProvider struct{ release <-chan struct{} }

func (blockingProvider) Name() string { return "blocking-provider" }
func (p blockingProvider) ResolveInterceptor(context.Context) exttransport.Interceptor {
	<-p.release
	return passInterceptor{}
}

type blockingPreInterceptor struct{ release <-chan struct{} }

func (i blockingPreInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	<-i.release
	return nil
}

type readingPreInterceptor struct{ done chan<- error }

func (i readingPreInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	_, err := io.ReadAll(request.Body)
	i.done <- err
	return nil
}

type blockingPostInterceptor struct{ release <-chan struct{} }

func (i blockingPostInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(*http.Response, error) { <-i.release }
}

type inspectingBlockingPostInterceptor struct {
	started  chan<- struct{}
	release  <-chan struct{}
	observed chan<- *http.Response
}

func (i inspectingBlockingPostInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(response *http.Response, _ error) {
		i.observed <- response
		close(i.started)
		<-i.release
	}
}

type readingPostInterceptor struct{}

func (readingPostInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(response *http.Response, _ error) {
		if response != nil && response.Body != nil {
			_, _ = io.ReadAll(response.Body)
		}
	}
}

type mutatingTLSPostInterceptor struct{}

func (mutatingTLSPostInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(response *http.Response, _ error) {
		if response != nil && response.TLS != nil {
			response.TLS.Version = tls.VersionTLS10
			response.TLS.ServerName = "mutated.example.com"
		}
	}
}

type mutatingCertificatePostInterceptor struct{}

func (mutatingCertificatePostInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return func(response *http.Response, _ error) {
		if response != nil && response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
			response.TLS.PeerCertificates[0].Subject.CommonName = "mutated-response"
		}
		if response != nil && response.TLS != nil && len(response.TLS.VerifiedChains) > 0 && len(response.TLS.VerifiedChains[0]) > 0 {
			response.TLS.VerifiedChains[0][0].Subject.Organization = []string{"mutated-chain"}
		}
		if response != nil && response.Request != nil && response.Request.TLS != nil && len(response.Request.TLS.PeerCertificates) > 0 {
			response.Request.TLS.PeerCertificates[0].Subject.CommonName = "mutated-request"
		}
	}
}

type passInterceptor struct{}

func (passInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) { return nil }

type abortingInterceptor struct {
	postCalls *atomic.Int32
}

type blockingAbortingInterceptor struct {
	started chan<- struct{}
	release <-chan struct{}
}

type panickingTraversalError struct{}

func (*panickingTraversalError) Error() string { return "secret-transport-error" }
func (*panickingTraversalError) Unwrap() error { panic("secret-transport-unwrap") }
func (*panickingTraversalError) Is(error) bool { panic("secret-transport-is") }
func (*panickingTraversalError) As(any) bool   { panic("secret-transport-as") }

type maliciousErrorInterceptor struct{}

func (maliciousErrorInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return nil
}
func (maliciousErrorInterceptor) PreRoundTripE(*http.Request) (func(*http.Response, error), error) {
	return nil, &panickingTraversalError{}
}

func (abortingInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) { return nil }
func (i abortingInterceptor) PreRoundTripE(*http.Request) (func(*http.Response, error), error) {
	return func(*http.Response, error) { i.postCalls.Add(1) }, errors.New("destination denied")
}

func (blockingAbortingInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return nil
}
func (i blockingAbortingInterceptor) PreRoundTripE(*http.Request) (func(*http.Response, error), error) {
	return func(*http.Response, error) {
		close(i.started)
		<-i.release
	}, errors.New("destination denied")
}

type downgradeInterceptor struct{}

func (downgradeInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.URL.Scheme = "http"
	return nil
}

type authorityMutationInterceptor struct {
	mutate     func(*http.Request)
	postCalls  *atomic.Int32
	postErrors chan<- error
}

func (i authorityMutationInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	i.mutate(request)
	return func(_ *http.Response, err error) {
		i.postCalls.Add(1)
		i.postErrors <- err
	}
}

type allowedRequestMutationInterceptor struct{}

func (allowedRequestMutationInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.URL.Path = "/rewritten"
	request.URL.RawQuery = "mode=extension"
	request.Header.Set("X-Extension", "enabled")
	return nil
}

type replacingBodyInterceptor struct {
	replacement io.ReadCloser
}

func (i replacingBodyInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.Body = i.replacement
	request.ContentLength = -1
	return nil
}

type replacingAbortingBodyInterceptor struct {
	replacement io.ReadCloser
}

type replacingAuthorityBodyInterceptor struct {
	replacement io.ReadCloser
}

func (i replacingAuthorityBodyInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.Body = i.replacement
	request.URL.Host = "blocked.example.com"
	return nil
}

func (i replacingAbortingBodyInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.Body = i.replacement
	return nil
}

func (i replacingAbortingBodyInterceptor) PreRoundTripE(request *http.Request) (func(*http.Response, error), error) {
	request.Body = i.replacement
	return nil, errors.New("blocked after body replacement")
}

type nilURLInterceptor struct{}

func (nilURLInterceptor) PreRoundTrip(request *http.Request) func(*http.Response, error) {
	request.URL = nil
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type trackedBody struct {
	io.Reader
	closes atomic.Int32
}

// closeUnblocksReadBody implements the net/http request Body contract: Close
// is safe concurrently with Read and releases a Read waiting for more input.
type closeUnblocksReadBody struct {
	payload     []byte
	readStarted chan struct{}
	closed      chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
	closes      atomic.Int32
}

func (b *closeUnblocksReadBody) Read(target []byte) (int, error) {
	if len(b.payload) > 0 {
		count := copy(target, b.payload)
		b.payload = b.payload[count:]
		return count, nil
	}
	b.readOnce.Do(func() { close(b.readStarted) })
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *closeUnblocksReadBody) Close() error {
	b.closes.Add(1)
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func (b *trackedBody) Close() error {
	b.closes.Add(1)
	return nil
}

func TestNewHTTPClient_TypedNilInterceptorFailsClosedAndClosesRequestBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var interceptor *typedNilInterceptor
	exttransport.Register(providerFunc{name: "typed-nil", interceptor: interceptor})
	client := NewHTTPClient(context.Background(), &http.Client{})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	_, err := client.Do(request)
	if !errors.Is(err, exttransport.ErrAborted) || !strings.Contains(err.Error(), "typed-nil") {
		t.Fatalf("Do() error = %v, want fail-closed typed-nil error", err)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_NilURLFromPreHookFailsClosed(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var baseCalls atomic.Int32
	exttransport.Register(providerFunc{name: "nil-url", interceptor: nilURLInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil || !errors.Is(err, exttransport.ErrAborted) {
		t.Fatalf("Do() response=%v error=%v, want fail-closed abort", response, err)
	}
	if baseCalls.Load() != 0 {
		t.Fatalf("base transport calls = %d, want 0", baseCalls.Load())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_PostMetadataSnapshotDoesNotConsumeLiveBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	body := &trackedBody{Reader: strings.NewReader("response")}
	exttransport.Register(providerFunc{name: "reading-post", interceptor: readingPostInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		}),
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read caller response: %v", err)
	}
	if string(got) != "response" {
		t.Fatalf("caller response body = %q, want unconsumed response", got)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close caller response: %v", err)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("response body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_PostMetadataSnapshotClonesTLSState(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "tls-snapshot", interceptor: mutatingTLSPostInterceptor{}})
	liveTLS := &tls.ConnectionState{Version: tls.VersionTLS13, ServerName: "original.example.com"}
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("response")),
				TLS:        liveTLS,
			}, nil
		}),
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if response.TLS != liveTLS || response.TLS.Version != tls.VersionTLS13 || response.TLS.ServerName != "original.example.com" {
		t.Fatalf("live TLS state mutated through post-hook snapshot: %+v", response.TLS)
	}
}

func TestInterceptingRoundTripper_PostMetadataSnapshotDeepClonesCertificates(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	certificateServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := certificateServer.Certificate()
	originalCommonName := certificate.Subject.CommonName
	certificateServer.Close()

	exttransport.Register(providerFunc{name: "certificate-snapshot", interceptor: mutatingCertificatePostInterceptor{}})
	liveTLS := &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			request.TLS = liveTLS
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       http.NoBody,
				TLS:        liveTLS,
				Request:    request,
			}, nil
		}),
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	if certificate.Subject.CommonName != originalCommonName {
		t.Fatalf("live certificate mutated through post-hook snapshot: got %q want %q", certificate.Subject.CommonName, originalCommonName)
	}
}

func TestInterceptingRoundTripper_HungPostCannotRetainLiveResponseBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	observed := make(chan *http.Response, 1)
	body := &trackedBody{Reader: strings.NewReader("response")}
	exttransport.Register(providerFunc{name: "hung-post", interceptor: inspectingBlockingPostInterceptor{
		started: started, release: release, observed: observed,
	}})
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Header:     http.Header{"X-Trace": []string{"trace-1"}},
				Body:       body,
			}, nil
		}),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, requestErr := client.Do(request)
	if response != nil || requestErr == nil {
		t.Fatalf("Do() response=%v error=%v, want post timeout", response, requestErr)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("post-hook did not start")
	}
	postResponse := <-observed
	if postResponse == nil || postResponse.StatusCode != http.StatusAccepted || postResponse.Header.Get("X-Trace") != "trace-1" {
		t.Fatalf("post-hook metadata response = %#v", postResponse)
	}
	if postResponse.Body != http.NoBody {
		t.Fatalf("post-hook body = %T, want http.NoBody metadata snapshot", postResponse.Body)
	}
	deadline := time.Now().Add(time.Second)
	for body.closes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("live response body close calls = %d, want 1 without waiting for post-hook", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_PostPanicReturnsErrorAndClosesResponseBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	body := &trackedBody{Reader: strings.NewReader("response")}
	exttransport.Register(providerFunc{name: "post-panic", interceptor: postPanicInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	var response *http.Response
	var requestErr error
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		response, requestErr = client.Do(request)
	}()
	if recovered != nil {
		t.Fatalf("post hook panic escaped RoundTrip: %v", recovered)
	}
	if response != nil || requestErr == nil {
		t.Fatalf("Do() response=%v error=%v, want extension post-hook error", response, requestErr)
	}
	assertRuntimeFailureIsSafe(t, requestErr, "post", "post unavailable")
	if body.closes.Load() != 1 {
		t.Fatalf("response body close calls = %d, want 1", body.closes.Load())
	}
}

func TestNewHTTPClient_ProviderPanicBecomesFailClosedRequestError(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(panicProvider{})
	client := NewHTTPClient(context.Background(), &http.Client{})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	_, err := client.Do(request)
	if err == nil {
		t.Fatalf("Do() error = %v, want stable provider failure", err)
	}
	assertRuntimeFailureIsSafe(t, err, "resolve-interceptor", "factory unavailable")
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestNewHTTPClient_ProviderNamePanicPreservesCauseWithoutLeaking(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(panicNameProvider{})
	assertProviderNameFailureClosesRequest(context.Background(), t, "", func(err error) {
		if strings.Contains(err.Error(), errProviderNamePanicSentinel.Error()) {
			t.Fatalf("provider name panic leaked sentinel: %v", err)
		}
		if !errors.Is(err, errProviderNamePanicSentinel) {
			t.Fatalf("provider name panic chain lost sentinel: %v", err)
		}
	})
}

func TestNewHTTPClient_InvalidProviderNameIsNotPrinted(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(invalidNameProvider{})
	assertProviderNameFailureClosesRequest(context.Background(), t, "secret", func(err error) {
		var failure *RuntimeFailure
		if !errors.As(err, &failure) || failure.Extension != "<invalid>" {
			t.Fatalf("provider name failure = %T %v", err, err)
		}
	})
}

func TestNewHTTPClient_ProviderNameTimeoutStopsWaitingAndClosesRequest(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	release := make(chan struct{})
	defer close(release)
	exttransport.Register(blockingNameProvider{release: release})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assertProviderNameFailureClosesRequest(ctx, t, "deadline exceeded", nil)
}

func assertProviderNameFailureClosesRequest(ctx context.Context, t *testing.T, secret string, inspect func(error)) {
	t.Helper()
	var baseCalls atomic.Int32
	base := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})}
	client := NewHTTPClient(ctx, base)
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	_, err := client.Do(request)
	if err == nil {
		t.Fatal("Do() error = nil, want fail-closed provider-name error")
	}
	assertRuntimeFailureIsSafe(t, err, "provider-name", secret)
	if baseCalls.Load() != 0 || body.closes.Load() != 1 {
		t.Fatalf("provider name failure calls: base=%d body-close=%d, want 0 and 1", baseCalls.Load(), body.closes.Load())
	}
	if inspect != nil {
		inspect(err)
	}
}

func TestInterceptingRoundTripper_PrePanicBlocksBaseTransport(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var baseCalls atomic.Int32
	exttransport.Register(providerFunc{name: "pre-panic", interceptor: prePanicInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	var requestErr error
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, requestErr = client.Do(request)
	}()
	if recovered != nil {
		t.Fatalf("pre hook panic escaped RoundTrip: %v", recovered)
	}
	if requestErr == nil {
		t.Fatalf("Do() error = %v, want extension pre-hook error", requestErr)
	}
	assertRuntimeFailureIsSafe(t, requestErr, "pre", "pre unavailable")
	if baseCalls.Load() != 0 {
		t.Fatalf("base transport calls = %d, want 0 after pre-hook panic", baseCalls.Load())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_AbortStillRunsPostAndSkipsBaseTransport(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var baseCalls atomic.Int32
	var postCalls atomic.Int32
	exttransport.Register(providerFunc{name: "deny", interceptor: abortingInterceptor{postCalls: &postCalls}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	_, err := client.Do(request)
	if !errors.Is(err, exttransport.ErrAborted) {
		t.Fatalf("Do() error = %v, want transport abort", err)
	}
	if strings.Contains(err.Error(), "destination denied") {
		t.Fatalf("transport abort leaked extension reason: %v", err)
	}
	if got := frameworkoutput.BuildErrorRecord(err)["code"]; got != "CLIENT_EXTENSION_ABORTED" {
		t.Fatalf("transport abort code = %v", got)
	}
	if baseCalls.Load() != 0 || postCalls.Load() != 1 {
		t.Fatalf("abort calls: base=%d post=%d, want base=0 post=1", baseCalls.Load(), postCalls.Load())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_AbortPostTimeoutReturnsAndClosesRequestBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var baseCalls atomic.Int32
	exttransport.Register(providerFunc{name: "blocking-deny", interceptor: blockingAbortingInterceptor{
		started: started, release: release,
	}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	body := &trackedBody{Reader: strings.NewReader("request")}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", body)
	response, err := client.Do(request)
	if response != nil || err == nil {
		t.Fatalf("Do() response=%v error=%v, want post timeout", response, err)
	}
	assertRuntimeFailureIsSafe(t, err, "post", context.DeadlineExceeded.Error())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("abort post-hook did not start")
	}
	if baseCalls.Load() != 0 || body.closes.Load() != 1 {
		t.Fatalf("abort timeout calls: base=%d body-close=%d, want 0 and 1", baseCalls.Load(), body.closes.Load())
	}
}

func TestInterceptingRoundTripper_ContainsPanickingErrorTraversal(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "malicious-error", interceptor: maliciousErrorInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("base transport called after malicious pre-hook error")
			return nil, nil
		}),
	})
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	_, err := client.Do(request)
	if err == nil || !errors.Is(err, exttransport.ErrAborted) {
		t.Fatalf("RoundTrip() error = %v, want controlled transport abort", err)
	}
	if strings.Contains(err.Error(), "secret-transport") {
		t.Fatalf("transport error leaked malicious callback error: %v", err)
	}
	var unrelated interface{ Marker() }
	var traversalPanic any
	func() {
		defer func() { traversalPanic = recover() }()
		if errors.Is(err, errors.New("unrelated")) || errors.As(err, &unrelated) {
			t.Fatal("malicious transport error unexpectedly matched unrelated target")
		}
	}()
	if traversalPanic != nil {
		t.Fatalf("transport error traversal escaped boundary: %v", traversalPanic)
	}
}

func TestResolveInterceptor_StopsWaitingWhenContextExpires(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := callResolveInterceptor(ctx, blockingProvider{release: release})
	if err == nil {
		t.Fatal("callResolveInterceptor() error = nil, want timeout")
	}
	assertRuntimeFailureIsSafe(t, err, "resolve-interceptor", context.DeadlineExceeded.Error())
}

func TestWithMaxTimeout_CapsLongParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), time.Hour)
	defer parentCancel()
	ctx, cancel := withMaxTimeout(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("withMaxTimeout() returned context without deadline")
	}
	if remaining := time.Until(deadline); remaining > DefaultTimeout || remaining < DefaultTimeout-time.Second {
		t.Fatalf("withMaxTimeout() remaining = %s, want approximately %s", remaining, DefaultTimeout)
	}
}

func TestInterceptingRoundTripper_PreTimeoutSkipsBaseAndClosesRequestBody(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var baseCalls atomic.Int32
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)
	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Millisecond)
	defer cancel()
	request = request.WithContext(ctx)
	roundTripper := &interceptingRoundTripper{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			baseCalls.Add(1)
			return nil, nil
		}),
		extension: "blocking-pre", interceptor: blockingPreInterceptor{release: release},
	}
	_, err := roundTripper.RoundTrip(request)
	if err == nil {
		t.Fatal("RoundTrip() error = nil, want pre-hook timeout")
	}
	assertRuntimeFailureIsSafe(t, err, "pre", context.DeadlineExceeded.Error())
	if baseCalls.Load() != 0 || body.closes.Load() != 1 {
		t.Fatalf("pre timeout calls: base=%d body-close=%d, want 0 and 1", baseCalls.Load(), body.closes.Load())
	}
}

func TestInterceptingRoundTripper_PreTimeoutClosesAndUnblocksRequestBodyRead(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var baseCalls atomic.Int32
	hookDone := make(chan error, 1)
	body := &closeUnblocksReadBody{
		payload:     []byte("request"),
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
	exttransport.Register(providerFunc{name: "reading-pre", interceptor: readingPreInterceptor{done: hookDone}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, requestErr := client.Do(request)
	if response != nil || requestErr == nil {
		t.Fatalf("Do() response=%v error=%v, want pre-hook timeout", response, requestErr)
	}
	assertRuntimeFailureIsSafe(t, requestErr, "pre", context.DeadlineExceeded.Error())
	select {
	case <-body.readStarted:
	case <-time.After(time.Second):
		t.Fatal("pre-hook did not read the request body")
	}
	select {
	case readErr := <-hookDone:
		if !errors.Is(readErr, io.ErrClosedPipe) {
			t.Fatalf("pre-hook body read error = %v, want Close to unblock with io.ErrClosedPipe", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("request Body.Close did not unblock the pre-hook read")
	}
	if baseCalls.Load() != 0 || body.closes.Load() != 1 {
		t.Fatalf("pre timeout calls: base=%d body-close=%d, want 0 and 1", baseCalls.Load(), body.closes.Load())
	}
}

func TestInterceptingRoundTripper_PostTimeoutClosesResponseBodyWithoutWaitingForHook(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	body := &trackedBody{Reader: strings.NewReader("response")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Millisecond)
	defer cancel()
	request = request.WithContext(ctx)
	roundTripper := &interceptingRoundTripper{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		}),
		extension: "blocking-post", interceptor: blockingPostInterceptor{release: release},
	}
	response, err := roundTripper.RoundTrip(request)
	if response != nil || err == nil {
		t.Fatalf("RoundTrip() response=%v error=%v, want post-hook timeout", response, err)
	}
	assertRuntimeFailureIsSafe(t, err, "post", context.DeadlineExceeded.Error())
	if body.closes.Load() != 1 {
		t.Fatalf("response body close calls while post-hook is blocked = %d, want 1", body.closes.Load())
	}
}

func assertRuntimeFailureIsSafe(t *testing.T, err error, stage, secret string) {
	t.Helper()
	var failure *RuntimeFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T = %v, want RuntimeFailure", err, err)
	}
	if failure.Stage != stage {
		t.Fatalf("runtime failure stage = %q, want %q", failure.Stage, stage)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatalf("public error leaked callback cause %q: %v", secret, err)
	}
	if got := frameworkoutput.BuildErrorRecord(err)["code"]; got != "CLIENT_EXTENSION_RUNTIME_FAILED" {
		t.Fatalf("runtime failure code = %v", got)
	}
	if failure.Cause == nil || (secret != "" && !strings.Contains(fmt.Sprint(failure.Cause), secret)) {
		t.Fatalf("internal cause = %v, want %q", failure.Cause, secret)
	}
}

func TestNewHTTPClient_PreservesCallerTimeoutAndRedirectTLSBaseline(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "noop", interceptor: passInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Timeout: time.Minute})
	if client.Timeout != time.Minute {
		t.Fatalf("Timeout = %s, want caller timeout %s", client.Timeout, time.Minute)
	}

	previous, _ := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "http://example.com/end", nil)
	if err := client.CheckRedirect(next, []*http.Request{previous}); !errors.Is(err, ErrSecurityBaseline) {
		t.Fatalf("CheckRedirect() error = %v, want security baseline rejection", err)
	}
}

func TestExtensionTransport_DoesNotCapBusinessRequestLifetime(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "noop", interceptor: passInterceptor{}})
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if deadline, ok := request.Context().Deadline(); ok {
			return nil, fmt.Errorf("business request received extension deadline %s", deadline)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("complete")),
			Request:    request,
		}, nil
	})}
	client := NewHTTPClient(context.Background(), base)
	if client.Timeout != 0 {
		t.Fatalf("extension client timeout = %s, want original unlimited business timeout", client.Timeout)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/slow-transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("business request failed: %v", err)
	}
	defer response.Body.Close()
	if body, readErr := io.ReadAll(response.Body); readErr != nil || string(body) != "complete" {
		t.Fatalf("business response body=%q error=%v", body, readErr)
	}
}

func TestNewHTTPClient_BaseRedirectCallbackCannotReintroduceTLSDowngrade(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "noop", interceptor: passInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			request.URL.Scheme = "http"
			return nil
		},
	})

	previous, _ := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "https://example.com/end", nil)
	if err := client.CheckRedirect(next, []*http.Request{previous}); !errors.Is(err, ErrSecurityBaseline) {
		t.Fatalf("CheckRedirect() error = %v, want security baseline rejection after base callback", err)
	}
}

func TestNewHTTPClient_BaseRedirectCallbackCannotHideTLSDowngradeByMutatingRedirectHistory(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var targetCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(w, request, "/target", http.StatusFound)
			return
		}
		targetCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	exttransport.Register(providerFunc{name: "noop", interceptor: passInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			request.URL.Scheme = "http"
			via[len(via)-1].URL.Scheme = "http"
			return nil
		},
	})

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/start", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.Do(request)
	if !errors.Is(err, ErrSecurityBaseline) {
		t.Fatalf("Do() error = %v, want immutable TLS downgrade rejection", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}

func TestInterceptingRoundTripper_BlocksTLSDowngradeAndClosesRequestBody(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	var baseCalls atomic.Int32
	exttransport.Register(providerFunc{name: "downgrade", interceptor: downgradeInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return nil, nil
	})})
	body := &trackedBody{Reader: strings.NewReader("request")}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", body)

	_, err := client.Do(request)
	if !errors.Is(err, ErrSecurityBaseline) {
		t.Fatalf("Do() error = %v, want TLS downgrade rejection", err)
	}
	if baseCalls.Load() != 0 {
		t.Fatalf("base transport calls = %d, want 0", baseCalls.Load())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
	}
}

func TestInterceptingRoundTripper_BlocksAuthorityChangesBeforeCredentialedBaseTransport(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "url-host", mutate: func(request *http.Request) { request.URL.Host = "evil.example.com" }},
		{name: "url-port", mutate: func(request *http.Request) { request.URL.Host = "example.com:444" }},
		{name: "request-host", mutate: func(request *http.Request) { request.Host = "evil.example.com" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if runClientSubprocess(t) {
				return
			}
			var baseCalls atomic.Int32
			var postCalls atomic.Int32
			postErrors := make(chan error, 1)
			exttransport.Register(providerFunc{name: "authority-mutator", interceptor: authorityMutationInterceptor{
				mutate: test.mutate, postCalls: &postCalls, postErrors: postErrors,
			}})
			client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				baseCalls.Add(1)
				if request.Header.Get("Authorization") != "" || request.Header.Get("X-Enterprise-Token") != "" {
					t.Error("base transport received a credential after authority mutation")
				}
				return nil, nil
			})})
			body := &trackedBody{Reader: strings.NewReader("request")}
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/original", body)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer never-forward-this-token")
			request.Header.Set("X-Enterprise-Token", "never-forward-this-custom-token")

			response, err := client.Do(request)
			if response != nil || !errors.Is(err, ErrSecurityBaseline) {
				t.Fatalf("Do() response=%v error=%v, want fail-closed authority rejection", response, err)
			}
			if baseCalls.Load() != 0 {
				t.Fatalf("base transport calls = %d, want 0", baseCalls.Load())
			}
			if body.closes.Load() != 1 {
				t.Fatalf("request body close calls = %d, want 1", body.closes.Load())
			}
			if postCalls.Load() != 1 {
				t.Fatalf("post-hook calls = %d, want 1", postCalls.Load())
			}
			if postErr := <-postErrors; !errors.Is(postErr, ErrSecurityBaseline) {
				t.Fatalf("post-hook error = %v, want security baseline rejection", postErr)
			}
		})
	}
}

func TestInterceptingRoundTripper_AllowsNonAuthorityRequestChanges(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	exttransport.Register(providerFunc{name: "request-rewriter", interceptor: allowedRequestMutationInterceptor{}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "example.com" || request.Host != "example.com" {
			t.Fatalf("request authority = URL.Host %q Request.Host %q", request.URL.Host, request.Host)
		}
		if request.URL.Path != "/rewritten" || request.URL.RawQuery != "mode=extension" || request.Header.Get("X-Extension") != "enabled" {
			t.Fatalf("allowed request changes were not preserved: %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/original", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestInterceptingRoundTripper_ReplacedRequestBodyClosesOriginalAndReplacement(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read server request body: %v", err)
		}
		received <- string(body)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	original := &trackedBody{Reader: strings.NewReader("original")}
	replacement := &trackedBody{Reader: strings.NewReader("replacement")}
	exttransport.Register(providerFunc{name: "replace-body", interceptor: replacingBodyInterceptor{replacement: replacement}})
	client := NewHTTPClient(context.Background(), server.Client())
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, original)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	if got := <-received; got != "replacement" {
		t.Fatalf("server body = %q, want replacement", got)
	}
	if original.closes.Load() != 1 || replacement.closes.Load() != 1 {
		t.Fatalf("body close calls: original=%d replacement=%d, want 1 each", original.closes.Load(), replacement.closes.Load())
	}
}

func TestInterceptingRoundTripper_AbortAfterBodyReplacementClosesBothBodies(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	original := &trackedBody{Reader: strings.NewReader("original")}
	replacement := &trackedBody{Reader: strings.NewReader("replacement")}
	exttransport.Register(providerFunc{name: "replace-body-abort", interceptor: replacingAbortingBodyInterceptor{replacement: replacement}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("base transport called after pre-hook abort")
		return nil, nil
	})})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", original)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil || !errors.Is(err, exttransport.ErrAborted) {
		t.Fatalf("Do() response=%v error=%v, want abort", response, err)
	}
	if original.closes.Load() != 1 || replacement.closes.Load() != 1 {
		t.Fatalf("body close calls: original=%d replacement=%d, want 1 each", original.closes.Load(), replacement.closes.Load())
	}
}

func TestInterceptingRoundTripper_SecurityRejectAfterBodyReplacementClosesBothBodies(t *testing.T) {
	if runClientSubprocess(t) {
		return
	}
	original := &trackedBody{Reader: strings.NewReader("original")}
	replacement := &trackedBody{Reader: strings.NewReader("replacement")}
	exttransport.Register(providerFunc{name: "replace-body-authority", interceptor: replacingAuthorityBodyInterceptor{replacement: replacement}})
	client := NewHTTPClient(context.Background(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("base transport called after authority change")
		return nil, nil
	})})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", original)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil || !errors.Is(err, ErrSecurityBaseline) {
		t.Fatalf("Do() response=%v error=%v, want security baseline rejection", response, err)
	}
	if original.closes.Load() != 1 || replacement.closes.Load() != 1 {
		t.Fatalf("body close calls: original=%d replacement=%d, want 1 each", original.closes.Load(), replacement.closes.Load())
	}
}

func runClientSubprocess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("TRANSPORT_CLIENT_HELPER") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
	command.Env = append(os.Environ(), "TRANSPORT_CLIENT_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("transport client helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("transport client helper failed: %v\n%s", err, output)
	}
	return true
}
