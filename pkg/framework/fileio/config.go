// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package fileio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Output selects fields for one JSON file. Paths are canonicalized by ParseOutputs.
type Output struct {
	Path   string   `json:"path"`
	Fields []string `json:"fields"`
}

// UnmarshalJSON rejects ambiguous duplicate and case-variant option names.
func (o *Output) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("output configuration must be an object")
	}
	seen := make(map[string]bool)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate or invalid output property %q", key)
		}
		seen[key] = true
		switch key {
		case "path":
			err = d.Decode(&o.Path)
		case "fields":
			err = d.Decode(&o.Fields)
		default:
			return fmt.Errorf("unknown output property %q", key)
		}
		if err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

// ParseOutputs accepts one object/array, or repeated objects, and preflights all targets.
func ParseOutputs(raw []string) ([]Output, error) {
	var outputs []Output
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "[") {
			if len(raw) != 1 {
				return nil, fmt.Errorf("--output-file accepts one array or repeated objects, not both")
			}
			if err := decodeConfig(value, &outputs); err != nil {
				return nil, err
			}
		} else {
			var item Output
			if err := decodeConfig(value, &item); err != nil {
				return nil, err
			}
			outputs = append(outputs, item)
		}
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("--output-file requires a non-empty object array")
	}
	return validateOutputs(outputs)
}

func decodeConfig(raw string, target any) error {
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("invalid --output-file JSON: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("--output-file expects exactly one JSON value")
	}
	return nil
}

func validateOutputs(outputs []Output) ([]Output, error) {
	targets := make(map[string]bool)
	fields := make(map[string]int)
	for i := range outputs {
		item := &outputs[i]
		path, err := outputPath(item.Path)
		if err != nil {
			return nil, fmt.Errorf("--output-file %q: %w", item.Path, err)
		}
		if targets[path] {
			return nil, fmt.Errorf("duplicate output path: %s", path)
		}
		targets[path] = true
		item.Path = path
		if len(item.Fields) == 0 {
			return nil, fmt.Errorf("--output-file fields must be a non-empty string array")
		}
		unique := make([]string, 0, len(item.Fields))
		for _, field := range item.Fields {
			if err := validateField(field, i, fields); err != nil {
				return nil, err
			}
			if _, exists := fields[field]; !exists {
				unique = append(unique, field)
				fields[field] = i
			}
		}
		item.Fields = unique
	}
	// Probe only after every configuration is valid, once per canonical parent.
	parents := make(map[string]bool)
	for _, item := range outputs {
		dir := filepath.Dir(item.Path)
		if parents[dir] {
			continue
		}
		if err := probeOutputDir(dir, diskOperations()); err != nil {
			return nil, fmt.Errorf("--output-file %q: directory preflight failed: %w", item.Path, err)
		}
		parents[dir] = true
	}
	return outputs, nil
}

func validateField(field string, index int, fields map[string]int) error {
	if strings.TrimSpace(field) != field || strings.ContainsAny(field, "[]*\\") {
		return fmt.Errorf("unsupported field path %q", field)
	}
	for _, part := range strings.Split(field, ".") {
		if part == "" {
			return fmt.Errorf("field path must contain non-empty object keys")
		}
	}
	for previous, owner := range fields {
		if previous == field && owner == index {
			continue
		}
		if previous == field || strings.HasPrefix(previous, field+".") || strings.HasPrefix(field, previous+".") {
			return fmt.Errorf("overlapping output fields %q and %q", previous, field)
		}
	}
	return nil
}
