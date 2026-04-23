// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package prompt provides minimal arrow-key driven interactive prompts
// without pulling in a heavy TUI framework. Falls back to numeric input
// when stdin is not a terminal.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrInterrupted is returned when the user cancels with Ctrl-C or Esc.
var ErrInterrupted = errors.New("prompt interrupted")

// SelectOne shows an arrow-key driven menu and returns the index of the
// selected option. When stdin/stdout is not a TTY it falls back to a
// numeric prompt that reads a full line.
func SelectOne(label string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, errors.New("no options to select")
	}

	in, out := os.Stdin, os.Stdout
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return selectNumericFallback(in, out, label, options)
	}
	return selectInteractive(in, out, label, options)
}

// ReadLine prints the prompt and reads a full line from stdin (trimmed).
// Unlike fmt.Scan, it preserves spaces and the rest of the line.
func ReadLine(label string) (string, error) {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func selectNumericFallback(in *os.File, out io.Writer, label string, options []string) (int, error) {
	fmt.Fprintln(out, label)
	for i, opt := range options {
		fmt.Fprintf(out, "    %d. %s\n", i+1, opt)
	}
	fmt.Fprint(out, "\n  Enter a number: ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return -1, err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(options) {
		return -1, fmt.Errorf("invalid choice: %q", strings.TrimSpace(line))
	}
	return choice - 1, nil
}

func selectInteractive(in *os.File, out *os.File, label string, options []string) (int, error) {
	oldState, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return selectNumericFallback(in, out, label, options)
	}
	defer func() { _ = term.Restore(int(in.Fd()), oldState) }()

	hideCursor(out)
	defer showCursor(out)

	cursor := 0
	first := true

	draw := func() {
		if !first {
			// Move cursor back up to the label line.
			fmt.Fprintf(out, "\x1b[%dA", len(options)+1)
		}
		first = false
		clearLine(out)
		fmt.Fprintf(out, "%s\r\n", label)
		for i, opt := range options {
			clearLine(out)
			if i == cursor {
				fmt.Fprintf(out, "  \x1b[36m▸ %s\x1b[0m\r\n", opt)
			} else {
				fmt.Fprintf(out, "    %s\r\n", opt)
			}
		}
	}

	draw()

	buf := make([]byte, 8)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return -1, err
		}
		key := buf[:n]

		switch {
		case n == 1 && key[0] == 0x03: // Ctrl-C
			return -1, ErrInterrupted
		case n == 1 && key[0] == 0x1b: // Esc alone
			return -1, ErrInterrupted
		case n == 1 && (key[0] == '\r' || key[0] == '\n'):
			return cursor, nil
		case n >= 3 && key[0] == 0x1b && key[1] == '[':
			switch key[2] {
			case 'A':
				cursor = (cursor - 1 + len(options)) % len(options)
				draw()
			case 'B':
				cursor = (cursor + 1) % len(options)
				draw()
			}
		case n == 1 && (key[0] == 'k' || key[0] == 'K'):
			cursor = (cursor - 1 + len(options)) % len(options)
			draw()
		case n == 1 && (key[0] == 'j' || key[0] == 'J'):
			cursor = (cursor + 1) % len(options)
			draw()
		}
	}
}

func clearLine(out io.Writer)  { fmt.Fprint(out, "\r\x1b[2K") }
func hideCursor(out io.Writer) { fmt.Fprint(out, "\x1b[?25l") }
func showCursor(out io.Writer) { fmt.Fprint(out, "\x1b[?25h") }
