// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package executor

import "context"

type Func func(ctx context.Context, req *Request) (*RawResult, error)

func (f Func) Execute(ctx context.Context, req *Request) (*RawResult, error) {
	return f(ctx, req)
}
