// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package transport adapts the public transport extension to net/http.
package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	exttransport "github.com/larksuite/meegle-cli/extension/transport"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

const (
	// DefaultTimeout is the maximum duration of one transport provider or hook
	// callback. The business request keeps the caller's Context and the base
	// client's timeout so enabling an extension cannot shorten data transfers.
	DefaultTimeout = 30 * time.Second
	MaxRedirects   = 10
)

// RuntimeFailure is the non-secret boundary error for a transport extension
// callback. Cause remains available to errors.Is/As without being rendered in
// CLI output.
type RuntimeFailure struct {
	Extension string
	Stage     string
	Cause     error
}

func (e *RuntimeFailure) Error() string {
	if e == nil {
		return "CLI transport extension runtime failed"
	}
	return fmt.Sprintf("CLI transport extension %q failed during %s", e.Extension, e.Stage)
}

func (e *RuntimeFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.Cause)
}

func (e *RuntimeFailure) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"code":      "CLIENT_EXTENSION_RUNTIME_FAILED",
		"message":   e.Error(),
		"retryable": false,
		"detail": map[string]any{
			"hook":  e.Extension,
			"stage": e.Stage,
		},
	}
}

func runtimeFailure(extension, stage string, cause error) error {
	return &RuntimeFailure{Extension: extension, Stage: stage, Cause: cause}
}

// ErrSecurityBaseline identifies a transport change rejected by the built-in
// CLI network policy.
var ErrSecurityBaseline = errors.New("transport security baseline rejected request")

var providerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Diagnostics is the immutable, non-secret result of resolving the process
// transport provider during CLI construction.
type Diagnostics struct {
	Provider     string
	Status       string
	FailureStage string
}

// NewHTTPClient returns base unchanged when no extension is registered.
// Otherwise it clones the client and wraps its RoundTripper, leaving SDK and
// other process-local HTTP clients untouched.
func NewHTTPClient(ctx context.Context, base *http.Client) *http.Client {
	client, _ := NewHTTPClientWithDiagnostics(ctx, base)
	return client
}

// NewHTTPClientWithDiagnostics returns the configured client together with the
// exact provider resolution state used for that client.
func NewHTTPClientWithDiagnostics(ctx context.Context, base *http.Client) (*http.Client, Diagnostics) {
	if base == nil {
		base = http.DefaultClient
	}
	provider := exttransport.GetProvider()
	if provider == nil {
		return base, Diagnostics{Provider: "built-in", Status: "active"}
	}
	ctx, cancel := withMaxTimeout(ctx)
	defer cancel()
	extension, err := ResolveProviderName(ctx, provider)
	if err != nil {
		return failClosedClient(base, extension, err), failedDiagnostics(extension, err)
	}
	interceptor, err := callResolveInterceptor(ctx, provider, extension)
	if err != nil {
		return failClosedClient(base, extension, err), failedDiagnostics(extension, err)
	}
	if isNilInterceptor(interceptor) {
		err = errors.New("transport provider returned a nil interceptor")
		return failClosedClient(base, extension, err), Diagnostics{Provider: extension, Status: "failed", FailureStage: "resolve-interceptor"}
	}
	base = withSecurityBaseline(base)

	clone := *base
	baseTransport := base.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	clone.Transport = &interceptingRoundTripper{
		base:        baseTransport,
		extension:   extension,
		interceptor: interceptor,
	}
	return &clone, Diagnostics{Provider: extension, Status: "active"}
}

func failedDiagnostics(provider string, err error) Diagnostics {
	diagnostics := Diagnostics{Provider: provider, Status: "failed"}
	var failure *RuntimeFailure
	if frameworkerrors.SafeAs(err, &failure) {
		diagnostics.FailureStage = failure.Stage
	}
	if diagnostics.FailureStage == "" {
		diagnostics.FailureStage = "resolve-interceptor"
	}
	return diagnostics
}

type interceptingRoundTripper struct {
	base        http.RoundTripper
	extension   string
	interceptor exttransport.Interceptor
}

func (t *interceptingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	hookCtx, cancelHook := withMaxTimeout(req.Context())
	defer cancelHook()
	request := req.Clone(hookCtx)
	originalBody := trackRequestBody(request)
	if !validHTTPURL(request.URL) {
		reason := fmt.Errorf("%w: extension %q received an invalid request URL", ErrSecurityBaseline, t.extension)
		closeTrackedRequestBody(originalBody)
		return abortRequest(req.Context(), t.extension, request, nil, reason)
	}
	originalScheme := request.URL.Scheme
	originalURLHost := request.URL.Host
	originalRequestHost := request.Host
	post, preErr := callPreHook(t.extension, t.interceptor, request)
	if preErr != nil {
		var failure *RuntimeFailure
		if frameworkerrors.SafeAs(preErr, &failure) {
			closeTrackedRequestBody(originalBody)
			closeRequestBody(request)
			return nil, preErr
		}
		closeTrackedRequestBody(originalBody)
		return abortRequest(req.Context(), t.extension, request, post, preErr)
	}
	if !validHTTPURL(request.URL) {
		reason := fmt.Errorf("%w: extension %q produced an invalid request URL", ErrSecurityBaseline, t.extension)
		closeTrackedRequestBody(originalBody)
		return abortRequest(req.Context(), t.extension, request, post, reason)
	}
	if request.URL.Host != originalURLHost || request.Host != originalRequestHost {
		reason := fmt.Errorf("%w: extension %q changed the request authority", ErrSecurityBaseline, t.extension)
		closeTrackedRequestBody(originalBody)
		return abortRequest(req.Context(), t.extension, request, post, reason)
	}
	if strings.EqualFold(originalScheme, "https") && !strings.EqualFold(request.URL.Scheme, "https") {
		reason := fmt.Errorf("%w: extension %q changed URL scheme from HTTPS to %q", ErrSecurityBaseline, t.extension, request.URL.Scheme)
		closeTrackedRequestBody(originalBody)
		return abortRequest(req.Context(), t.extension, request, post, reason)
	}
	closeOriginalBodyIfReplaced(request, originalBody)

	// The pre-hook's deadline protects extension code only. Restore the
	// caller-owned business Context before entering the real network transport.
	cancelHook()
	request = request.Clone(req.Context())
	resp, err := t.base.RoundTrip(request)
	if post != nil {
		if postErr := callPostHook(req.Context(), t.extension, post, responseMetadataSnapshot(resp), err); postErr != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, combineRuntimeFailure(postErr, err)
		}
	}
	return resp, err
}

type trackedRequestBody struct {
	io.ReadCloser
	closeOnce sync.Once
	closeErr  error
}

func (b *trackedRequestBody) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		if b.ReadCloser != nil {
			b.closeErr = b.ReadCloser.Close()
		}
	})
	return b.closeErr
}

func trackRequestBody(request *http.Request) *trackedRequestBody {
	if request == nil || request.Body == nil {
		return nil
	}
	tracked := &trackedRequestBody{ReadCloser: request.Body}
	request.Body = tracked
	return tracked
}

func closeTrackedRequestBody(body *trackedRequestBody) {
	if body != nil {
		_ = body.Close()
	}
}

func closeOriginalBodyIfReplaced(request *http.Request, original *trackedRequestBody) {
	if original != nil && (request == nil || request.Body != original) {
		_ = original.Close()
	}
}

// responseMetadataSnapshot prevents an untrusted post-hook from consuming or
// retaining the live response stream. Header, Trailer, and Request are copied
// so the hook may inspect or annotate its snapshot without mutating the
// response later decoded by the MCP/OAuth caller.
func responseMetadataSnapshot(resp *http.Response) *http.Response {
	if resp == nil {
		return nil
	}
	snapshot := new(http.Response)
	*snapshot = *resp
	snapshot.Header = resp.Header.Clone()
	snapshot.Trailer = resp.Trailer.Clone()
	snapshot.Body = http.NoBody
	snapshot.TLS = cloneTLSConnectionState(resp.TLS)
	if resp.Request != nil {
		request := resp.Request.Clone(resp.Request.Context())
		request.Body = http.NoBody
		request.GetBody = nil
		request.TLS = cloneTLSConnectionState(resp.Request.TLS)
		snapshot.Request = request
	}
	return snapshot
}

func cloneTLSConnectionState(state *tls.ConnectionState) *tls.ConnectionState {
	if state == nil {
		return nil
	}
	clone := *state
	certificateClones := make(map[*x509.Certificate]*x509.Certificate)
	cloneCertificate := func(certificate *x509.Certificate) *x509.Certificate {
		if certificate == nil {
			return nil
		}
		if existing, ok := certificateClones[certificate]; ok {
			return existing
		}
		parsed, err := x509.ParseCertificate(append([]byte(nil), certificate.Raw...))
		if err != nil {
			// A live TLS certificate normally always has valid Raw DER. Keep an
			// isolated raw-only value for defensive/custom transports instead of
			// sharing any nested mutable certificate fields.
			parsed = &x509.Certificate{Raw: append([]byte(nil), certificate.Raw...)}
		}
		certificateClones[certificate] = parsed
		return parsed
	}
	clone.PeerCertificates = make([]*x509.Certificate, len(state.PeerCertificates))
	for index, certificate := range state.PeerCertificates {
		clone.PeerCertificates[index] = cloneCertificate(certificate)
	}
	clone.VerifiedChains = make([][]*x509.Certificate, len(state.VerifiedChains))
	for index, chain := range state.VerifiedChains {
		clone.VerifiedChains[index] = make([]*x509.Certificate, len(chain))
		for certificateIndex, certificate := range chain {
			clone.VerifiedChains[index][certificateIndex] = cloneCertificate(certificate)
		}
	}
	clone.SignedCertificateTimestamps = cloneByteSlices(state.SignedCertificateTimestamps)
	clone.OCSPResponse = append([]byte(nil), state.OCSPResponse...)
	clone.TLSUnique = append([]byte(nil), state.TLSUnique...)
	return &clone
}

func cloneByteSlices(values [][]byte) [][]byte {
	clones := make([][]byte, len(values))
	for index, value := range values {
		clones[index] = append([]byte(nil), value...)
	}
	return clones
}

func validHTTPURL(value *url.URL) bool {
	if value == nil || value.Host == "" {
		return false
	}
	return strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https")
}

type failingRoundTripper struct {
	err error
}

func (t *failingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	closeRequestBody(request)
	return nil, t.err
}

func failClosedClient(base *http.Client, extension string, reason error) *http.Client {
	clone := *base
	var failure *RuntimeFailure
	if frameworkerrors.SafeAs(reason, &failure) {
		clone.Transport = &failingRoundTripper{err: reason}
	} else {
		clone.Transport = &failingRoundTripper{err: &exttransport.AbortError{Extension: extension, Reason: frameworkerrors.GuardCause(reason)}}
	}
	return &clone
}

func withSecurityBaseline(base *http.Client) *http.Client {
	clone := *base
	baseRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("%w: stopped after %d redirects", ErrSecurityBaseline, MaxRedirects)
		}
		sourceScheme := redirectSourceScheme(via)
		if err := validateRedirectSecurity(request, sourceScheme); err != nil {
			return err
		}
		if baseRedirect != nil {
			if err := baseRedirect(request, via); err != nil {
				return err
			}
		}
		return validateRedirectSecurity(request, sourceScheme)
	}
	return &clone
}

func redirectSourceScheme(via []*http.Request) string {
	if len(via) == 0 || via[len(via)-1] == nil || via[len(via)-1].URL == nil {
		return ""
	}
	return via[len(via)-1].URL.Scheme
}

func validateRedirectSecurity(request *http.Request, sourceScheme string) error {
	if request != nil && request.URL != nil &&
		strings.EqualFold(sourceScheme, "https") && !strings.EqualFold(request.URL.Scheme, "https") {
		return fmt.Errorf("%w: redirect changed URL scheme from HTTPS to %q", ErrSecurityBaseline, request.URL.Scheme)
	}
	return nil
}

func abortRequest(ctx context.Context, extension string, request *http.Request, post func(*http.Response, error), reason error) (*http.Response, error) {
	closeRequestBody(request)
	abortErr := &exttransport.AbortError{Extension: extension, Reason: frameworkerrors.GuardCause(reason)}
	if post != nil {
		if postErr := callPostHook(ctx, extension, post, nil, reason); postErr != nil {
			return nil, combineRuntimeFailure(postErr, abortErr)
		}
	}
	return nil, abortErr
}

func closeRequestBody(request *http.Request) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
}

func callProviderName(ctx context.Context, provider exttransport.Provider) (name string, err error) {
	result := make(chan providerNameResult, 1)
	go func() {
		value := providerNameResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.err = panicCause(recovered)
			}
			result <- value
		}()
		value.name = provider.Name()
	}()
	select {
	case value := <-result:
		if value.err != nil {
			return "<unavailable>", runtimeFailure("<unavailable>", "provider-name", value.err)
		}
		name = value.name
	case <-ctx.Done():
		return "<unavailable>", runtimeFailure("<unavailable>", "provider-name", ctx.Err())
	}
	if !providerNamePattern.MatchString(name) {
		return "<invalid>", runtimeFailure("<invalid>", "provider-name", fmt.Errorf("invalid provider name %q", name))
	}
	return name, nil
}

// ResolveProviderName resolves and validates an untrusted provider name under
// the same deadline used by HTTP setup and diagnostics. The returned name is
// always safe to print, including when err is non-nil.
func ResolveProviderName(ctx context.Context, provider exttransport.Provider) (string, error) {
	ctx, cancel := withMaxTimeout(ctx)
	defer cancel()
	return callProviderName(ctx, provider)
}

type providerNameResult struct {
	name string
	err  error
}

func callResolveInterceptor(ctx context.Context, provider exttransport.Provider, extension ...string) (interceptor exttransport.Interceptor, err error) {
	ctx, cancel := withMaxTimeout(ctx)
	defer cancel()
	result := make(chan resolveResult, 1)
	go func() {
		value := resolveResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.err = panicCause(recovered)
			}
			result <- value
		}()
		value.interceptor = provider.ResolveInterceptor(ctx)
	}()
	extensionName := "<unavailable>"
	if len(extension) > 0 && extension[0] != "" {
		extensionName = extension[0]
	}
	select {
	case value := <-result:
		if value.err != nil {
			return nil, runtimeFailure(extensionName, "resolve-interceptor", value.err)
		}
		return value.interceptor, nil
	case <-ctx.Done():
		return nil, runtimeFailure(extensionName, "resolve-interceptor", ctx.Err())
	}
}

type resolveResult struct {
	interceptor exttransport.Interceptor
	err         error
}

func callPreHook(extension string, interceptor exttransport.Interceptor, request *http.Request) (post func(*http.Response, error), err error) {
	result := make(chan preHookResult, 1)
	go func() {
		value := preHookResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.runtimeErr = panicCause(recovered)
			}
			result <- value
		}()
		if abortable, ok := interceptor.(exttransport.AbortableInterceptor); ok {
			value.post, value.err = abortable.PreRoundTripE(request)
			return
		}
		value.post = interceptor.PreRoundTrip(request)
	}()
	select {
	case value := <-result:
		if value.runtimeErr != nil {
			return nil, runtimeFailure(extension, "pre", value.runtimeErr)
		}
		return value.post, value.err
	case <-request.Context().Done():
		return nil, runtimeFailure(extension, "pre", request.Context().Err())
	}
}

type preHookResult struct {
	post       func(*http.Response, error)
	err        error
	runtimeErr error
}

func callPostHook(ctx context.Context, extension string, post func(*http.Response, error), resp *http.Response, requestErr error) error {
	ctx, cancel := withMaxTimeout(ctx)
	defer cancel()
	if resp != nil && resp.Request != nil {
		resp.Request = resp.Request.Clone(ctx)
	}
	result := make(chan error, 1)
	go func() {
		var callErr error
		defer func() {
			if recovered := recover(); recovered != nil {
				callErr = panicCause(recovered)
			}
			result <- callErr
		}()
		post(resp, requestErr)
	}()
	select {
	case callErr := <-result:
		if callErr != nil {
			return runtimeFailure(extension, "post", callErr)
		}
		return nil
	case <-ctx.Done():
		return runtimeFailure(extension, "post", ctx.Err())
	}
}

func combineRuntimeFailure(runtimeErr, other error) error {
	var failure *RuntimeFailure
	if !frameworkerrors.SafeAs(runtimeErr, &failure) || other == nil {
		return runtimeErr
	}
	return &RuntimeFailure{
		Extension: failure.Extension,
		Stage:     failure.Stage,
		Cause:     errors.Join(failure.Cause, other),
	}
}

func panicCause(recovered any) error {
	if err, ok := recovered.(error); ok {
		return &recoveredPanicError{cause: err}
	}
	return fmt.Errorf("panic: %v", recovered)
}

type recoveredPanicError struct{ cause error }

func (*recoveredPanicError) Error() string { return "callback panicked with an error" }
func (e *recoveredPanicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.cause)
}

func withMaxTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= DefaultTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

func isNilInterceptor(interceptor exttransport.Interceptor) bool {
	if interceptor == nil {
		return true
	}
	value := reflect.ValueOf(interceptor)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
