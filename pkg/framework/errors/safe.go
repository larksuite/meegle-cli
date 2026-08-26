// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

import stderrors "errors"

// SafeIs applies errors.Is without allowing a custom error implementation to
// propagate a panic from Is, As, or Unwrap.
func SafeIs(err, target error) (matched bool) {
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	return stderrors.Is(err, target)
}

// SafeAs applies errors.As without allowing a custom error implementation to
// propagate a panic from Is, As, or Unwrap.
func SafeAs(err error, target any) (matched bool) {
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	return stderrors.As(err, target)
}

// SafeMessage returns err.Error without allowing an untrusted Error method to
// panic at the final formatting boundary.
func SafeMessage(err error, fallback string) (message string) {
	message = fallback
	if err == nil {
		return message
	}
	defer func() { _ = recover() }()
	return err.Error()
}

// GuardCause keeps cause available to errors.Is/As while ensuring all
// traversal happens inside SafeIs/SafeAs. The returned error intentionally has
// no Unwrap method, so callers cannot walk directly into an untrusted cause.
func GuardCause(cause error) error {
	if cause == nil {
		return nil
	}
	if _, guarded := cause.(*guardedCause); guarded {
		return cause
	}
	return &guardedCause{cause: cause}
}

type guardedCause struct{ cause error }

func (*guardedCause) Error() string { return "guarded error cause" }

func (e *guardedCause) Is(target error) bool {
	return e != nil && e.cause != nil && SafeIs(e.cause, target)
}

func (e *guardedCause) As(target any) bool {
	return e != nil && e.cause != nil && SafeAs(e.cause, target)
}
