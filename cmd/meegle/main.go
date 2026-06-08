// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"

	productmeegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/commands"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

var version = "dev"

func main() {
	// Propagate the ldflags-injected version into the User-Agent. Without this
	// the UA falls back to Go's debug.ReadBuildInfo() pseudo-version, which is
	// what backends observe today (e.g. "meegle-cli/v0.0.0-…+dirty").
	mcpclient.SetVersion(version)

	app, err := productmeegle.NewCLIApp(version, &productmeegle.StaticCommands{
		Auth:       commands.NewAuthCmd(),
		Config:     commands.NewConfigCmd(),
		Inspect:    commands.NewInspectCmdWithProvider(productmeegle.ResolveMappedCommands),
		Completion: commands.NewCompletionCmd(),
		URL:        commands.NewURLCmd(),
	})
	if err != nil {
		fail(err)
	}
	if err := app.Execute(context.Background(), os.Args[1:]); err != nil {
		fail(err)
	}
}

// fail prints err to stderr and exits. Pipeline errors are already rendered
// as a format-aware envelope inside App.Execute (see cliapp.AlreadyRenderedError);
// for those we only need to exit with the right code. Pre-pipeline errors —
// typically cobra parse failures or app construction — fall back to the
// product-specific MeegleError text (with Suggestion) or the framework's
// plain-text renderer.
func fail(err error) {
	var rendered *cliapp.AlreadyRenderedError
	if stderrors.As(err, &rendered) {
		os.Exit(frameworkerrors.ExitCode(err))
	}
	var me *meerrors.MeegleError
	if stderrors.As(err, &me) {
		_, _ = fmt.Fprintln(os.Stderr, meerrors.FormatText(err))
		os.Exit(me.ExitCode)
	}
	_, _ = fmt.Fprintln(os.Stderr, frameworkerrors.Render(err, false))
	os.Exit(frameworkerrors.ExitCode(err))
}
