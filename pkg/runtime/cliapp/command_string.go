// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cliapp

import "strings"

// ParseCommandStringArgs tokenizes a command string using the quoting rules
// shared by the programmatic command-string entry points.
//
// Double quotes group values containing spaces. The escape sequences \n, \",
// \\ and "\ " are decoded; unknown escape sequences keep their backslash so
// values such as Windows paths are not silently corrupted.
func ParseCommandStringArgs(cmdStr string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false

	for index := 0; index < len(cmdStr); index++ {
		char := cmdStr[index]

		if char == '\\' {
			if index+1 >= len(cmdStr) {
				current.WriteByte(char)
				continue
			}

			next := cmdStr[index+1]
			switch next {
			case 'n':
				current.WriteByte('\n')
				index++
			case '"', '\\', ' ':
				current.WriteByte(next)
				index++
			default:
				current.WriteByte(char)
			}
			continue
		}

		if char == '"' {
			inQuotes = !inQuotes
			continue
		}

		if char == ' ' && !inQuotes {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(char)
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
