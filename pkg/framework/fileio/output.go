// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package fileio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/jsonvalue"
)

// Saved describes a fully published file, including on partial failure.
type Saved struct {
	Path   string   `json:"path"`
	Fields []string `json:"fields"`
	Format string   `json:"format"`
}

// OutputError preserves business completion and published files without the payload.
type OutputError struct {
	Stage             string
	Path              string
	Files             []Saved
	Cause             error
	BusinessExecution string
}

func (e *OutputError) Error() string {
	return fmt.Sprintf("file output failed at %s (%s): %s; business execution %s, do not automatically retry", e.Stage, e.Path, frameworkerrors.SafeMessage(e.Cause, "output failure"), e.businessExecution())
}

// ErrorPayload implements the standard output error contract.
func (e *OutputError) ErrorPayload() map[string]any {
	return map[string]any{"code": "FILE_OUTPUT_FAILED", "message": e.Error(), "retryable": false}
}

// ErrorMetadata is also available to SDK callers that receive an error.
func (e *OutputError) ErrorMetadata() map[string]any {
	files := e.Files
	if files == nil {
		files = []Saved{}
	}
	return map[string]any{"business_execution": e.businessExecution(), "output_files": files, "output_failure": map[string]any{"stage": e.Stage, "path": e.Path}}
}

func (e *OutputError) businessExecution() string {
	if e.BusinessExecution != "" {
		return e.BusinessExecution
	}
	return "completed"
}

// Unwrap preserves cancellation and OS errors through the framework's guarded traversal.
func (e *OutputError) Unwrap() error { return frameworkerrors.GuardCause(e.Cause) }

// Export validates every selection before writing, then publishes files individually.
// Remaining data is copied only along selected paths; source maps are never modified.
func Export(ctx context.Context, data any, outputs []Output) (remaining any, saved []Saved, err error) {
	stage, path := "select", ""
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("result serialization panicked")
		}
		if err != nil {
			err = &OutputError{Stage: stage, Path: path, Files: saved, Cause: err}
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	root, err := object(data)
	if err != nil {
		return nil, nil, err
	}
	rest := root
	payloads := make([]map[string]any, len(outputs))
	for i, output := range outputs {
		path = output.Path
		payloads[i] = make(map[string]any)
		for _, field := range output.Fields {
			var selected any
			rest, selected, err = removePath(rest, strings.Split(field, "."))
			if err != nil {
				return nil, nil, fmt.Errorf("field %q: %w", field, err)
			}
			insertPath(payloads[i], strings.Split(field, "."), selected)
		}
	}
	saved = make([]Saved, 0, len(outputs))
	for i, output := range outputs {
		path = output.Path
		var committed bool
		committed, stage, err = saveJSON(ctx, path, payloads[i], diskOperations())
		if committed {
			saved = append(saved, Saved{Path: path, Fields: append([]string(nil), output.Fields...), Format: "json"})
		}
		if err != nil {
			return nil, saved, err
		}
	}
	return rest, saved, nil
}

func object(value any) (map[string]any, error) {
	if m, ok := value.(map[string]any); ok {
		if m == nil {
			return nil, fmt.Errorf("expected a JSON object, got null")
		}
		return m, nil
	}
	var data []byte
	var err error
	switch v := value.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	default:
		data, err = json.Marshal(value)
	}
	var m map[string]any
	if err != nil || jsonvalue.Unmarshal(data, &m) != nil || m == nil {
		return nil, fmt.Errorf("field paths require a JSON object")
	}
	return m, nil
}

func removePath(source map[string]any, parts []string) (map[string]any, any, error) {
	value, found := source[parts[0]]
	if !found {
		return nil, nil, fmt.Errorf("field does not exist")
	}
	copyMap := make(map[string]any, len(source))
	for k, v := range source {
		copyMap[k] = v
	}
	if len(parts) == 1 {
		delete(copyMap, parts[0])
		return copyMap, value, nil
	}
	child, err := object(value)
	if err != nil {
		return nil, nil, err
	}
	remaining, selected, err := removePath(child, parts[1:])
	copyMap[parts[0]] = remaining
	return copyMap, selected, err
}

func insertPath(target map[string]any, parts []string, value any) {
	for _, part := range parts[:len(parts)-1] {
		next, ok := target[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			target[part] = next
		}
		target = next
	}
	target[parts[len(parts)-1]] = value
}

// File operations are per-call so failure tests never replace process globals.
type temporaryFile interface {
	io.WriteCloser
	Name() string
}

type fileOperations struct {
	create func(string, string) (temporaryFile, error)
	link   func(string, string) error
	remove func(string) error
}

func diskOperations() fileOperations {
	return fileOperations{
		create: func(dir, pattern string) (temporaryFile, error) { return os.CreateTemp(dir, pattern) },
		link:   os.Link,
		remove: os.Remove,
	}
}

// Probe the same directory and operations used by exports without occupying the
// requested destination. A successful probe cannot prevent later OS failures.
func probeOutputDir(dir string, ops fileOperations) (err error) {
	f, err := ops.create(dir, ".meegle-output-probe-*")
	if err != nil {
		return err
	}
	name, link := f.Name(), f.Name()+".link"
	closed, linked := false, false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
		if linked {
			err = errors.Join(err, ops.remove(link))
		}
		err = errors.Join(err, ops.remove(name))
	}()
	if n, writeErr := f.Write([]byte("{}\n")); writeErr != nil {
		return writeErr
	} else if n != 3 {
		return io.ErrShortWrite
	}
	err = f.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = ops.link(name, link); err != nil {
		return err
	}
	linked = true
	return nil
}

func saveJSON(ctx context.Context, path string, value any, ops fileOperations) (committed bool, stage string, err error) {
	stage = "create"
	if err = ctx.Err(); err != nil {
		return
	}
	f, err := ops.create(filepath.Dir(path), ".meegle-output-*")
	if err != nil {
		return false, stage, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		_ = ops.remove(f.Name())
		if recover() != nil {
			err = fmt.Errorf("file serialization panicked")
		}
	}()
	stage = "write"
	encoder := json.NewEncoder(contextWriter{ctx: ctx, writer: f})
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(value); err != nil {
		return
	}
	stage = "close"
	err = f.Close()
	closed = true
	if err != nil {
		return
	}
	stage = "publish"
	if err = ctx.Err(); err != nil {
		return
	}
	// A hard link publishes the complete same-directory file atomically and
	// fails if the target exists. No racy stat-then-rename overwrite is possible.
	if err = ops.link(f.Name(), path); err != nil {
		return
	}
	committed = true
	stage = "cleanup"
	err = ops.remove(f.Name())
	return
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if err := w.ctx.Err(); err != nil {
			return total, err
		}
		chunk := len(p)
		if chunk > 64<<10 {
			chunk = 64 << 10
		}
		n, err := w.writer.Write(p[:chunk])
		total += n
		if err != nil {
			return total, err
		}
		if n != chunk {
			return total, io.ErrShortWrite
		}
		p = p[n:]
	}
	return total, nil
}
