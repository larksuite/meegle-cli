// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pipeline

import (
	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/router"
)

type PipelineContext struct {
	Input        *frameworkadapter.RawInput
	Parsed       *router.ParsedCommand
	Result       *executor.RawResult
	Err          *frameworkerrors.CLIError
	OutputConfig map[string]any
	Values       map[string]any
}
