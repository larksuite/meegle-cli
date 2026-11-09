// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"context"
	"fmt"

	"github.com/larksuite/meegle-cli/pkg/framework/adapter"
	"github.com/larksuite/meegle-cli/pkg/framework/fileio"
	"github.com/larksuite/meegle-cli/pkg/framework/output/encoders"
)

// File exports consume the business result before presentation-only hooks.
func (p *Processor) processFiles(ctx context.Context, state *Context) (err error) {
	if state.Result == nil {
		return &fileio.OutputError{Stage: "result", Cause: fmt.Errorf("command returned no result"), BusinessExecution: "unknown"}
	}
	if state.Result.Err != nil {
		return state.Result.Err
	}
	data, saved, err := fileio.Export(ctx, state.Result.Data, state.Parsed.FileOutputs)
	if err != nil {
		// Failure metadata must win over any similarly named backend metadata.
		if failure, ok := err.(*fileio.OutputError); ok {
			mergeFileMetadata(state, failure.ErrorMetadata())
		}
		return err
	}
	stage := "format"
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("output writer panicked")
		}
		if err != nil {
			failure := &fileio.OutputError{Stage: stage, Files: saved, Cause: err}
			mergeFileMetadata(state, failure.ErrorMetadata())
			err = failure
		}
	}()
	mergeFileMetadata(state, map[string]any{"output_files": saved})
	if err := ctx.Err(); err != nil {
		return err
	}
	state.Result.Data = map[string]any{"data": data, "meta": state.Result.Metadata, "error": nil}
	if state.Input != nil && state.Input.IOMode == adapter.IOModeProgrammatic {
		return nil
	}
	payload, err := encoders.EncodeJSON(state.Result.Data)
	if err != nil {
		return err
	}
	stage = "stdout"
	if p.Writer == nil {
		return fmt.Errorf("output writer is unavailable")
	}
	return p.Writer.Write(state, payload)
}

func mergeFileMetadata(state *Context, values map[string]any) {
	meta := make(map[string]any, len(state.Result.Metadata)+len(values))
	for key, value := range state.Result.Metadata {
		meta[key] = value
	}
	for key, value := range values {
		meta[key] = value
	}
	state.Result.Metadata = meta
}
