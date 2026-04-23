// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

type Handler func(ctx context.Context, req *Request) (*RawResult, error)

type DirectExecutor struct {
	handlers map[string]Handler
}

func NewDirectExecutor(handlers map[string]Handler) *DirectExecutor {
	copyHandlers := make(map[string]Handler, len(handlers))
	for key, handler := range handlers {
		copyHandlers[key] = handler
	}
	return &DirectExecutor{handlers: copyHandlers}
}

func (b *DirectExecutor) Execute(ctx context.Context, req *Request) (*RawResult, error) {
	if b == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, "direct executor is nil")
	}
	if req == nil || req.Command == nil || req.Command.Node == nil {
		return nil, frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, "executor request is missing command")
	}
	handler, ok := b.handlers[req.Command.Node.HandlerRef]
	if !ok {
		return nil, frameworkerrors.New(frameworkerrors.CategoryConfig, frameworkerrors.CodeConfigInvalid, fmt.Sprintf("handler %s is not registered", req.Command.Node.HandlerRef))
	}
	return handler(ctx, req)
}
