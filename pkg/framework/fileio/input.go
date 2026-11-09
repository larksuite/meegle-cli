// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package fileio implements explicit local JSON input and result exports.
package fileio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/larksuite/meegle-cli/pkg/framework/jsonvalue"
)

// MaxInputBytes bounds the new input-file option independently of server limits.
const MaxInputBytes = 32 << 20

// ReadInput reads exactly one UTF-8 JSON object from a regular local file.
func ReadInput(ctx context.Context, reference string) (map[string]any, error) {
	if !strings.HasPrefix(reference, "@") {
		return nil, fmt.Errorf("--input-file expects @path")
	}
	path := strings.TrimPrefix(reference, "@")
	if err := localPath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular file")
	}
	if info.Size() > MaxInputBytes {
		return nil, fmt.Errorf("input exceeds %d-byte limit", MaxInputBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("input is no longer a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: f}, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("input exceeds %d-byte limit", MaxInputBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("input must be UTF-8 JSON")
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("input contains a UTF-8 BOM; save the file as UTF-8 without BOM")
	}
	var params map[string]any
	if err := jsonvalue.Unmarshal(data, &params); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, fmt.Errorf("input is not valid JSON at byte %d: %w", syntax.Offset, err)
		}
		return nil, fmt.Errorf("input must contain exactly one JSON object: %w", err)
	}
	if params == nil {
		return nil, fmt.Errorf("input must contain exactly one JSON object")
	}
	return params, ctx.Err()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func localPath(path string) error {
	if strings.TrimSpace(path) == "" || path == "-" || strings.Contains(path, "://") {
		return fmt.Errorf("expected a non-empty local file path (not a URL or stdin/stdout)")
	}
	return nil
}

func outputPath(path string) (string, error) {
	if err := localPath(path); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("output parent must be an existing directory")
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	if _, err := os.Lstat(abs); !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("output path already exists: %s", abs)
		}
		return "", err
	}
	return abs, nil
}
