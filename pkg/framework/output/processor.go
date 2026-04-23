// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	frameworkadapter "github.com/larksuite/meegle-cli/pkg/framework/adapter"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

type Processor struct {
	Hooks     []OutputHook
	Formatter Formatter
	Writer    IOWriter
	Options   *FormatOptions
}

// RenderError formats err as the unified error envelope and returns the
// rendered bytes for stderr. The hook chain is intentionally bypassed —
// hooks are designed for success data and assume a non-error payload. The
// formatter receives FormatOptions.ErrorEnvelope=true so downstream (e.g.
// CompositeFormatter) can switch table mode to render the inner error record
// as KV and skip wrapper-unwrapping. Returns (nil, nil) when err is nil.
//
// Callers (usually App.Execute) should write the returned bytes to stderr and
// propagate the original err so the process exit code stays correct.
func (p *Processor) RenderError(state *Context, err error) ([]byte, error) {
	if err == nil {
		return nil, nil
	}
	options := p.Options
	if options == nil {
		options = optionsFromContext(state)
	}
	meta := map[string]any{}
	if state != nil && state.Result != nil {
		for k, v := range state.Result.Metadata {
			meta[k] = v
		}
	}
	payload := BuildErrorEnvelope(err, meta)
	errOpts := *options
	errOpts.ErrorEnvelope = true
	if p.Formatter != nil {
		return p.Formatter.Format(payload, &errOpts)
	}
	return json.Marshal(payload)
}

func (p *Processor) Process(ctx context.Context, state *Context) error {
	_ = ctx
	if p == nil || state == nil || state.Result == nil {
		return nil
	}
	options := p.Options
	if options == nil {
		options = optionsFromContext(state)
	}
	state.Options = options
	data := state.Result.Data
	for _, hook := range p.Hooks {
		if hook == nil {
			continue
		}
		next, err := hook.Process(state, data)
		if err != nil {
			return err
		}
		data = next
	}
	state.Result.Data = data
	if state.Input != nil && state.Input.IOMode == frameworkadapter.IOModeProgrammatic {
		return nil
	}
	if p.Writer == nil {
		return nil
	}
	var payload []byte
	var err error
	if p.Formatter != nil {
		payload, err = p.Formatter.Format(data, options)
	} else {
		payload, err = json.Marshal(data)
	}
	if err != nil {
		return frameworkerrors.New(frameworkerrors.CategoryInternal, frameworkerrors.CodeInternal, err.Error())
	}
	return p.Writer.Write(state, payload)
}

func optionsFromContext(state *Context) *FormatOptions {
	options := &FormatOptions{Mode: "json"}
	if state == nil || state.Parsed == nil {
		return options
	}
	if value, ok := state.Parsed.Flags["format"]; ok {
		options.Mode = strings.TrimSpace(fmt.Sprint(value))
	}
	if value, ok := state.Parsed.Flags["select"]; ok {
		options.Select = parseSelectExpr(strings.TrimSpace(fmt.Sprint(value)))
	}
	if value, ok := state.Parsed.Flags["envelope"]; ok {
		if b, ok := value.(bool); ok {
			options.Envelope = b
		}
	}
	if value, ok := state.Parsed.Flags["verbose"]; ok {
		if verbose, ok := value.(bool); ok {
			options.Verbose = verbose
		}
	}
	return options
}

func parseSelectExpr(expr string) []string {
	if expr == "" {
		return nil
	}
	parts := strings.Split(expr, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
