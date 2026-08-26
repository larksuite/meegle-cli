// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package credential_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/larksuite/meegle-cli/extension/credential"
)

var registrationTestSequence atomic.Int64

type namedProvider struct {
	name     string
	priority int
}

type pointerProvider struct{}

func (*pointerProvider) Name() string                                                { return "pointer" }
func (*pointerProvider) ResolveAccount(context.Context) (*credential.Account, error) { return nil, nil }
func (*pointerProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type panicPriorityProvider struct{}

func (panicPriorityProvider) Name() string  { return "panic-priority" }
func (panicPriorityProvider) Priority() int { panic("priority unavailable") }
func (panicPriorityProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}

type reentrantPriorityProvider struct{}

func (reentrantPriorityProvider) Name() string { return "reentrant-priority" }
func (reentrantPriorityProvider) Priority() int {
	_ = credential.Providers()
	return 5
}
func (reentrantPriorityProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (reentrantPriorityProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type invalidNameProvider struct{}

func (invalidNameProvider) Name() string { return "secret\ncredential-active: forged" }
func (invalidNameProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (invalidNameProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type blockingNameProvider struct{}

func (blockingNameProvider) Name() string { select {} }
func (blockingNameProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (blockingNameProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

var errNamePanicSentinel = errors.New("secret credential name panic")

type panicNameProvider struct{}

func (panicNameProvider) Name() string { panic(errNamePanicSentinel) }
func (panicNameProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (panicNameProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

type panickingTraversalError struct{}

func (*panickingTraversalError) Error() string { return "secret credential traversal error" }
func (*panickingTraversalError) Unwrap() error { panic("credential unwrap must be contained") }
func (*panickingTraversalError) Is(error) bool { panic("credential Is must be contained") }
func (*panickingTraversalError) As(any) bool   { panic("credential As must be contained") }

type maliciousPanicNameProvider struct{}

func (maliciousPanicNameProvider) Name() string { panic(&panickingTraversalError{}) }
func (maliciousPanicNameProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (maliciousPanicNameProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

func TestRegister_ReentrantPriorityDoesNotDeadlock(t *testing.T) {
	if os.Getenv("CREDENTIAL_REENTRANT_HELPER") == "1" {
		credential.Register(reentrantPriorityProvider{})
		return
	}
	// Race-instrumented test binaries can take over a second just to start on
	// CI; the broken implementation still fails immediately with Go's fatal
	// deadlock detector, so five seconds avoids a timing-only false positive.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRegister_ReentrantPriorityDoesNotDeadlock$")
	command.Env = append(os.Environ(), "CREDENTIAL_REENTRANT_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("Register deadlocked while Priority re-entered registry: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, output)
	}
}

func TestRegister_FreezesValidatedProviderNameAndContainsMetadataFailures(t *testing.T) {
	mode := os.Getenv("CREDENTIAL_NAME_HELPER")
	if mode != "" {
		started := time.Now()
		switch mode {
		case "invalid":
			credential.Register(invalidNameProvider{})
		case "blocking":
			credential.Register(blockingNameProvider{})
		case "panic":
			credential.Register(panicNameProvider{})
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatalf("Register(%s) metadata took %s", mode, elapsed)
		}
		registrations := credential.Registrations()
		name := registrations[len(registrations)-1].Name
		if mode == "invalid" && name != "<invalid>" {
			t.Fatalf("invalid provider frozen name = %q", name)
		}
		if mode != "invalid" && name != "<unavailable>" {
			t.Fatalf("failed provider frozen name = %q", name)
		}
		err := credential.Validate()
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "forged") {
			t.Fatalf("Validate() unsafe error = %v", err)
		}
		if mode == "panic" && !errors.Is(err, errNamePanicSentinel) {
			t.Fatalf("name panic chain lost sentinel: %v", err)
		}
		return
	}

	for _, mode := range []string{"invalid", "blocking", "panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
			command.Env = append(os.Environ(), "CREDENTIAL_NAME_HELPER="+mode)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("Register(%s) helper timed out: %v\n%s", mode, ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("Register(%s) helper failed: %v\n%s", mode, err, output)
			}
		})
	}
}
func (panicPriorityProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

func TestRegister_PriorityPanicBecomesValidationError(t *testing.T) {
	credential.Register(panicPriorityProvider{})
	if err := credential.Validate(); err == nil || !strings.Contains(err.Error(), "panic-priority") || !strings.Contains(err.Error(), "priority") {
		t.Fatalf("Validate() error = %v, want stable provider priority error", err)
	}
}

func TestValidate_ContainsPanickingMetadataErrorTraversal(t *testing.T) {
	if os.Getenv("CREDENTIAL_TRAVERSAL_HELPER") == "1" {
		credential.Register(maliciousPanicNameProvider{})
		err := credential.Validate()
		if err == nil {
			t.Fatal("Validate() error = nil")
		}
		if errors.Is(err, errors.New("unrelated")) {
			t.Fatal("malicious registration error matched an unrelated sentinel")
		}
		var target interface{ CredentialTraversalTarget() }
		if errors.As(err, &target) {
			t.Fatal("malicious registration error matched an unrelated target")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestValidate_ContainsPanickingMetadataErrorTraversal$")
	command.Env = append(os.Environ(), "CREDENTIAL_TRAVERSAL_HELPER=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("credential traversal helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("credential traversal escaped containment: %v\n%s", err, output)
	}
}

func TestRegister_IgnoresNilAndTypedNilProviders(t *testing.T) {
	before := len(credential.Providers())
	credential.Register(nil)
	var provider *pointerProvider
	credential.Register(provider)
	if got := len(credential.Providers()); got != before {
		t.Fatalf("Providers() length = %d, want %d after nil registrations", got, before)
	}
}

func (p namedProvider) Name() string  { return p.name }
func (p namedProvider) Priority() int { return p.priority }
func (namedProvider) ResolveAccount(context.Context) (*credential.Account, error) {
	return nil, nil
}
func (namedProvider) ResolveToken(context.Context, credential.TokenSpec) (*credential.Token, error) {
	return nil, nil
}

func TestRegister_OrdersProvidersByPriorityAndKeepsTiesStable(t *testing.T) {
	suffix := registrationTestSequence.Add(1)
	want := []string{
		fmt.Sprintf("first-%d", suffix),
		fmt.Sprintf("second-%d", suffix),
		fmt.Sprintf("default-%d", suffix),
	}
	credential.Register(namedProvider{name: want[2], priority: 10})
	credential.Register(namedProvider{name: want[0], priority: 1})
	credential.Register(namedProvider{name: want[1], priority: 1})

	wanted := map[string]bool{want[0]: true, want[1]: true, want[2]: true}
	var got []string
	for _, provider := range credential.Providers() {
		if wanted[provider.Name()] {
			got = append(got, provider.Name())
		}
	}
	if len(got) != len(want) {
		t.Fatalf("matching providers = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("matching providers = %v, want %v", got, want)
		}
	}
}
