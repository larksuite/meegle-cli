// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package governance registers sample command governance hooks.
package governance

import (
	"context"
	"fmt"
	"os"

	"github.com/larksuite/meegle-cli/extension/platform"
)

func observe(_ context.Context, invocation platform.Invocation) {
	if err := invocation.Err(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "[corp-audit] %s failed: %v\n", invocation.Cmd().Path(), err)
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "[corp-audit] %s ok\n", invocation.Cmd().Path())
}

func wrap(next platform.Handler) platform.Handler {
	return func(ctx context.Context, invocation platform.Invocation) error {
		if os.Getenv("CORP_TRACE_WRAP") == "1" {
			_, _ = fmt.Fprintf(os.Stderr, "[corp-wrap] %s\n", invocation.Cmd().Path())
		}
		return next(ctx, invocation)
	}
}

func init() {
	builder := platform.NewPlugin("corp-governance", "1.0.0").
		Observer(platform.After, "audit", platform.All(), observe).
		Wrap("command-context", platform.All(), wrap).
		On(platform.Startup, "startup", func(context.Context, *platform.LifecycleContext) error {
			_, _ = fmt.Fprintln(os.Stderr, "[corp-lifecycle] startup")
			return nil
		}).
		On(platform.Shutdown, "shutdown", func(context.Context, *platform.LifecycleContext) error {
			_, _ = fmt.Fprintln(os.Stderr, "[corp-lifecycle] shutdown")
			return nil
		})

	if os.Getenv("CORP_READ_ONLY") == "1" {
		builder.Restrict(&platform.Rule{
			Name: "agent-readonly",
			// Keep non-secret diagnostics available so operators can inspect
			// the active policy after business commands are restricted.
			Allow:   []string{"workitem/**", "view/**", "extension/**"},
			MaxRisk: platform.RiskRead,
		})
	} else {
		builder.FailOpen()
	}
	platform.Register(builder.MustBuild())
}
