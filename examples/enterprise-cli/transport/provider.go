// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package transport registers a sample enterprise network interceptor.
package transport

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	transportapi "github.com/larksuite/meegle-cli/extension/transport"
)

type provider struct{}
type interceptor struct{}

func (provider) Name() string { return "corp-network" }
func (provider) ResolveInterceptor(context.Context) transportapi.Interceptor {
	return interceptor{}
}

func (interceptor) PreRoundTrip(*http.Request) func(*http.Response, error) {
	return nil
}

func (interceptor) PreRoundTripE(req *http.Request) (func(*http.Response, error), error) {
	allowedSuffix := os.Getenv("CORP_ALLOWED_HOST_SUFFIX")
	if allowedSuffix != "" && !strings.HasSuffix(req.URL.Hostname(), allowedSuffix) {
		return nil, fmt.Errorf("destination %q is outside the corporate network", req.URL.Hostname())
	}

	started := time.Now()
	req.Header.Set("X-Corp-Caller", "meegle-cli")
	return func(resp *http.Response, err error) {
		_ = resp
		_ = err
		_ = time.Since(started) // Replace with the enterprise metrics sink.
	}, nil
}

func init() {
	transportapi.Register(provider{})
}
