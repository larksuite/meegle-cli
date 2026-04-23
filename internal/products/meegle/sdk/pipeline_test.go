// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

func TestNewSDKPipelineFactory(t *testing.T) {
	factory := newSDKPipelineFactory(&ClientConfig{
		Host:  "example.com",
		Token: "test-token",
	}, nil)
	pipe, err := factory(cliapp.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}
	expectedNames := []string{
		"sdk_inject", "param_merge",
		"meegle_validate", "session", "mcp_executor", "output",
	}
	if len(pipe.Steps) != len(expectedNames) {
		t.Fatalf("expected %d steps, got %d", len(expectedNames), len(pipe.Steps))
	}
	for i, name := range expectedNames {
		if pipe.Steps[i].Name() != name {
			t.Errorf("step %d: expected name %q, got %q", i, name, pipe.Steps[i].Name())
		}
	}
}

func TestSDKInjectStep_SetsOutputConfig(t *testing.T) {
	step := &sdkInjectStep{cfg: &ClientConfig{
		Host:    "meegle.com",
		Token:   "my-token",
		Headers: map[string]string{"X-Custom": "value"},
	}}
	state := &pipeline.PipelineContext{}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.OutputConfig["mcp.host"] != "meegle.com" {
		t.Errorf("expected host meegle.com, got %v", state.OutputConfig["mcp.host"])
	}
	if state.OutputConfig["mcp.token"] != "my-token" {
		t.Errorf("expected token my-token, got %v", state.OutputConfig["mcp.token"])
	}
	if state.OutputConfig["mcp.server_url"] != "https://meegle.com/mcp_server/v1" {
		t.Errorf("unexpected server_url: %v", state.OutputConfig["mcp.server_url"])
	}
}

func TestSDKInjectStep_NoToken(t *testing.T) {
	step := &sdkInjectStep{cfg: &ClientConfig{
		Host:    "meegle.com",
		Headers: map[string]string{"X-Custom": "value"},
	}}
	state := &pipeline.PipelineContext{}
	err := step.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.OutputConfig["mcp.token"] != "" {
		t.Errorf("expected empty token, got %v", state.OutputConfig["mcp.token"])
	}
}
