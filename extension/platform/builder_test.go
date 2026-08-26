// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/extension/platform"
)

type recordingRegistrar struct {
	observers  int
	wrappers   int
	lifecycles int
	rules      int
}

func TestBuilder_RejectsInvalidVersionAndNilHooks(t *testing.T) {
	_, err := platform.NewPlugin("corp-governance", "dev").
		Observer(platform.After, "audit", platform.All(), nil).
		Wrap("guard", platform.All(), nil).
		On(platform.Startup, "startup", nil).
		Build()
	if err == nil {
		t.Fatal("Build() succeeded")
	}
	for _, fragment := range []string{"invalid plugin version", "observer must not be nil", "wrapper must not be nil", "lifecycle handler must not be nil"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("Build() error missing %q: %v", fragment, err)
		}
	}
}

func (r *recordingRegistrar) Observe(platform.When, string, platform.Selector, platform.Observer) {
	r.observers++
}
func (r *recordingRegistrar) Wrap(string, platform.Selector, platform.Wrapper) {
	r.wrappers++
}
func (r *recordingRegistrar) On(platform.LifecycleEvent, string, platform.LifecycleHandler) {
	r.lifecycles++
}
func (r *recordingRegistrar) Restrict(*platform.Rule) { r.rules++ }

func TestBuilder_InstallsAllV1HookKindsAndClosesRestrictionFailures(t *testing.T) {
	plugin := platform.NewPlugin("corp-governance", "1.0.0").
		Observer(platform.After, "audit", platform.All(), func(context.Context, platform.Invocation) {}).
		Wrap("write-context", platform.ByWrite(), func(next platform.Handler) platform.Handler { return next }).
		On(platform.Startup, "startup", func(context.Context, *platform.LifecycleContext) error { return nil }).
		Restrict(&platform.Rule{Name: "read-only", MaxRisk: platform.RiskRead}).
		MustBuild()

	if !plugin.Capabilities().Restricts {
		t.Fatal("Restrict() did not declare the plugin as restricting")
	}
	if plugin.Capabilities().FailurePolicy != platform.FailClosed {
		t.Fatalf("failure policy = %v, want FailClosed", plugin.Capabilities().FailurePolicy)
	}

	recorder := &recordingRegistrar{}
	if err := plugin.Install(recorder); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if recorder.observers != 1 || recorder.wrappers != 1 || recorder.lifecycles != 1 || recorder.rules != 1 {
		t.Fatalf("installed counts = %+v, want one of each", recorder)
	}
}
