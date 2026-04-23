// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

// EnvelopeHook wraps the result payload in {data, meta, error} when
// --envelope is set. It only fires on the success path; the error-path
// envelope (which must appear regardless of --envelope per the design spec
// §"错误处理契约") is built by BuildErrorEnvelope and emitted through
// Processor.RenderError — see processor.go and pkg/runtime/cliapp/app.go
// (executeParsed → renderErrorEnvelope) for the full flow. Keeping success
// and error envelope logic separate means EnvelopeHook never has to reason
// about nil data or partially-populated state.Result.
type EnvelopeHook struct{}

func (h EnvelopeHook) Name() string { return "envelope" }

func (h EnvelopeHook) Process(ctx *Context, data any) (any, error) {
	if ctx == nil || ctx.Options == nil || !ctx.Options.Envelope {
		return data, nil
	}
	meta := map[string]any{}
	if ctx.Result != nil {
		for k, v := range ctx.Result.Metadata {
			meta[k] = v
		}
	}
	return map[string]any{
		"data":  data,
		"meta":  meta,
		"error": nil,
	}, nil
}
