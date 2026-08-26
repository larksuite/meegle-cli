// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	extplatform "github.com/larksuite/meegle-cli/extension/platform"
	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"
	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

type rebuildTraceKey struct{}

type rebuildTrace struct {
	observed []string
	wrapped  []string
}

var registerRebuildPlugin sync.Once

func ensureRebuildPluginRegistered() {
	registerRebuildPlugin.Do(func() {
		extplatform.Register(extplatform.NewPlugin("mcp-rebuild-governance", "1.0.0").
			FailClosed().
			Observer(extplatform.Before, "observe", extplatform.All(), func(ctx context.Context, invocation extplatform.Invocation) {
				if trace, _ := ctx.Value(rebuildTraceKey{}).(*rebuildTrace); trace != nil {
					trace.observed = append(trace.observed, invocation.Cmd().Path())
				}
			}).
			Wrap("wrap", extplatform.All(), func(next extplatform.Handler) extplatform.Handler {
				return func(ctx context.Context, invocation extplatform.Invocation) error {
					if trace, _ := ctx.Value(rebuildTraceKey{}).(*rebuildTrace); trace != nil {
						trace.wrapped = append(trace.wrapped, invocation.Cmd().Path())
					}
					return next(ctx, invocation)
				}
			}).
			Restrict(&extplatform.Rule{Name: "deny-update", Allow: []string{"**"}, Deny: []string{"workitem/update"}}).
			MustBuild())
	})
}

type changingToolLister struct {
	calls int
}

func (l *changingToolLister) ListTools(context.Context) ([]types.ToolDefinition, error) {
	l.calls++
	if l.calls == 1 {
		return []types.ToolDefinition{{Name: "get_workitem_brief"}}, nil
	}
	return []types.ToolDefinition{{Name: "get_view_detail"}, {Name: "update_field"}}, nil
}

type integrationCountStep struct{ calls *int }

func (integrationCountStep) Name() string { return "count" }
func (s integrationCountStep) Execute(context.Context, *pipeline.PipelineContext) error {
	*s.calls++
	return nil
}

func TestMCPToolsListRebuild_ReappliesPlatformExtensionsExactlyOnce(t *testing.T) {
	if os.Getenv("PLATFORM_REBUILD_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPToolsListRebuild_ReappliesPlatformExtensionsExactlyOnce$")
		command.Env = append(os.Environ(), "PLATFORM_REBUILD_HELPER=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("platform rebuild helper timed out: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("platform rebuild helper failed: %v\n%s", err, output)
		}
		return
	}
	ensureRebuildPluginRegistered()
	runtime, err := platformruntime.Install("1.0.0")
	if err != nil {
		t.Fatalf("install extensions: %v", err)
	}
	lister := &changingToolLister{}
	setup := meegle.NewDynamicRegistrySetup(lister, nil)
	manager := registry.NewManager(setup)
	if err := manager.Init(context.Background()); err != nil {
		t.Fatalf("initial tools/list: %v", err)
	}
	pipelineCalls := 0
	app, err := cliapp.New(
		cliapp.WithAppName("rebuild-integration"),
		cliapp.WithManager(manager),
		cliapp.WithPipeline(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{integrationCountStep{calls: &pipelineCalls}}}),
		cliapp.WithRootCommandCustomizer(func(root *cobra.Command, _ cliapp.RootCommandMetadata) { runtime.Apply(root) }),
	)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	trace := &rebuildTrace{}
	ctx := context.WithValue(context.Background(), rebuildTraceKey{}, trace)
	output := &bytes.Buffer{}
	if err := app.ExecuteWithIO(ctx, []string{"workitem", "get"}, output, output); err != nil {
		t.Fatalf("execute first discovered tool: %v", err)
	}
	if err := manager.Rebuild(ctx); err != nil {
		t.Fatalf("second tools/list: %v", err)
	}
	if err := app.ExecuteWithIO(ctx, []string{"view", "get"}, output, output); err != nil {
		t.Fatalf("execute tool discovered after rebuild: %v", err)
	}
	if err := app.ExecuteWithIO(ctx, []string{"workitem", "update"}, output, output); err == nil {
		t.Fatal("policy did not deny write tool discovered after rebuild")
	}

	if lister.calls != 2 {
		t.Fatalf("tools/list calls = %d, want 2", lister.calls)
	}
	if got := strings.Join(trace.observed, ","); got != "workitem/get,view/get,workitem/update" {
		t.Fatalf("observer paths = %q, want each discovered command exactly once", got)
	}
	if got := strings.Join(trace.wrapped, ","); got != "workitem/get,view/get" {
		t.Fatalf("wrapper paths = %q, want each allowed command exactly once", got)
	}
	if pipelineCalls != 2 {
		t.Fatalf("pipeline calls = %d, want only allowed discovered commands", pipelineCalls)
	}
}
