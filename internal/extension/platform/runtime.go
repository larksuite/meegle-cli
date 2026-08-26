// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package platform installs and executes public platform extensions.
package platform

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

const wrappedAnnotation = "meegle.extension.wrapped"

type observerEntry struct {
	name     string
	when     platformapi.When
	selector platformapi.Selector
	observer platformapi.Observer
	policy   platformapi.FailurePolicy
}

type wrapperEntry struct {
	name     string
	selector platformapi.Selector
	wrapper  platformapi.Wrapper
	policy   platformapi.FailurePolicy
}

type lifecycleEntry struct {
	name    string
	event   platformapi.LifecycleEvent
	handler platformapi.LifecycleHandler
	policy  platformapi.FailurePolicy
}

type ruleEntry struct {
	plugin string
	rule   *platformapi.Rule
}

// RuntimeFailure is the non-secret boundary error for an extension callback.
// Cause stays available to errors.Is/As but is deliberately excluded from the
// public message and structured payload because third-party callbacks may put
// credentials or request data in returned errors and panic values.
type RuntimeFailure struct {
	HookName string
	Stage    string
	Cause    error
}

type wrapperInvocationTracker struct {
	mu        sync.Mutex
	ready     *sync.Cond
	hookName  string
	closed    bool
	called    bool
	inFlight  int
	errs      []error
	violation error
}

func newWrapperInvocationTracker(hookName string) *wrapperInvocationTracker {
	tracker := &wrapperInvocationTracker{hookName: hookName}
	tracker.ready = sync.NewCond(&tracker.mu)
	return tracker
}

func (t *wrapperInvocationTracker) call(ctx context.Context, next platformapi.Handler, invocation platformapi.Invocation) (err error) {
	t.mu.Lock()
	if t.closed || t.called {
		violation := runtimeFailure(t.hookName, "wrapper", fmt.Errorf("wrapper next must be called synchronously at most once"))
		if t.violation == nil {
			t.violation = violation
		}
		t.mu.Unlock()
		return violation
	}
	t.called = true
	t.inFlight++
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		if err != nil {
			t.errs = append(t.errs, err)
		}
		t.inFlight--
		t.ready.Broadcast()
		t.mu.Unlock()
	}()
	return next(ctx, invocation)
}

func (t *wrapperInvocationTracker) closeAndWait() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.closed = true
	for t.inFlight > 0 {
		t.ready.Wait()
	}
	called := t.called
	violation := t.violation
	var downstreamErr error
	if len(t.errs) > 0 {
		downstreamErr = t.errs[0]
	}
	t.mu.Unlock()
	if violation != nil {
		return violation
	}
	if !called {
		return runtimeFailure(t.hookName, "wrapper", fmt.Errorf("wrapper next must be called synchronously before returning success"))
	}
	return downstreamErr
}

func (t *wrapperInvocationTracker) matching(err error) error {
	if t == nil || err == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, downstreamErr := range t.errs {
		if frameworkerrors.SafeIs(err, downstreamErr) {
			return downstreamErr
		}
	}
	return nil
}

type safeDelegatedError struct {
	public error
	cause  error
}

func (e *safeDelegatedError) Error() (message string) {
	message = "command failed"
	defer func() { _ = recover() }()
	if e != nil && e.public != nil {
		message = e.public.Error()
	}
	return message
}

func (e *safeDelegatedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.cause)
}

func (e *safeDelegatedError) ErrorPayload() (record map[string]any) {
	record = map[string]any{"code": "CLIENT_COMMAND_RUNTIME_FAILED", "message": "command failed", "retryable": false}
	defer func() {
		if recover() != nil {
			record = map[string]any{"code": "CLIENT_COMMAND_RUNTIME_FAILED", "message": "command failed", "retryable": false}
		}
	}()
	if e == nil || e.public == nil {
		return record
	}
	delegated := frameworkoutput.BuildErrorRecord(e.public)
	if delegated["code"] == nil || delegated["message"] == nil || delegated["retryable"] == nil {
		return record
	}
	return delegated
}

func (e *RuntimeFailure) Error() string {
	if e == nil {
		return "CLI extension runtime failed"
	}
	return fmt.Sprintf("CLI extension hook %q failed during %s", e.HookName, e.Stage)
}

func (e *RuntimeFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.Cause)
}

func (e *RuntimeFailure) ErrorPayload() map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"code":      "CLIENT_EXTENSION_RUNTIME_FAILED",
		"message":   e.Error(),
		"retryable": false,
		"detail": map[string]any{
			"hook":  e.HookName,
			"stage": e.Stage,
		},
	}
}

func runtimeFailure(hookName, stage string, cause error) error {
	return &RuntimeFailure{HookName: hookName, Stage: stage, Cause: cause}
}

func panicCause(recovered any) error {
	if err, ok := recovered.(error); ok {
		return &recoveredPanicError{cause: err}
	}
	return fmt.Errorf("panic: %v", recovered)
}

type recoveredPanicError struct{ cause error }

func (*recoveredPanicError) Error() string { return "callback panicked with an error" }
func (e *recoveredPanicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return frameworkerrors.GuardCause(e.cause)
}

// Runtime is an immutable snapshot of successfully installed plugins.
type Runtime struct {
	observers         []observerEntry
	wrappers          []wrapperEntry
	lifecycles        []lifecycleEntry
	rules             []ruleEntry
	restrictor        string
	pluginDiagnostics []PluginDiagnostic
}

// Apply wraps every executable command in the final Cobra tree. It is safe to
// call repeatedly on the same tree and on newly rebuilt trees.
func (r *Runtime) Apply(root *cobra.Command) {
	if r == nil || root == nil {
		return
	}
	applyCommand := func(command *cobra.Command) {
		if command == nil || command.RunE == nil {
			return
		}
		if command.Annotations == nil {
			command.Annotations = map[string]string{}
		}
		if command.Annotations[wrappedAnnotation] == "1" {
			return
		}
		command.Annotations[wrappedAnnotation] = "1"
		original := command.RunE
		command.RunE = r.wrapRunE(command, original)
	}
	commands := allCommands(root)
	applyCommand(root)
	for _, command := range commands {
		applyCommand(command)
	}
	if len(r.rules) == 0 {
		return
	}
	for _, command := range commands {
		if command.RunE != nil && r.denial(commandView{command: command}) != nil {
			command.Hidden = true
		}
	}
	for index := len(commands) - 1; index >= 0; index-- {
		command := commands[index]
		children := command.Commands()
		if command.RunE != nil || len(children) == 0 {
			continue
		}
		allHidden := true
		for _, child := range children {
			if !child.Hidden {
				allHidden = false
				break
			}
		}
		if allHidden {
			command.Hidden = true
		}
	}
}

func allCommands(root *cobra.Command) []*cobra.Command {
	var commands []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			commands = append(commands, child)
			walk(child)
		}
	}
	walk(root)
	return commands
}

func (r *Runtime) wrapRunE(command *cobra.Command, original func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		invocation := &invocation{
			command: commandView{command: command},
			args:    append([]string(nil), args...),
			started: time.Now(),
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		if err := r.runObservers(ctx, platformapi.Before, invocation); err != nil {
			invocation.err = err
			_ = r.runObservers(ctx, platformapi.After, invocation)
			return err
		}
		if denial := r.denial(invocation.command); denial != nil {
			invocation.denied = true
			invocation.layer = denial.Layer
			invocation.source = denial.PolicySource
			invocation.err = denial
			_ = r.runObservers(ctx, platformapi.After, invocation)
			return denial
		}

		handler := platformapi.Handler(func(handlerCtx context.Context, _ platformapi.Invocation) error {
			if handlerCtx == nil {
				handlerCtx = ctx
			}
			previous := cmd.Context()
			cmd.SetContext(handlerCtx)
			if previous != nil {
				defer cmd.SetContext(previous)
			}
			return callCommand(commandView{command: command}.Path(), func() error { return original(cmd, args) })
		})
		for index := len(r.wrappers) - 1; index >= 0; index-- {
			entry := r.wrappers[index]
			matched, matchErr := callSelector(entry.name, entry.selector, invocation.command)
			if matchErr != nil {
				if entry.policy == platformapi.FailOpen {
					logHookFailure(entry.name, matchErr)
					continue
				}
				invocation.err = matchErr
				_ = r.runObservers(ctx, platformapi.After, invocation)
				return matchErr
			}
			if matched {
				wrapped, err := buildWrapper(entry, handler)
				if err != nil {
					invocation.err = err
					_ = r.runObservers(ctx, platformapi.After, invocation)
					return err
				}
				handler = wrapped
			}
		}
		invocation.err = handler(ctx, invocation)

		if err := r.runObservers(ctx, platformapi.After, invocation); err != nil && invocation.err == nil {
			invocation.err = err
		}
		return invocation.err
	}
}

func (r *Runtime) runObservers(ctx context.Context, when platformapi.When, invocation *invocation) error {
	for _, entry := range r.observers {
		if entry.when != when {
			continue
		}
		matched, err := callSelector(entry.name, entry.selector, invocation.command)
		if err != nil {
			if entry.policy == platformapi.FailOpen {
				logHookFailure(entry.name, err)
				continue
			}
			return err
		}
		if !matched {
			continue
		}
		if err := callObserver(ctx, entry, invocation); err != nil {
			if entry.policy == platformapi.FailOpen {
				logHookFailure(entry.name, err)
				continue
			}
			return err
		}
	}
	return nil
}

func callObserver(ctx context.Context, entry observerEntry, invocation platformapi.Invocation) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = runtimeFailure(entry.name, "observer", panicCause(recovered))
		}
	}()
	if entry.observer == nil {
		return runtimeFailure(entry.name, "observer", fmt.Errorf("observer callback is nil"))
	}
	entry.observer(ctx, invocation)
	return nil
}

func buildWrapper(entry wrapperEntry, next platformapi.Handler) (handler platformapi.Handler, err error) {
	if entry.wrapper == nil {
		return nil, runtimeFailure(entry.name, "wrapper", fmt.Errorf("wrapper factory is nil"))
	}
	return func(ctx context.Context, invocation platformapi.Invocation) (callErr error) {
		tracker := newWrapperInvocationTracker(entry.name)
		trackedNext := platformapi.Handler(func(nextCtx context.Context, nextInvocation platformapi.Invocation) error {
			return tracker.call(nextCtx, next, nextInvocation)
		})
		wrapped, buildErr := callWrapperFactory(entry, trackedNext)
		if buildErr != nil {
			return buildErr
		}
		callErr = callWrapperHandler(ctx, entry, wrapped, invocation)
		trackerErr := tracker.closeAndWait()
		if callErr == nil {
			return trackerErr
		}
		// Errors returned by the downstream command retain their product error
		// model. A wrapper-originated structured rejection (for example
		// AbortError) also keeps its own payload. All other wrapper errors cross
		// the untrusted callback boundary as a secret-free RuntimeFailure.
		if downstreamErr := tracker.matching(callErr); downstreamErr != nil {
			return &safeDelegatedError{public: downstreamErr, cause: callErr}
		}
		var abort *platformapi.AbortError
		if frameworkerrors.SafeAs(callErr, &abort) && abort != nil {
			return &safeDelegatedError{public: abort, cause: callErr}
		}
		var denial *platformapi.CommandDeniedError
		if frameworkerrors.SafeAs(callErr, &denial) && denial != nil {
			return &safeDelegatedError{public: denial, cause: callErr}
		}
		var runtimeErr *RuntimeFailure
		if frameworkerrors.SafeAs(callErr, &runtimeErr) && runtimeErr != nil {
			return &safeDelegatedError{public: runtimeErr, cause: callErr}
		}
		return runtimeFailure(entry.name, "wrapper", callErr)
	}, nil
}

func callWrapperHandler(ctx context.Context, entry wrapperEntry, wrapped platformapi.Handler, invocation platformapi.Invocation) (callErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			callErr = runtimeFailure(entry.name, "wrapper", panicCause(recovered))
		}
	}()
	return wrapped(ctx, invocation)
}

func callWrapperFactory(entry wrapperEntry, next platformapi.Handler) (wrapped platformapi.Handler, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = runtimeFailure(entry.name, "wrapper", panicCause(recovered))
		}
	}()
	wrapped = entry.wrapper(next)
	if wrapped == nil {
		return nil, runtimeFailure(entry.name, "wrapper", fmt.Errorf("wrapper factory returned nil"))
	}
	return wrapped, nil
}

func callCommand(path string, run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("command %q panicked: %w", path, panicCause(recovered))
		}
	}()
	return run()
}

func logHookFailure(name string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "[meegle] fail-open hook %s skipped: %v\n", name, err)
}

func callSelector(name string, selector platformapi.Selector, command platformapi.CommandView) (matched bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = runtimeFailure(name, "selector", panicCause(recovered))
		}
	}()
	if selector == nil {
		return false, runtimeFailure(name, "selector", fmt.Errorf("selector is nil"))
	}
	return selector(command), nil
}

type commandView struct{ command *cobra.Command }

func (v commandView) Path() string {
	if v.command == nil {
		return ""
	}
	if path := v.command.Annotations["command_path"]; path != "" {
		return strings.ReplaceAll(path, " ", "/")
	}
	parts := strings.Fields(v.command.CommandPath())
	if len(parts) > 1 {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}

func (v commandView) Domain() string {
	commandPath := v.Path()
	if before, _, ok := strings.Cut(commandPath, "/"); ok {
		return before
	}
	return commandPath
}

func (v commandView) Risk() (platformapi.Risk, bool) {
	value, ok := v.Annotation("risk_level")
	if !ok {
		return "", false
	}
	risk, err := platformapi.ParseRisk(value)
	return risk, err == nil && risk != ""
}

// V1 CLI commands execute with a user credential. Bot/plugin identities are
// intentionally reserved for the later plugin-token capability.
func (v commandView) Identities() []platformapi.Identity {
	return []platformapi.Identity{platformapi.IdentityUser}
}

func (v commandView) Annotation(key string) (string, bool) {
	if v.command == nil {
		return "", false
	}
	for command := v.command; command != nil; command = command.Parent() {
		if value, ok := command.Annotations[key]; ok {
			return value, true
		}
	}
	return "", false
}

type invocation struct {
	command commandView
	args    []string
	started time.Time
	err     error
	denied  bool
	layer   string
	source  string
}

func (i *invocation) Cmd() platformapi.CommandView { return i.command }
func (i *invocation) Args() []string               { return append([]string(nil), i.args...) }
func (i *invocation) Started() time.Time           { return i.started }
func (i *invocation) Err() error                   { return i.err }
func (i *invocation) DeniedByPolicy() bool         { return i.denied }
func (i *invocation) DenialLayer() string          { return i.layer }
func (i *invocation) DenialPolicySource() string   { return i.source }
