// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"context"
	"fmt"
	"time"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
)

// Emit dispatches one process lifecycle event. Shutdown is bounded to two
// seconds so an extension cannot hold the CLI process open indefinitely.
func (r *Runtime) Emit(ctx context.Context, event platformapi.LifecycleEvent, runErr error) error {
	return r.emitWithLifecycleTimeouts(ctx, event, runErr, DefaultCallbackTimeout, 2*time.Second)
}

func (r *Runtime) emitWithShutdownTimeout(ctx context.Context, event platformapi.LifecycleEvent, runErr error, shutdownTimeout time.Duration) error {
	return r.emitWithLifecycleTimeouts(ctx, event, runErr, DefaultCallbackTimeout, shutdownTimeout)
}

func (r *Runtime) emitWithLifecycleTimeouts(ctx context.Context, event platformapi.LifecycleEvent, runErr error, startupTimeout, shutdownTimeout time.Duration) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if event == platformapi.Shutdown {
		var cancel context.CancelFunc
		// Command cancellation must not erase the cleanup window. WithoutCancel
		// retains request-scoped values while giving shutdown its own budget.
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
	}
	indices := make([]int, 0, len(r.lifecycles))
	for index := range r.lifecycles {
		indices = append(indices, index)
	}
	if event == platformapi.Shutdown {
		for left, right := 0, len(indices)-1; left < right; left, right = left+1, right-1 {
			indices[left], indices[right] = indices[right], indices[left]
		}
	}
	var firstFailure error
	for _, index := range indices {
		entry := r.lifecycles[index]
		if entry.event != event || entry.handler == nil {
			continue
		}
		if ctx.Err() != nil {
			err := runtimeFailure(entry.name, "lifecycle", ctx.Err())
			if entry.policy == platformapi.FailOpen {
				logHookFailure(entry.name, err)
				continue
			}
			if firstFailure == nil {
				firstFailure = err
			}
			continue
		}
		// A callback is an untrusted boundary. Give every callback its own event
		// object so an overdue handler cannot race with later handlers through a
		// shared mutable pointer.
		lifecycle := &platformapi.LifecycleContext{Event: event, Err: runErr}
		var err error
		if event == platformapi.Startup {
			err = callLifecycleWithTimeout(ctx, startupTimeout, entry, lifecycle)
		} else {
			err = callLifecycleBounded(ctx, entry, lifecycle)
		}
		if err != nil {
			if entry.policy == platformapi.FailOpen {
				logHookFailure(entry.name, err)
				continue
			}
			if event == platformapi.Shutdown {
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			return err
		}
	}
	return firstFailure
}

func callLifecycleWithTimeout(ctx context.Context, timeout time.Duration, entry lifecycleEntry, lifecycle *platformapi.LifecycleContext) error {
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return callLifecycleBounded(hookCtx, entry, lifecycle)
}

func callLifecycleBounded(ctx context.Context, entry lifecycleEntry, lifecycle *platformapi.LifecycleContext) error {
	result := make(chan error, 1)
	go func() {
		result <- callLifecycle(ctx, entry, lifecycle)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return runtimeFailure(entry.name, "lifecycle", ctx.Err())
	}
}

func callLifecycle(ctx context.Context, entry lifecycleEntry, lifecycle *platformapi.LifecycleContext) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = runtimeFailure(entry.name, "lifecycle", panicCause(recovered))
		}
	}()
	if entry.handler == nil {
		return runtimeFailure(entry.name, "lifecycle", fmt.Errorf("lifecycle callback is nil"))
	}
	if err := entry.handler(ctx, lifecycle); err != nil {
		return runtimeFailure(entry.name, "lifecycle", err)
	}
	return nil
}
