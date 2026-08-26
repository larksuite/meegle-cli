// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package transport_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/larksuite/meegle-cli/extension/transport"
)

type namedProvider string
type noopInterceptor struct{}
type pointerProvider struct{}

func (p namedProvider) Name() string { return string(p) }
func (namedProvider) ResolveInterceptor(context.Context) transport.Interceptor {
	return noopInterceptor{}
}
func (noopInterceptor) PreRoundTrip(*http.Request) func(*http.Response, error) { return nil }
func (*pointerProvider) Name() string                                          { return "pointer" }
func (*pointerProvider) ResolveInterceptor(context.Context) transport.Interceptor {
	return noopInterceptor{}
}

func TestRegister_LastProviderWins(t *testing.T) {
	transport.Register(namedProvider("first"))
	transport.Register(namedProvider("second"))

	provider := transport.GetProvider()
	if provider == nil || provider.Name() != "second" {
		t.Fatalf("GetProvider() = %v, want second provider", provider)
	}
}

func TestRegister_IgnoresNilAndTypedNilProviders(t *testing.T) {
	transport.Register(namedProvider("stable"))
	transport.Register(nil)
	var provider *pointerProvider
	transport.Register(provider)

	got := transport.GetProvider()
	if got == nil || got.Name() != "stable" {
		t.Fatalf("GetProvider() = %v, want previously active provider", got)
	}
}
