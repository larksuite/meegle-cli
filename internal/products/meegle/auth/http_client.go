// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"net/http"
)

type httpClientContextKey struct{}

// WithHTTPClient binds a CLI-scoped HTTP client to an auth operation without
// mutating http.DefaultClient. SDK callers that do not decorate their context
// continue to use the standard client.
func WithHTTPClient(ctx context.Context, client *http.Client) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return ctx
	}
	return context.WithValue(ctx, httpClientContextKey{}, client)
}

// HTTPClient returns the client bound to ctx, or http.DefaultClient when the
// caller did not opt into a scoped client.
func HTTPClient(ctx context.Context) *http.Client {
	if ctx != nil {
		if client, ok := ctx.Value(httpClientContextKey{}).(*http.Client); ok && client != nil {
			return client
		}
	}
	return http.DefaultClient
}
