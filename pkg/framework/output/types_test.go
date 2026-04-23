// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

import (
	"reflect"
	"testing"
)

func TestFormatOptions_ZeroValue(t *testing.T) {
	var opts FormatOptions
	if opts.Mode != "" || opts.Envelope || opts.Verbose || opts.Select != nil {
		t.Fatalf("zero value should be empty: %+v", opts)
	}
}

func TestFormatOptions_SelectIsSlice(t *testing.T) {
	opts := FormatOptions{Select: []string{"id", "name"}}
	if !reflect.DeepEqual(opts.Select, []string{"id", "name"}) {
		t.Fatalf("got %v", opts.Select)
	}
}
