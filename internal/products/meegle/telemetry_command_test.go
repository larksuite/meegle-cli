// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	telemetryevent "github.com/larksuite/meegle-cli/internal/products/meegle/telemetry"
	"github.com/larksuite/meegle-cli/pkg/framework/observability"
)

func TestFallbackCommandAndFlagsNeverKeepValues(t *testing.T) {
	command, flags := fallbackCommandAndFlags([]string{
		"--profile", "personal-secret-profile", "ai-handoff", "create-link",
		"--query=never-store-this", "--related-context", `{"work_item_id":"secret"}`,
	})
	if command != "unknown" || len(flags) != 0 {
		t.Fatalf("fallback = (%q, %#v), want no raw argv-derived fields", command, flags)
	}
}

func TestSanitizeErrorCodeKeepsPrefixedValueWithinLimit(t *testing.T) {
	code := sanitizeErrorCode("1" + strings.Repeat("a", 80))
	if len(code) != 64 || !strings.HasPrefix(code, "E_1") {
		t.Fatalf("sanitized code = %q, length = %d", code, len(code))
	}
}

func TestObserveKnownCommandAcceptsOnlyRegisteredNames(t *testing.T) {
	root := &cobra.Command{Use: "meegle"}
	ai := &cobra.Command{Use: "ai-handoff"}
	create := &cobra.Command{Use: "create-link"}
	create.Flags().String("query", "", "query")
	root.PersistentFlags().String("profile", "", "profile")
	ai.AddCommand(create)
	root.AddCommand(ai)
	tracker := &CommandTelemetry{trace: observability.NewTrace(), fallbackCommand: "unknown"}
	ctx := tracker.Context(context.Background())
	tracker.ObserveKnownCommand(ctx, root, []string{
		"--profile", "private-profile", "ai-handoff", "create-link",
		"--query", "never-store-this-secret", "--unknown-secret-flag=value",
	})
	snapshot := tracker.trace.Snapshot()
	if snapshot.Command != "ai-handoff create-link" || len(snapshot.FlagNames) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	serialized, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{"private-profile", "never-store-this-secret", "unknown-secret-flag"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("trace contains %q: %s", forbidden, serialized)
		}
	}
}

func TestCommandFinishQueuesFailureAndSpawnsOneMaintenanceProcess(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	originalSpawn := spawnTelemetryMaintenanceProcess
	spawned := 0
	spawnTelemetryMaintenanceProcess = func(_ string, _ string, record *telemetryRecord) error {
		spawned++
		if record == nil {
			return nil
		}
		_, err := newTelemetryReporter(trackerRouteForRecord(record)).RecordSelectedContext(context.Background(), record.Event, record.Generation)
		return err
	}
	t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })

	tracker := StartCommandTelemetryWithVersion([]string{"workitem", "get"}, "1.2.3")
	ctx := tracker.Context(context.Background())
	tracker.ObserveKnownCommand(ctx, telemetryTestRoot(), []string{"workitem", "get"})
	tracker.Finish(context.DeadlineExceeded, 1, false)

	if spawned != 1 {
		t.Fatalf("maintenance spawns = %d, want 1", spawned)
	}
	items, err := telemetryevent.NewQueue(tracker.route.Root, telemetryevent.QueueOptions{}).List()
	if err != nil || len(items) != 1 {
		t.Fatalf("queue = %#v, err=%v", items, err)
	}
	if items[0].Event.PolicyFromValidTCC {
		t.Fatal("initial default policy must stamp policy_from_valid_tcc=false")
	}
}

func TestCommandFinishDoesNotCollectSuccessButStillRefreshesDuePolicy(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	originalSpawn := spawnTelemetryMaintenanceProcess
	spawned := 0
	spawnTelemetryMaintenanceProcess = func(string, string, *telemetryRecord) error { spawned++; return nil }
	t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })

	tracker := StartCommandTelemetry([]string{"workitem", "get"})
	tracker.Finish(nil, 0, false)

	if spawned != 1 {
		t.Fatalf("maintenance spawns = %d, want 1 for due policy", spawned)
	}
	if telemetryevent.NewQueue(tracker.route.Root, telemetryevent.QueueOptions{}).HasEvents() {
		t.Fatal("successful command was collected")
	}
}

func TestCommandFinishDoesNotFallBackToForegroundOnWorkerFailure(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	originalSpawn := spawnTelemetryMaintenanceProcess
	spawned := 0
	spawnTelemetryMaintenanceProcess = func(_ string, _ string, record *telemetryRecord) error {
		spawned++
		if record == nil {
			t.Fatal("missing failure record")
		}
		return errors.New("worker unavailable")
	}
	t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })
	tracker := StartCommandTelemetry([]string{"workitem", "get"})
	tracker.Finish(context.DeadlineExceeded, 1, false)
	if spawned != 1 || telemetryevent.NewQueue(tracker.route.Root, telemetryevent.QueueOptions{}).HasEvents() {
		t.Fatalf("foreground fallback: worker attempts=%d", spawned)
	}
}

func TestCommandFinishDoesNotStartWorkerForExcludedFailure(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	tracker := StartCommandTelemetry([]string{"workitem", "get"})
	policy := telemetryevent.DefaultPolicy()
	policy.Sampling.Default.FailureRate = 0
	if err := telemetryevent.NewPolicyStore(tracker.route.PolicyRoot).ApplyRefresh(policy, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	originalSpawn := spawnTelemetryMaintenanceProcess
	spawnTelemetryMaintenanceProcess = func(string, string, *telemetryRecord) error {
		t.Fatal("excluded failure started a worker")
		return nil
	}
	t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })
	tracker.Finish(context.DeadlineExceeded, 1, false)
}

func TestTelemetryMaintenanceDoesNotSpawnDuringUploadCooldown(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	route := resolveTelemetryRoute("")
	now := time.Now()
	queue := telemetryevent.NewQueue(route.Root, telemetryevent.QueueOptions{})
	tracker := StartCommandTelemetry([]string{"workitem", "get"})
	if err := queue.Enqueue(tracker.eventFromResult(context.DeadlineExceeded, 1, false)); err != nil {
		t.Fatal(err)
	}
	if err := queue.RecordUploadAttempt(now); err != nil {
		t.Fatal(err)
	}
	if err := telemetryevent.NewPolicyStore(route.PolicyRoot).ApplyRefresh(telemetryevent.DefaultPolicy(), true, now); err != nil {
		t.Fatal(err)
	}
	if err := cleanupTelemetryQueues(context.Background()); err != nil {
		t.Fatal(err)
	}
	originalSpawn := spawnTelemetryMaintenanceProcess
	spawned := 0
	spawnTelemetryMaintenanceProcess = func(string, string, *telemetryRecord) error { spawned++; return nil }
	t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })

	maybeSpawnTelemetryMaintenance(route)

	if spawned != 0 {
		t.Fatalf("maintenance spawns during upload cooldown = %d, want 0", spawned)
	}
	stale := now.Add(-2 * telemetryCleanupInterval)
	if err := os.Chtimes(filepath.Join(telemetryCacheRoot(), ".last-cleanup"), stale, stale); err != nil {
		t.Fatal(err)
	}
	maybeSpawnTelemetryMaintenance(route)
	if spawned != 1 {
		t.Fatalf("local cleanup was blocked by upload cooldown: spawns=%d", spawned)
	}
}

func TestTelemetryRoutesUseProfileAndHostWhilePolicyUsesOnlyHost(t *testing.T) {
	setupTelemetryCommandRuntime(t)
	profileA := resolveTelemetryRoute("profile-a")
	profileB := resolveTelemetryRoute("profile-b")
	if profileA.Root == profileB.Root {
		t.Fatalf("same-host profiles share event queue: %#v %#v", profileA, profileB)
	}
	if profileA.PolicyRoot != profileB.PolicyRoot {
		t.Fatalf("same-host profiles do not share policy: %#v %#v", profileA, profileB)
	}
}

func TestTelemetryUsesCommandProfileForQueueAndMaintenance(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"single", []string{"--profile", "a"}, "a"},
		{"repeated", []string{"--profile", "a", "--profile", "b"}, "b"},
		{"equals", []string{"--profile=a", "--profile=b"}, "b"},
		{"separator", []string{"--profile=a", "--", "--profile=b"}, "a"},
		{"missing value", []string{"--profile=a", "--profile"}, "a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTelemetryCommandRuntime(t)
			originalSpawn := spawnTelemetryMaintenanceProcess
			var spawnedProfile, spawnedRoute string
			spawnTelemetryMaintenanceProcess = func(profile, route string, record *telemetryRecord) error {
				spawnedProfile, spawnedRoute = profile, route
				_, err := newTelemetryReporter(trackerRouteForRecord(record)).RecordSelectedContext(context.Background(), record.Event, record.Generation)
				return err
			}
			t.Cleanup(func() { spawnTelemetryMaintenanceProcess = originalSpawn })
			tracker := StartCommandTelemetry(test.args)
			tracker.Context(context.Background())
			tracker.Finish(context.Canceled, 130, false)
			wantRoute := resolveTelemetryRoute(test.want)
			if tracker.profile != test.want || spawnedProfile != test.want || spawnedRoute != wantRoute.Key {
				t.Fatalf("profile=%q, maintenance=(%q, %q), want (%q, %q)", tracker.profile, spawnedProfile, spawnedRoute, test.want, wantRoute.Key)
			}
			items, err := telemetryevent.NewQueue(wantRoute.Root, telemetryevent.QueueOptions{}).List()
			if err != nil || len(items) != 1 {
				t.Fatalf("expected profile queue = %#v, %v", items, err)
			}
		})
	}
}

func TestTelemetryDisclosureUpdateIsShownOnceWithoutChangingLocalSwitch(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			setupTestDir(t)
			t.Setenv(telemetryControlEnv, "")
			settings := telemetryevent.NewSettingsStore(telemetryConfigRoot())
			if err := settings.MarkDisclosureShown(1); err != nil {
				t.Fatal(err)
			}
			if err := settings.SetEnabled(enabled); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			MaybeShowTelemetryDisclosure(&output)
			MaybeShowTelemetryDisclosure(&output)
			if strings.Count(output.String(), "meegle telemetry disable") != 1 || strings.Count(output.String(), "\n") != 3 {
				t.Fatalf("disclosure = %q, want one three-line notice", output.String())
			}
			if !strings.Contains(output.String(), "account, tenant, and existing request logs") ||
				!strings.Contains(output.String(), "does not delete uploaded data") ||
				!strings.Contains(output.String(), "https://github.com/larksuite/meegle-cli#failed-command-diagnostics") {
				t.Fatalf("missing disclosure details: %q", output.String())
			}
			state, err := settings.Load()
			if err != nil || state.Enabled != enabled {
				t.Fatalf("disclosure changed local switch: state=%+v err=%v", state, err)
			}
		})
	}
}

func TestTelemetryDisclosureEnablesFirstNonInteractiveFailure(t *testing.T) {
	originalDir := configDir
	configDir = t.TempDir()
	t.Cleanup(func() { configDir = originalDir })
	t.Setenv("MEEGLE_TELEMETRY", "")
	var output bytes.Buffer
	MaybeShowTelemetryDisclosure(&output)
	if !strings.Contains(output.String(), "meegle telemetry disable") {
		t.Fatalf("missing non-interactive disclosure: %q", output.String())
	}
	due, err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).DisclosureDue(telemetryDisclosureVersion)
	if err != nil || due {
		t.Fatalf("disclosure due = %v, err = %v", due, err)
	}
	tracker := StartCommandTelemetry([]string{"probe"})
	if !tracker.collectionAllowed {
		t.Fatal("first non-interactive command must allow collection after disclosure")
	}
}

type failedDisclosureWriter struct{}

func (failedDisclosureWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestTelemetryDisclosureFailureDoesNotAuthorizeCollection(t *testing.T) {
	for _, mode := range []string{"nil-writer", "output", "persistence", "environment", "hard-disable"} {
		t.Run(mode, func(t *testing.T) {
			originalDir := configDir
			configDir = t.TempDir()
			t.Cleanup(func() { configDir = originalDir })
			t.Setenv("MEEGLE_TELEMETRY", "")
			var output bytes.Buffer
			switch mode {
			case "nil-writer":
				if err := EnsureTelemetryDisclosure(nil); err == nil {
					t.Fatal("nil output must fail disclosure")
				}
			case "output":
				if err := EnsureTelemetryDisclosure(failedDisclosureWriter{}); err == nil {
					t.Fatal("failed output must fail disclosure")
				}
			case "persistence":
				if err := os.MkdirAll(telemetryConfigRoot(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(telemetryConfigRoot(), ".disclosure-lock"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := EnsureTelemetryDisclosure(&output); err == nil {
					t.Fatal("failed persistence must fail disclosure")
				}
			case "environment":
				t.Setenv("MEEGLE_TELEMETRY", "disabled")
				MaybeShowTelemetryDisclosure(&output)
				if output.Len() != 0 {
					t.Fatal("environment opt-out must suppress automatic notice")
				}
			case "hard-disable":
				if err := SetTelemetryEnabled(false); err != nil {
					t.Fatal(err)
				}
				MaybeShowTelemetryDisclosure(&output)
			}
			if tracker := StartCommandTelemetry([]string{"probe"}); tracker.collectionAllowed {
				t.Fatal("failed disclosure or opt-out must prevent collection")
			}
		})
	}
}

func TestTelemetryStatusMatchesCollectionGates(t *testing.T) {
	for _, mode := range []string{"default", "profile-policy", "local-disabled", "environment", "missing-notice", "corrupt-notice", "corrupt-settings", "corrupt-policy"} {
		t.Run(mode, func(t *testing.T) {
			setupTelemetryCommandRuntime(t)
			const profile = "selected"
			if err := SaveProfileConfig(profile, MeegleConfig{Host: "meegle.com"}); err != nil {
				t.Fatal(err)
			}
			route := resolveTelemetryRoute(profile)
			switch mode {
			case "profile-policy":
				policy := telemetryevent.DefaultPolicy()
				policy.Enabled = false
				if err := telemetryevent.NewPolicyStore(route.PolicyRoot).ApplyRefresh(policy, true, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "local-disabled":
				if err := SetTelemetryEnabled(false); err != nil {
					t.Fatal(err)
				}
			case "environment":
				t.Setenv("MEEGLE_TELEMETRY", "disabled")
			case "missing-notice":
				if err := os.Remove(filepath.Join(telemetryConfigRoot(), "disclosure.json")); err != nil {
					t.Fatal(err)
				}
			case "corrupt-notice", "corrupt-settings", "corrupt-policy":
				path := filepath.Join(telemetryConfigRoot(), "disclosure.json")
				if mode == "corrupt-settings" {
					path = filepath.Join(telemetryConfigRoot(), "settings.json")
				} else if mode == "corrupt-policy" {
					path = filepath.Join(route.PolicyRoot, "policy.json")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			enabled, err := TelemetryEnabled(profile)
			if wantErr := strings.HasPrefix(mode, "corrupt-"); (err != nil) != wantErr {
				t.Fatalf("status error = %v, want error %v", err, wantErr)
			}
			tracker := StartCommandTelemetry([]string{"--profile", profile, "probe"})
			if enabled != tracker.collectionAllowed || enabled != (mode == "default") {
				t.Fatalf("status enabled=%v, command collection=%v, mode=%s", enabled, tracker.collectionAllowed, mode)
			}
		})
	}
}

func setupTelemetryCommandRuntime(t *testing.T) {
	t.Helper()
	setupTestDir(t)
	t.Setenv("MEEGLE_HOST", "meegle.com")
	t.Setenv("MEEGLE_TELEMETRY", "")
	if err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).MarkDisclosureShown(telemetryDisclosureVersion); err != nil {
		t.Fatal(err)
	}
}

func trackerRouteForRecord(record *telemetryRecord) telemetryRoute {
	route := telemetryRouteForIdentity(ResolvedIdentity{ProfileName: record.Profile, Host: record.Host})
	route.IdentityKey = record.IdentityKey
	return route
}

func telemetryTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "meegle"}
	workitem := &cobra.Command{Use: "workitem"}
	get := &cobra.Command{Use: "get"}
	workitem.AddCommand(get)
	root.AddCommand(workitem)
	return root
}
