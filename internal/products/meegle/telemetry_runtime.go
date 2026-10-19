// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/larksuite/meegle-cli/internal/products/meegle/httptransport"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	telemetryevent "github.com/larksuite/meegle-cli/internal/products/meegle/telemetry"
)

const (
	telemetryDisclosureVersion    = 2
	telemetryMaintenanceArg       = "__telemetry-maintain"
	telemetryProfileEnv           = "MEEGLE_TELEMETRY_PROFILE"
	telemetryRouteEnv             = "MEEGLE_TELEMETRY_ROUTE"
	telemetryRecordEnv            = "MEEGLE_TELEMETRY_RECORD"
	telemetryControlEnv           = "MEEGLE_TELEMETRY"
	telemetryDisabledValue        = "disabled"
	telemetryMaintenanceRunLimit  = 5 * time.Second
	telemetryMaintenanceFlowLimit = 3 * time.Second
	telemetryCleanupInterval      = time.Minute
)

type telemetryRoute struct {
	Key              string
	Root             string
	PolicyRoot       string
	Host             string
	CredentialSource string
	Profile          string
	IdentityKey      string
}

// Only allowlisted event data and local routing/consent metadata cross processes.
// Credentials stay in the existing environment and are not copied into the record.
type telemetryRecord struct {
	Event       telemetryevent.Event `json:"event"`
	Profile     string               `json:"profile"`
	Host        string               `json:"host"`
	IdentityKey string               `json:"identity_key"`
	Generation  uint64               `json:"generation"`
}

var spawnTelemetryMaintenanceProcess = spawnTelemetryMaintenance

func telemetryConfigRoot() string { return filepath.Join(GetConfigDir(), "telemetry") }

func telemetryCacheRoot() string { return filepath.Join(GetCacheDir(), "telemetry") }

func telemetryRoutesRoot() string { return filepath.Join(telemetryCacheRoot(), "routes") }

func telemetryPoliciesRoot() string { return filepath.Join(telemetryCacheRoot(), "policies") }

func telemetrySendLockDir() string { return filepath.Join(telemetryCacheRoot(), ".send-lock") }

func telemetryMaintenanceLockDir() string {
	return filepath.Join(telemetryCacheRoot(), ".maintenance-lock")
}

func telemetryQuotaLockDir() string { return filepath.Join(telemetryCacheRoot(), ".quota-lock") }

func telemetryPolicyRoot(host string) string {
	host = strings.ToLower(strings.TrimSpace(sanitizeHost(host)))
	digest := sha256.Sum256([]byte(host))
	return filepath.Join(telemetryPoliciesRoot(), hex.EncodeToString(digest[:16]))
}

func resolveTelemetryRoute(profile string) telemetryRoute {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile, _ = GetCurrentProfileName()
	}
	cfg, _ := LoadConfig(profile)
	host := cfg.Host
	if expanded, err := ExpandEnvTemplate(host); err == nil {
		host = expanded
	} else {
		host = ""
	}
	if envHost := os.Getenv("MEEGLE_HOST"); envHost != "" {
		host = envHost
	}
	host = strings.ToLower(strings.TrimSpace(sanitizeHost(host)))
	source := telemetryevent.CredentialSourceUnset
	if os.Getenv("MEEGLE_USER_ACCESS_TOKEN") != "" {
		source = telemetryevent.CredentialSourceEnv
	} else if strings.TrimSpace(cfg.AccessToken) != "" {
		source = telemetryevent.CredentialSourceConfig
	}
	route := telemetryRouteForIdentity(ResolvedIdentity{ProfileName: profile, Host: host})
	route.CredentialSource = source
	return route
}

func telemetryRouteForIdentity(identity ResolvedIdentity) telemetryRoute {
	profile := strings.TrimSpace(identity.ProfileName)
	host := strings.ToLower(strings.TrimSpace(sanitizeHost(identity.Host)))
	digest := sha256.Sum256([]byte(profile + "\x00" + host))
	key := hex.EncodeToString(digest[:16])
	identityKey := ""
	if identity.Source != SourceUnset {
		// Only provenance is persisted, never the token. Rotation within the same
		// source is allowed; switching providers or auth headers is not.
		binding := sha256.Sum256([]byte(strings.Join([]string{profile, host, identity.Source.String(),
			identity.CredentialProvider, http.CanonicalHeaderKey(identity.AccessTokenHeader)}, "\x00")))
		identityKey = hex.EncodeToString(binding[:])
	}
	return telemetryRoute{
		Key: key, Root: filepath.Join(telemetryRoutesRoot(), key), PolicyRoot: telemetryPolicyRoot(host),
		Host: host, CredentialSource: telemetryCredentialSource(identity.Source), Profile: profile, IdentityKey: identityKey,
	}
}

func newTelemetryReporter(route telemetryRoute) *telemetryevent.Reporter {
	return telemetryevent.NewReporter(telemetryevent.ReporterConfig{
		Queue:      telemetryevent.NewQueue(route.Root, telemetryevent.QueueOptions{LockTimeout: telemetryMaintenanceRunLimit, IdentityKey: route.IdentityKey}),
		Settings:   telemetryevent.NewSettingsStore(telemetryConfigRoot()),
		Policies:   telemetryevent.NewPolicyStore(route.PolicyRoot),
		CommitLock: telemetryevent.NewProcessLock(telemetryQuotaLockDir()),
		NextSequence: func(ctx context.Context) (uint64, error) {
			return telemetryevent.NextRoutedQueueSequenceContext(ctx, telemetryRoutesRoot())
		},
	})
}

func SetTelemetryEnabled(enabled bool) error {
	settings := telemetryevent.NewSettingsStore(telemetryConfigRoot())
	if enabled {
		return settings.SetEnabled(true)
	}
	if err := settings.SetEnabled(false); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), telemetryMaintenanceRunLimit+time.Second)
	defer cancel()
	release, err := telemetryevent.NewProcessLock(telemetrySendLockDir()).Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	quotaRelease, err := telemetryevent.NewProcessLock(telemetryQuotaLockDir()).Acquire(ctx)
	if err != nil {
		return err
	}
	defer quotaRelease()
	return os.RemoveAll(telemetryRoutesRoot())
}

func telemetryDisabledByEnvironment() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(telemetryControlEnv)), telemetryDisabledValue)
}

// MaybeShowTelemetryDisclosure makes a best-effort startup disclosure on stderr,
// including for redirected invocations. Collection requires a persisted notice.
func MaybeShowTelemetryDisclosure(writer io.Writer) {
	if telemetryDisabledByEnvironment() {
		return
	}
	_ = EnsureTelemetryDisclosure(writer)
}

// EnsureTelemetryDisclosure displays the current notice once and persists it.
// Explicit enable uses the returned error before changing the local switch.
func EnsureTelemetryDisclosure(writer io.Writer) error {
	if writer == nil {
		return errors.New("telemetry disclosure output is unavailable")
	}
	settings := telemetryevent.NewSettingsStore(telemetryConfigRoot())
	due, err := settings.DisclosureDue(telemetryDisclosureVersion)
	if err != nil || !due {
		return err
	}
	_, err = fmt.Fprintln(writer, `Meegle CLI reports failed-command diagnostics by default for troubleshooting; diagnostics can be linked to your account, tenant, and existing request logs.
Disable reporting and clear pending local diagnostics: meegle telemetry disable (does not delete uploaded data).
Details: https://github.com/larksuite/meegle-cli#failed-command-diagnostics`)
	if err != nil {
		return err
	}
	return settings.MarkDisclosureShown(telemetryDisclosureVersion)
}

// TelemetryEnabled reads the collection gates without resolving credentials or
// fetching remote policy. The policy snapshot matches command-start selection.
func TelemetryEnabled(profile string) (bool, error) {
	if telemetryDisabledByEnvironment() {
		return false, nil
	}
	due, err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).DisclosureDue(telemetryDisclosureVersion)
	if err != nil || due {
		return false, err
	}
	return newTelemetryReporter(resolveTelemetryRoute(profile)).Enabled()
}

func IsTelemetryMaintenanceArgs(args []string) bool {
	return len(args) == 1 && args[0] == telemetryMaintenanceArg
}

func maybeSpawnTelemetryMaintenance(route telemetryRoute) {
	if telemetryMaintenanceDue(route) {
		_ = spawnTelemetryMaintenanceProcess(route.Profile, route.Key, nil)
	}
}

func telemetryMaintenanceDue(route telemetryRoute) bool {
	if telemetryDisabledByEnvironment() || route.Host == "" {
		return false
	}
	settings, err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).Load()
	if err != nil || !settings.Enabled {
		return false
	}
	now := time.Now()
	queue := telemetryevent.NewQueue(route.Root, telemetryevent.QueueOptions{})
	queuePending := queue.HasEvents() && (queue.UploadDue(now) || telemetryCleanupDue(now))
	policyDue := telemetryevent.NewPolicyStore(route.PolicyRoot).RefreshDue(now)
	return queuePending || policyDue
}

func telemetryCleanupDue(now time.Time) bool {
	info, err := os.Stat(filepath.Join(telemetryCacheRoot(), ".last-cleanup"))
	return err != nil || info.ModTime().After(now.Add(telemetryCleanupInterval)) ||
		!now.Before(info.ModTime().Add(telemetryCleanupInterval))
}

func cleanupTelemetryQueues(ctx context.Context) error {
	// Serialize against disable and upload, not foreground recording. Snapshot
	// scans must not hold the commit lock needed by a command finishing now.
	release, err := telemetryevent.NewProcessLock(telemetrySendLockDir()).Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	settings, err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).Load()
	if err != nil || !settings.Enabled {
		return err
	}
	// Record the attempt even on failure to avoid spawning once per command.
	if err := os.WriteFile(filepath.Join(telemetryCacheRoot(), ".last-cleanup"), nil, 0600); err != nil {
		return err
	}
	return telemetryevent.PruneRoutedQueuesContext(ctx, telemetryRoutesRoot())
}

func RunTelemetryMaintenance(ctx context.Context) error {
	return runTelemetryMaintenanceWithHTTPClient(ctx, nil)
}

func runTelemetryMaintenanceWithHTTPClient(ctx context.Context, httpClient *http.Client) error {
	if telemetryDisabledByEnvironment() {
		return nil
	}
	runCtx, cancel := context.WithTimeout(ctx, telemetryMaintenanceRunLimit)
	defer cancel()
	if data := os.Getenv(telemetryRecordEnv); data != "" {
		_ = os.Unsetenv(telemetryRecordEnv)
		if len(data) > 2*telemetryevent.MaxEventBytes {
			return telemetryevent.ErrEventTooLarge
		}
		var record telemetryRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return err
		}
		route := telemetryRouteForIdentity(ResolvedIdentity{ProfileName: record.Profile, Host: record.Host})
		if route.Profile != os.Getenv(telemetryProfileEnv) || route.Key != os.Getenv(telemetryRouteEnv) {
			return errors.New("telemetry record route does not match its worker")
		}
		route.IdentityKey = record.IdentityKey
		if _, err := newTelemetryReporter(route).RecordSelectedContext(runCtx, record.Event, record.Generation); err != nil {
			return err
		}
		if !telemetryMaintenanceDue(route) {
			return nil
		}
	}
	settings, err := telemetryevent.NewSettingsStore(telemetryConfigRoot()).Load()
	if err != nil || !settings.Enabled {
		return nil
	}
	profile := strings.TrimSpace(os.Getenv(telemetryProfileEnv))
	if profile == "" {
		profile, _ = GetCurrentProfileName()
	}
	lockCtx, lockCancel := context.WithTimeout(runCtx, 100*time.Millisecond)
	release, lockErr := telemetryevent.NewProcessLock(telemetryMaintenanceLockDir()).Acquire(lockCtx)
	lockCancel()
	if lockErr != nil {
		// Another detached process owns maintenance. Exiting instead of waiting
		// prevents command bursts from accumulating background workers.
		return nil
	}
	defer release()
	// Leave the network flows their own budget even when an inactive route is
	// large or unreadable. A cleanup failure must not block healthy routes.
	cleanupCtx, cleanupCancel := context.WithTimeout(runCtx, telemetryMaintenanceRunLimit-telemetryMaintenanceFlowLimit)
	cleanupErr := cleanupTelemetryQueues(cleanupCtx)
	cleanupCancel()
	cfg, err := LoadConfig(profile)
	if err != nil {
		return cleanupErr
	}
	identity, err := resolveCLIIdentity(runCtx, cfg, profile, false)
	if err != nil || identity.Host == "" || identity.Token == "" {
		route := resolveTelemetryRoute(profile)
		queue := telemetryevent.NewQueue(route.Root, telemetryevent.QueueOptions{})
		if queue.HasEvents() && queue.UploadDue(time.Now()) {
			_ = queue.RecordUploadAttempt(time.Now())
		}
		policyStore := telemetryevent.NewPolicyStore(route.PolicyRoot)
		if policyStore.RefreshDue(time.Now()) {
			_ = policyStore.RecordRefreshFailure(time.Now())
		}
		return cleanupErr
	}
	route := telemetryRouteForIdentity(identity)
	if expected := strings.TrimSpace(os.Getenv(telemetryRouteEnv)); expected != "" && expected != route.Key {
		return nil
	}
	policyStore := telemetryevent.NewPolicyStore(route.PolicyRoot)

	headers := make(http.Header)
	for key, value := range identity.Headers {
		if strings.EqualFold(key, "Authorization") || (identity.AccessTokenHeader != "" && strings.EqualFold(key, identity.AccessTokenHeader)) {
			continue
		}
		headers.Set(key, value)
	}
	client := *http.DefaultClient
	if httpClient != nil {
		client = *httpClient
	}
	// Fixed telemetry endpoints must not forward credentials through redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := httptransport.New(httptransport.Config{
		BaseURL:          GetAPIBaseURL(MeegleConfig{Host: identity.Host}),
		HTTPClient:       &client,
		Token:            func() (string, error) { return identity.Token, nil },
		Headers:          headers,
		AuthHeader:       identity.AccessTokenHeader,
		UserAgent:        mcpclient.BuildUserAgent(identity.UserAgentCaller),
		Timeout:          3 * time.Second,
		MaxResponseBytes: telemetryevent.MaxReportBytes,
	})

	refresher := telemetryevent.NewPolicyRefresher(telemetryevent.PolicyRefresherConfig{
		Policies: policyStore, Transport: transport,
	})
	uploader := telemetryevent.NewUploader(telemetryevent.UploaderConfig{
		Queue:     telemetryevent.NewQueue(route.Root, telemetryevent.QueueOptions{LockTimeout: 500 * time.Millisecond, IdentityKey: route.IdentityKey}),
		Settings:  telemetryevent.NewSettingsStore(telemetryConfigRoot()),
		Transport: transport, LockDir: telemetrySendLockDir(),
	})
	return errors.Join(cleanupErr, runTelemetryMaintenanceFlows(
		runCtx, telemetryMaintenanceFlowLimit,
		func(flowCtx context.Context) error { return refresher.RefreshIfDue(flowCtx) },
		func(flowCtx context.Context) error { return uploader.Run(flowCtx) },
	))
}

func runTelemetryMaintenanceFlows(parent context.Context, timeout time.Duration, flows ...func(context.Context) error) error {
	if parent == nil {
		parent = context.Background()
	}
	results := make(chan error, len(flows))
	started := 0
	for _, flow := range flows {
		if flow == nil {
			continue
		}
		started++
		go func(run func(context.Context) error) {
			flowCtx, cancel := context.WithTimeout(parent, timeout)
			defer cancel()
			var err error
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						err = fmt.Errorf("telemetry maintenance flow panicked: %v", recovered)
					}
				}()
				err = run(flowCtx)
			}()
			results <- err
		}(flow)
	}
	errs := make([]error, 0, started)
	for index := 0; index < started; index++ {
		errs = append(errs, <-results)
	}
	return errors.Join(errs...)
}

func spawnTelemetryMaintenance(profile, routeKey string, record *telemetryRecord) error {
	var data []byte
	if record != nil {
		var err error
		data, err = json.Marshal(record)
		if err != nil {
			return err
		}
		if len(data) > 2*telemetryevent.MaxEventBytes {
			return telemetryevent.ErrEventTooLarge
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, telemetryMaintenanceArg)
	command.Env = append(os.Environ(), telemetryProfileEnv+"="+profile, telemetryRouteEnv+"="+routeKey, telemetryRecordEnv+"="+string(data))
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
