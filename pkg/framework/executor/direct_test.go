// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	frameworkrouter "github.com/larksuite/meegle-cli/pkg/framework/router"
)

func TestDirectExecutorExecutesByHandlerRef(t *testing.T) {
	exec := NewDirectExecutor(map[string]Handler{
		"core.test.echo": func(ctx context.Context, req *Request) (*RawResult, error) {
			_ = ctx
			return &RawResult{Data: req.Values}, nil
		},
	})
	result, err := exec.Execute(context.Background(), &Request{
		Command: &frameworkrouter.ParsedCommand{Node: &registry.CommandNode{HandlerRef: "core.test.echo"}},
		Values:  map[string]any{"query": "abc"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["query"] != "abc" {
		t.Fatalf("result data = %#v", result.Data)
	}
}
