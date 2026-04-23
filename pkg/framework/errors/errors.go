// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import (
	stderrs "errors"
	"fmt"
	"strings"
)

type Category int

const (
	CategoryUser Category = iota
	CategoryServer
	CategoryAuth
	CategoryConfig
	CategoryInternal
)

type CLIError struct {
	Category Category
	Code     string
	Message  string
	Detail   string
	Cause    error
}

func (e *CLIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *CLIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CLIError) ExitCode() int {
	if e == nil {
		return 0
	}
	return int(e.Category) + 1
}

// IsRetryable reports whether retrying the operation that produced this error
// has a reasonable chance of succeeding. Only CategoryServer errors (transient
// network / backend 5xx) are considered retryable by default; user, auth,
// config and internal errors are not. Exposed on the envelope payload as the
// `retryable` field so Agents can decide whether to re-issue the command.
func (e *CLIError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.Category == CategoryServer
}

func New(category Category, code, message string) *CLIError {
	return &CLIError{Category: category, Code: code, Message: message}
}

func As(err error) *CLIError {
	if err == nil {
		return nil
	}
	var cliErr *CLIError
	if stderrs.As(err, &cliErr) {
		return cliErr
	}
	return nil
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if cliErr := As(err); cliErr != nil {
		return cliErr.ExitCode()
	}
	return 1
}

func Render(err error, verbose bool) string {
	if err == nil {
		return ""
	}
	cliErr := As(err)
	if cliErr == nil {
		return err.Error()
	}
	message := strings.TrimSpace(cliErr.Message)
	if message == "" {
		message = "unexpected error"
	}
	if !verbose {
		return message
	}
	parts := []string{message}
	if cliErr.Code != "" {
		parts = append(parts, fmt.Sprintf("code: %s", cliErr.Code))
	}
	if detail := strings.TrimSpace(cliErr.Detail); detail != "" {
		parts = append(parts, fmt.Sprintf("detail: %s", detail))
	}
	if cliErr.Cause != nil {
		parts = append(parts, fmt.Sprintf("cause: %v", cliErr.Cause))
	}
	return strings.Join(parts, "\n")
}
