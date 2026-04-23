// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package adapter

type StructuredAdapter struct {
	Request      *StructuredCommandRequest
	Materializer InputMaterializer
}

func (a StructuredAdapter) Adapt() (*RawInput, error) {
	if a.Request == nil {
		return &RawInput{IOMode: IOModeStructured}, nil
	}
	materializer := a.Materializer
	if materializer == nil {
		materializer = DefaultMaterializer{}
	}
	path := append([]string(nil), a.Request.Path...)
	flagArgs := materializer.Materialize(a.Request.Params)
	return &RawInput{
		Args:   append(path, flagArgs...),
		IOMode: IOModeStructured,
	}, nil
}
