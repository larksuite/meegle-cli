// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package cmd exposes the embeddable Meegle CLI process entry point.
package cmd

import (
	"context"
	"fmt"
	"os"

	productmeegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/commands"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/integrations/formatting"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

var version = "dev"

// Execute runs Meegle CLI with the current process arguments and streams.
// It returns the process exit code and never calls os.Exit, so an embedding
// binary can perform its own deferred cleanup before exiting.
func Execute() int {
	return ExecuteWithVersion(version)
}

// ExecuteWithVersion runs Meegle CLI with an explicit distribution version.
// Enterprise entry points should use this form when a platform plugin declares
// RequiredCLIVersion, so the compatibility check uses the enterprise binary's
// own release version instead of the official entry point's link-time value.
func ExecuteWithVersion(cliVersion string) int {
	app, err := newCLIApp(cliVersion)
	if err != nil {
		return renderError(err)
	}
	if err := app.Execute(context.Background(), os.Args[1:]); err != nil {
		if frameworkerrors.SafeIs(err, productmeegle.ErrFirstRunSetupComplete) {
			return 0
		}
		var statusExit *commands.StatusExitError
		if frameworkerrors.SafeAs(err, &statusExit) {
			return statusExit.Code
		}
		return renderError(err)
	}
	return 0
}

func newCLIApp(cliVersion string) (*cliapp.App, error) {
	return productmeegle.NewCLIApp(cliVersion, &productmeegle.StaticCommands{
		Auth:       commands.NewAuthCmd(),
		Config:     commands.NewConfigCmd(),
		Inspect:    commands.NewInspectCmdWithProvider(productmeegle.ResolveMappedCommands),
		Completion: commands.NewCompletionCmd(),
		URL:        commands.NewURLCmd(),
	})
}

func renderError(err error) int {
	if cliapp.IsSuccessfulExit(err) {
		return 0
	}
	var rendered *cliapp.AlreadyRenderedError
	if frameworkerrors.SafeAs(err, &rendered) {
		return frameworkerrors.ExitCode(err)
	}
	if cliapp.RenderExplicitError(os.Args[1:], os.Stderr, formatting.DefaultProcessor(), err) {
		return frameworkerrors.ExitCode(err)
	}
	var me *meerrors.MeegleError
	if frameworkerrors.SafeAs(err, &me) {
		_, _ = fmt.Fprintln(os.Stderr, meerrors.FormatText(err))
		return me.ExitCode
	}
	_, _ = fmt.Fprintln(os.Stderr, frameworkerrors.Render(err, false))
	return frameworkerrors.ExitCode(err)
}
