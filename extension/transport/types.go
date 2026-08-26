// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package transport defines the public Meegle CLI HTTP extension seam.
package transport

import (
	"context"
	"net/http"
)

// Provider creates the interceptor used by the CLI HTTP client factory.
type Provider interface {
	Name() string
	ResolveInterceptor(ctx context.Context) Interceptor
}

// Interceptor wraps a round trip with an optional pre/post hook pair. The pre
// hook receives the live request after credentials are injected, so trusted
// in-process code can technically read, change, or remove authentication
// headers. The CLI does not isolate extensions or freeze header values. The
// hook may adjust headers, path, and query parameters, but it must not change
// req.URL.Host or req.Host. The CLI freezes both authority values and rejects
// a changed destination before the built-in transport is called. The
// hook must stop using req when PreRoundTrip returns or req.Context() is done.
// Reading req.Body consumes the request stream, so the hook must restore it
// before a successful return. Any replacement Body must obey net/http's
// request-body contract: Read is safe concurrently with Close, and Close
// unblocks Read. When the hook replaces Body, the CLI closes the original
// Body before dispatch and the base transport owns the replacement, so both
// streams are released exactly once along their respective paths.
//
// Provider, pre-hook, and post-hook execution is bounded independently; those
// callback limits do not shorten the caller-owned network request. The post
// hook receives a metadata snapshot: resp.Body is always http.NoBody, while
// Header, Trailer, Request, TLS state, and certificate objects are copied from
// the live response. When Request
// is present, its Context carries the post-hook deadline. The hook may inspect
// status and metadata, but it cannot consume or retain the caller's response
// stream. If it outlives its deadline, the CLI reports a timeout and closes the
// live body immediately.
type Interceptor interface {
	PreRoundTrip(req *http.Request) func(resp *http.Response, err error)
}

// AbortableInterceptor may reject a request before the built-in transport is
// called. When this interface is implemented, PreRoundTripE is used instead
// of PreRoundTrip and follows the same request ownership and Body contract.
type AbortableInterceptor interface {
	Interceptor
	PreRoundTripE(req *http.Request) (post func(resp *http.Response, err error), err error)
}
