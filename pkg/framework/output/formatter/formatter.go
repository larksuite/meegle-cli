// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package formatter provides the framework-level Formatter and IOWriter
// implementations. Standard dispatches the stream formats (json, ndjson);
// table rendering is provided by pkg/integrations/formatting.TableView and
// assembled by product-level DefaultProcessor() functions.
package formatter

import (
	"fmt"
	"io"
	"strings"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/output/encoders"
)

// Standard handles the two stream formats (json, ndjson).
// Table dispatch lives in the mid-layer; Standard returns an error if asked
// to format "table" so a misconfigured Processor fails loudly.
type Standard struct{}

func (Standard) Format(data any, opts *frameworkoutput.FormatOptions) ([]byte, error) {
	mode := "json"
	if opts != nil && strings.TrimSpace(opts.Mode) != "" {
		mode = strings.TrimSpace(opts.Mode)
	}
	switch mode {
	case "ndjson":
		return encoders.EncodeNDJSON(data)
	case "json", "":
		return encoders.EncodeJSON(data)
	default:
		return nil, fmt.Errorf("unsupported format %q (framework standard handles json/ndjson only)", mode)
	}
}

// Stdio writes payload to the writer carried by the output.Context. It is a
// no-op in IOModeProgrammatic, where the caller collects the result from
// state.Result directly.
type Stdio struct{}

func (Stdio) Write(ctx *frameworkoutput.Context, payload []byte) error {
	if ctx == nil {
		return nil
	}
	writer := io.Writer(nil)
	if ctx.Input != nil {
		writer = ctx.Input.Stdout
	}
	if writer == nil {
		return nil
	}
	if len(payload) == 0 {
		return nil
	}
	if payload[len(payload)-1] != '\n' {
		payload = append(payload, '\n')
	}
	_, err := writer.Write(payload)
	return err
}

// DefaultProcessor returns a Processor that handles json/ndjson only, with
// the built-in select + envelope hooks. Products that need table rendering
// should call pkg/integrations/formatting.DefaultProcessor() instead.
func DefaultProcessor() *frameworkoutput.Processor {
	return &frameworkoutput.Processor{
		Hooks: []frameworkoutput.OutputHook{
			frameworkoutput.FieldSelectorHook{},
			frameworkoutput.EnvelopeHook{},
		},
		Formatter: Standard{},
		Writer:    Stdio{},
	}
}
