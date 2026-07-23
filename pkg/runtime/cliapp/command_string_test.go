// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import (
	"reflect"
	"testing"
)

func TestParseCommandStringArgs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "simple command",
			raw:  "workitem create --key PROJ",
			want: []string{"workitem", "create", "--key", "PROJ"},
		},
		{
			name: "quoted value with spaces",
			raw:  `workitem create --summary "Test Issue"`,
			want: []string{"workitem", "create", "--summary", "Test Issue"},
		},
		{
			name: "escaped quotes",
			raw:  `workitem create --text "say \"hello\""`,
			want: []string{"workitem", "create", "--text", `say "hello"`},
		},
		{
			name: "escaped newlines",
			raw:  `comment add --content "first line\n\nsecond line"`,
			want: []string{"comment", "add", "--content", "first line\n\nsecond line"},
		},
		{
			name: "unknown escape remains intact",
			raw:  `workitem create --path "C:\docs\file"`,
			want: []string{"workitem", "create", "--path", `C:\docs\file`},
		},
		{
			name: "escaped backslash keeps literal slash n",
			raw:  `comment add --content "first line\\nsecond line"`,
			want: []string{"comment", "add", "--content", `first line\nsecond line`},
		},
		{
			name: "trailing backslash remains intact",
			raw:  `workitem create --path C:\`,
			want: []string{"workitem", "create", "--path", `C:\`},
		},
		{
			name: "escaped unquoted space",
			raw:  `workitem create --summary Test\ Issue`,
			want: []string{"workitem", "create", "--summary", "Test Issue"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseCommandStringArgs(test.raw)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseCommandStringArgs(%q) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}
