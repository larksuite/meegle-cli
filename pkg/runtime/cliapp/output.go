// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package cliapp re-exports the framework formatter types so that callers that
// directly import cliapp can reference Formatter and Writer without also having
// to import the framework's formatter sub-package.
package cliapp

import "github.com/larksuite/meegle-cli/pkg/framework/output/formatter"

// Formatter is the standard CLI output formatter backed by
// framework/output/formatter.Standard.  It implements framework/output.Formatter.
type Formatter = formatter.Standard

// Writer is the standard CLI stdio writer backed by
// framework/output/formatter.Stdio.  It implements framework/output.IOWriter.
type Writer = formatter.Stdio
