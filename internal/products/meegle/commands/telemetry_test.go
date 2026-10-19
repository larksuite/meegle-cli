// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/internal/products/meegle"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	telemetryevent "github.com/larksuite/meegle-cli/internal/products/meegle/telemetry"
)

func TestTelemetrySettingsFailureUsesClientError(t *testing.T) {
	const childEnv = "MEEGLE_TEST_TELEMETRY_SETTINGS_ERROR"
	if os.Getenv(childEnv) == "1" {
		for _, action := range []string{"enable", "disable"} {
			command := NewTelemetryCmd()
			command.SetArgs([]string{action})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			err := command.Execute()
			var clientErr *meerrors.MeegleError
			if !errors.As(err, &clientErr) || clientErr == nil {
				t.Fatalf("%s error type = %T, want MeegleError", action, err)
			}
			if clientErr.Code != "TELEMETRY_SETTINGS_FAILED" || clientErr.ExitCode != 1 || clientErr.Cause == nil {
				t.Fatalf("%s error = %#v, want stable client error with cause", action, clientErr)
			}
		}
		return
	}
	// Config paths are initialized once, so isolate the command in a child process.
	fixtureHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixtureHome, ".meegle"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fixtureHome)
	t.Setenv(childEnv, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestTelemetrySettingsFailureUsesClientError$")
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("isolated command test: %v\n%s", err, output)
	}
}

type failingTelemetryNoticeWriter struct{}

func (failingTelemetryNoticeWriter) Write([]byte) (int, error) {
	return 0, errors.New("notice output unavailable")
}

func TestTelemetryEnableAndStatusCommands(t *testing.T) {
	const childEnv = "MEEGLE_TEST_TELEMETRY_CONTROLS"
	mode := os.Getenv(childEnv)
	if mode == "" {
		for _, mode := range []string{"enable", "enable-environment", "enable-output-error", "enable-save-error", "enable-settings-error", "status-enabled", "status-disabled", "status-environment", "status-profile", "status-corrupt", "status-table", "status-ndjson"} {
			t.Run(mode, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("MEEGLE_TELEMETRY", "")
				t.Setenv("MEEGLE_HOST", "")
				t.Setenv(childEnv, mode)
				child := exec.Command(os.Args[0], "-test.run=^TestTelemetryEnableAndStatusCommands$", "-test.timeout=10s")
				if output, err := child.CombinedOutput(); err != nil {
					t.Fatalf("isolated command: %v\n%s", err, output)
				}
			})
		}
		return
	}

	root := filepath.Join(meegle.GetConfigDir(), "telemetry")
	settings := telemetryevent.NewSettingsStore(root)
	if err := settings.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	var output, notice bytes.Buffer
	command := NewTelemetryCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetOut(&output)
	command.SetErr(&notice)
	action := "status"
	if strings.HasPrefix(mode, "enable") {
		action = "enable"
	}
	if strings.HasSuffix(mode, "environment") {
		t.Setenv("MEEGLE_TELEMETRY", "disabled")
	}
	if mode == "enable-output-error" {
		command.SetErr(failingTelemetryNoticeWriter{})
	}
	if mode == "enable-save-error" {
		if err := os.Mkdir(filepath.Join(root, ".disclosure-lock"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "enable-settings-error" {
		if err := os.Remove(filepath.Join(root, "settings.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "settings.json"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if action == "status" {
		if err := settings.MarkDisclosureShown(2); err != nil {
			t.Fatal(err)
		}
		if mode != "status-disabled" {
			if err := settings.SetEnabled(true); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "status-corrupt" {
			if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	args := []string{action}
	if mode == "status-profile" {
		const host = "selected.example.com"
		if err := meegle.SaveProfileConfig("selected", meegle.MeegleConfig{Host: host}); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(host))
		policyRoot := filepath.Join(meegle.GetCacheDir(), "telemetry", "policies", hex.EncodeToString(digest[:16]))
		policy := telemetryevent.DefaultPolicy()
		policy.Enabled = false
		if err := telemetryevent.NewPolicyStore(policyRoot).ApplyRefresh(policy, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		parent := &cobra.Command{Use: "meegle", SilenceUsage: true, SilenceErrors: true}
		parent.PersistentFlags().String("profile", "", "profile")
		parent.AddCommand(command)
		command = parent
		args = []string{"--profile", "selected", "telemetry", "status"}
	} else if mode == "status-table" {
		args = append(args, "--format", "table")
	} else if mode == "status-ndjson" {
		args = append(args, "-o", "ndjson")
	}
	command.SetArgs(args)
	err := command.Execute()
	if strings.HasSuffix(mode, "error") || mode == "status-corrupt" {
		var clientErr *meerrors.MeegleError
		if !errors.As(err, &clientErr) || clientErr == nil || output.Len() != 0 {
			t.Fatalf("failed command err=%v stdout=%q", err, output.String())
		}
		if mode == "enable-output-error" || mode == "enable-save-error" {
			state, loadErr := settings.Load()
			if loadErr != nil || state.Enabled {
				t.Fatalf("failed notice enabled collection: %+v, %v", state, loadErr)
			}
			if due, dueErr := settings.DisclosureDue(2); dueErr != nil || !due {
				t.Fatalf("failed notice was acknowledged: due=%v err=%v", due, dueErr)
			}
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if action == "enable" {
		if !strings.Contains(notice.String(), "meegle telemetry disable") {
			t.Fatalf("enable missing notice: %q", notice.String())
		}
		if due, err := settings.DisclosureDue(2); err != nil || due {
			t.Fatalf("enable missing disclosure record: due=%v err=%v", due, err)
		}
		state, err := settings.Load()
		if err != nil || !state.Enabled || !strings.Contains(output.String(), "enabled locally") {
			t.Fatalf("enable state=%+v err=%v output=%q", state, err, output.String())
		}
		return
	}
	if mode == "status-table" {
		if !strings.Contains(output.String(), "enabled") || !strings.Contains(output.String(), "true") {
			t.Fatalf("table status = %q", output.String())
		}
		return
	}
	var payload map[string]bool
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wantEnabled := mode != "status-disabled" && mode != "status-environment" && mode != "status-profile"
	if len(payload) != 1 || payload["enabled"] != wantEnabled {
		t.Fatalf("status = %q, want enabled=%v only", output.String(), wantEnabled)
	}
}
