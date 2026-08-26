// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package credential

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"time"

	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
)

const (
	defaultPriority        = 10
	metadataResolveTimeout = time.Second
)

var providerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var providerRegistry struct {
	sync.Mutex
	providers []providerEntry
	issues    []RegistrationIssue
}

type providerEntry struct {
	provider Provider
	name     string
	priority int
}

// Registration is an immutable snapshot of a provider and the priority that
// was frozen when it was registered.
type Registration struct {
	Provider Provider
	Name     string
	Priority int
}

// RegistrationIssue is a stable, non-secret description of a provider that
// could not be registered safely during package initialization.
type RegistrationIssue struct {
	Provider string
	Stage    string
	Cause    error
}

func (i RegistrationIssue) Error() string {
	return fmt.Sprintf("credential provider %q failed during %s", i.Provider, i.Stage)
}

func (i RegistrationIssue) Unwrap() error {
	return frameworkerrors.GuardCause(i.Cause)
}

// Register adds a provider to the process-wide chain. Providers are ordered by
// optional Priority() int, lowest first; equal priorities keep registration
// order. It is normally called from an extension package's init function.
func Register(provider Provider) {
	if isNilProvider(provider) {
		return
	}
	// Enterprise callbacks must run outside the registry lock. A provider may
	// legitimately inspect the already-registered chain while computing its
	// priority; invoking it under the non-reentrant mutex would deadlock init.
	providerName, nameErr := resolveName(provider)
	priorityValue, priorityErr := resolvePriority(provider)
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providerRegistry.providers = append(providerRegistry.providers, providerEntry{provider: provider, name: providerName, priority: priorityValue})
	if nameErr != nil {
		providerRegistry.issues = append(providerRegistry.issues, RegistrationIssue{
			Provider: providerName, Stage: "name", Cause: nameErr,
		})
	}
	if priorityErr != nil {
		providerRegistry.issues = append(providerRegistry.issues, RegistrationIssue{
			Provider: providerName, Stage: "priority", Cause: priorityErr,
		})
	}
	sort.SliceStable(providerRegistry.providers, func(i, j int) bool {
		return providerRegistry.providers[i].priority < providerRegistry.providers[j].priority
	})
}

func isNilProvider(provider Provider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Providers returns a snapshot of the registered provider chain.
func Providers() []Provider {
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	providers := make([]Provider, len(providerRegistry.providers))
	for index, entry := range providerRegistry.providers {
		providers[index] = entry.provider
	}
	return providers
}

// Registrations returns the provider chain together with the effective,
// registration-time priorities used to order it.
func Registrations() []Registration {
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	registrations := make([]Registration, len(providerRegistry.providers))
	for index, entry := range providerRegistry.providers {
		registrations[index] = Registration{Provider: entry.provider, Name: entry.name, Priority: entry.priority}
	}
	return registrations
}

// Validate reports registration-time provider failures. CLI startup calls it
// before resolving accounts or tokens so init-time panics become ordinary,
// diagnosable startup errors instead of terminating the process.
func Validate() error {
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	if len(providerRegistry.issues) == 0 {
		return nil
	}
	issues := make([]error, len(providerRegistry.issues))
	for index, issue := range providerRegistry.issues {
		issues[index] = issue
	}
	return errors.Join(issues...)
}

func resolvePriority(provider Provider) (int, error) {
	prioritized, ok := provider.(interface{ Priority() int })
	if !ok {
		return defaultPriority, nil
	}
	result := make(chan int, 1)
	failed := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				failed <- panicCause(recovered)
			}
		}()
		result <- prioritized.Priority()
	}()
	select {
	case priority := <-result:
		return priority, nil
	case err := <-failed:
		return defaultPriority, err
	case <-time.After(metadataResolveTimeout):
		return defaultPriority, context.DeadlineExceeded
	}
}

func resolveName(provider Provider) (string, error) {
	result := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				failed <- panicCause(recovered)
			}
		}()
		result <- provider.Name()
	}()
	select {
	case name := <-result:
		if !providerNamePattern.MatchString(name) {
			return "<invalid>", fmt.Errorf("invalid provider name %q", name)
		}
		return name, nil
	case err := <-failed:
		return "<unavailable>", err
	case <-time.After(metadataResolveTimeout):
		return "<unavailable>", context.DeadlineExceeded
	}
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
