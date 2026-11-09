// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	telemetryevent "github.com/larksuite/meegle-cli/internal/products/meegle/telemetry"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/observability"
)

type CommandTelemetry struct {
	callID            string
	started           time.Time
	profile           string
	hostClass         string
	credentialSource  string
	fallbackCommand   string
	fallbackFlags     []string
	trace             *observability.Trace
	reporter          *telemetryevent.Reporter
	collectionAllowed bool
	localEnabled      bool
	consentGeneration uint64
	route             telemetryRoute
	clientVersion     string
}

type commandTelemetryContextKey struct{}

func StartCommandTelemetry(args []string) *CommandTelemetry {
	return StartCommandTelemetryWithVersion(args, "")
}

func StartCommandTelemetryWithVersion(args []string, clientVersion string) *CommandTelemetry {
	if telemetryDisabledByEnvironment() {
		command, flags := fallbackCommandAndFlags(args)
		return &CommandTelemetry{
			fallbackCommand: command,
			fallbackFlags:   flags,
			trace:           observability.NewTrace(),
			clientVersion:   normalizedTelemetryClientVersion(clientVersion),
		}
	}
	profile, _ := GetCurrentProfileName()
	profile = profileNameFromArgs(args, profile)
	route := resolveTelemetryRoute(profile)
	command, flags := fallbackCommandAndFlags(args)
	tracker := &CommandTelemetry{
		callID: randomTelemetryCallID(), started: time.Now(), profile: profile,
		fallbackCommand: command, fallbackFlags: flags,
		trace: observability.NewTrace(), reporter: newTelemetryReporter(route), route: route,
		clientVersion: normalizedTelemetryClientVersion(clientVersion),
	}
	// Collection must never begin before the versioned disclosure has been
	// persisted. main prints it before starting telemetry; if stderr was not
	// writable, the next command retries and this command is not collected.
	settingsStore := telemetryevent.NewSettingsStore(telemetryConfigRoot())
	disclosureDue, err := settingsStore.DisclosureDue(telemetryDisclosureVersion)
	if err != nil || disclosureDue {
		return tracker
	}
	settings, err := settingsStore.Load()
	if err != nil || !settings.Enabled {
		return tracker
	}
	tracker.localEnabled = true
	tracker.consentGeneration = settings.Generation
	enabled, err := tracker.reporter.Enabled()
	tracker.hostClass = classifyHost(route.Host)
	tracker.credentialSource = route.CredentialSource
	if err == nil && enabled {
		tracker.collectionAllowed = true
	}
	return tracker
}

func (tracker *CommandTelemetry) Context(ctx context.Context) context.Context {
	if tracker == nil {
		return ctx
	}
	ctx = observability.WithTrace(ctx, tracker.trace)
	observability.SetCommand(ctx, strings.Fields(tracker.fallbackCommand), tracker.fallbackFlags)
	observability.SetStage(ctx, telemetryevent.StageCLIStart)
	return context.WithValue(ctx, commandTelemetryContextKey{}, tracker)
}

func observeTelemetryIdentity(ctx context.Context, identity ResolvedIdentity) {
	if ctx == nil {
		return
	}
	tracker, _ := ctx.Value(commandTelemetryContextKey{}).(*CommandTelemetry)
	if tracker == nil || !tracker.localEnabled {
		return
	}
	tracker.route = telemetryRouteForIdentity(identity)
	tracker.profile = identity.ProfileName
	tracker.hostClass = classifyHost(identity.Host)
	tracker.credentialSource = telemetryCredentialSource(identity.Source)
	tracker.reporter = newTelemetryReporter(tracker.route)
	enabled, err := tracker.reporter.Enabled()
	tracker.collectionAllowed = err == nil && enabled
}

func (tracker *CommandTelemetry) SetStage(ctx context.Context, stage string) {
	observability.SetStage(ctx, stage)
}

// ObserveKnownCommand records only command and flag names confirmed by the
// already-built Cobra registry. Raw argv values and unknown names are never
// accepted into the telemetry model, including when parsing later fails.
func (tracker *CommandTelemetry) ObserveKnownCommand(ctx context.Context, root *cobra.Command, args []string) {
	if tracker == nil || root == nil {
		return
	}
	cmd, _, err := root.Find(args)
	if err != nil || cmd == nil || cmd == root {
		return
	}
	path := strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), root.Name()))
	if path == "" {
		return
	}
	flags := knownFlagNames(root, cmd, args)
	tracker.fallbackCommand = path
	tracker.fallbackFlags = flags
	observability.SetCommand(ctx, strings.Fields(path), flags)
}

func (tracker *CommandTelemetry) Finish(err error, exitCode int, panicked bool) {
	if tracker == nil || !tracker.localEnabled {
		return
	}
	if tracker.collectionAllowed {
		event := tracker.eventFromResult(err, exitCode, panicked)
		state, policyErr := telemetryevent.NewPolicyStore(tracker.route.PolicyRoot).Load()
		if policyErr == nil {
			if rate, selected := state.Policy.SampleRate(event); selected && telemetryevent.ShouldSample(event.CallID, rate) {
				event.SampleRate = rate
				event.SampleWeight = float64(telemetryevent.BasisPoints) / float64(rate)
				event.PolicyFromValidTCC = state.PolicyFromValidTCC
				_ = spawnTelemetryMaintenanceProcess(tracker.route.Profile, tracker.route.Key, &telemetryRecord{
					Event: event, Profile: tracker.route.Profile, Host: tracker.route.Host,
					IdentityKey: tracker.route.IdentityKey, Generation: tracker.consentGeneration,
				})
				return
			}
		}
	}
	maybeSpawnTelemetryMaintenance(tracker.route)
}

func (tracker *CommandTelemetry) eventFromResult(err error, exitCode int, panicked bool) telemetryevent.Event {
	event := tracker.baseEvent()
	snapshot := tracker.trace.Snapshot()
	if command := strings.TrimSpace(snapshot.Command); command != "" {
		event.Command = command
	}
	if len(snapshot.FlagNames) > 0 {
		event.FlagNames = snapshot.FlagNames
	}
	event.DurationMS = time.Since(tracker.started).Milliseconds()
	event.ExitCode = exitCode
	event.BackendReachability = snapshot.Reachability
	if snapshot.HostClass != "" {
		event.HostClass = snapshot.HostClass
	}
	if snapshot.CredentialSource != "" {
		event.CredentialSource = snapshot.CredentialSource
	}
	for _, ref := range snapshot.BackendRefs {
		event.BackendRefs = append(event.BackendRefs, telemetryevent.BackendRef{
			Service: ref.Service, Operation: ref.Operation, LogID: ref.LogID,
		})
	}
	if panicked {
		event.Success = false
		event.FinalOutcome = telemetryevent.OutcomePanic
		event.Category = telemetryevent.CategoryInternal
		event.ErrorCode = "INTERNAL_PANIC"
		event.FailureStage = resolveFailureStage(snapshot)
		return finalizeTelemetryEvent(event)
	}
	if err == nil || exitCode == 0 {
		event.Success = true
		event.FinalOutcome = telemetryevent.OutcomeSuccess
		event.BackendReachability = normalizeReachability(event.BackendReachability)
		return finalizeTelemetryEvent(event)
	}

	event.Success = false
	event.FailureStage = resolveFailureStage(snapshot)
	event.Category, event.ErrorCode, event.FinalOutcome = classifyTelemetryError(err)
	event.Retryable, event.HTTPStatus = telemetryErrorMetadata(err, event.FinalOutcome)
	if event.BackendReachability == "" || event.BackendReachability == telemetryevent.ReachabilityUnknown {
		event.BackendReachability = inferReachability(err)
		if event.BackendReachability == telemetryevent.ReachabilityUnknown && stageCannotReachBackend(event.FailureStage) {
			event.BackendReachability = telemetryevent.ReachabilityNo
		}
	}
	if logID := meerrors.BackendLogID(err); logID != "" {
		logID = extractLogID(logID)
		alreadyTraced := false
		for _, ref := range event.BackendRefs {
			if ref.LogID == logID {
				alreadyTraced = true
				break
			}
		}
		if !alreadyTraced {
			event.BackendRefs = append(event.BackendRefs, telemetryevent.BackendRef{
				Service: "meegle_mcp", Operation: "tools_call", LogID: logID,
			})
		}
		event.BackendReachability = telemetryevent.ReachabilityYes
	}
	return finalizeTelemetryEvent(event)
}

func finalizeTelemetryEvent(event telemetryevent.Event) telemetryevent.Event {
	event = event.Normalize()
	if event.Success {
		event.Fingerprint = ""
		return event
	}
	event.Fingerprint = telemetryFingerprint(event)
	return event
}

func telemetryFingerprint(event telemetryevent.Event) string {
	// Only allowlisted normalized fields enter the digest. Raw error text,
	// panic values, stack paths, argv and flag values are never inputs.
	digest := sha256.Sum256([]byte(event.Command + "\x00" + event.FailureStage + "\x00" + event.ErrorCode))
	return hex.EncodeToString(digest[:])
}

func (tracker *CommandTelemetry) baseEvent() telemetryevent.Event {
	command := tracker.fallbackCommand
	if command == "" {
		command = "unknown"
	}
	return telemetryevent.Event{
		SchemaVersion: telemetryevent.SchemaVersion,
		Kind:          telemetryevent.EventKindCommand, CallID: tracker.callID,
		OccurredAtMS: time.Now().UnixMilli(), ClientVersion: normalizedTelemetryClientVersion(tracker.clientVersion),
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		Command: command, FlagNames: append([]string(nil), tracker.fallbackFlags...),
		BackendReachability: telemetryevent.ReachabilityUnknown,
		HostClass:           tracker.hostClass, ClientType: telemetryevent.ClientTypeInteractiveCLI,
		CredentialSource: tracker.credentialSource,
	}
}

func normalizedTelemetryClientVersion(explicit string) string {
	if explicit != "" {
		if len(explicit) <= 64 && regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`).MatchString(explicit) {
			return explicit
		}
		return "unknown"
	}
	const prefix = "meegle-cli/"
	userAgent := mcpclient.DefaultUserAgent()
	if !strings.HasPrefix(userAgent, prefix) {
		return "unknown"
	}
	version := strings.TrimPrefix(userAgent, prefix)
	if len(version) == 0 || len(version) > 64 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`).MatchString(version) {
		return "unknown"
	}
	return version
}

func telemetryErrorMetadata(err error, outcome string) (retryable bool, httpStatus int) {
	var me *meerrors.MeegleError
	if frameworkerrors.SafeAs(err, &me) && me != nil {
		httpStatus = me.HTTPStatus
		if me.Retryable != nil {
			return me.IsRetryable(), httpStatus
		}
		if httpStatus != 0 {
			return httpStatus == 429 || httpStatus >= 500, httpStatus
		}
		return outcome == telemetryevent.OutcomeTransportError || me.ExitCode == 2, 0
	}
	if cli := frameworkerrors.As(err); cli != nil {
		return cli.IsRetryable(), 0
	}
	return false, 0
}

func classifyTelemetryError(err error) (category, code, outcome string) {
	if frameworkerrors.SafeIs(err, context.Canceled) || frameworkerrors.SafeIs(err, context.DeadlineExceeded) {
		return telemetryevent.CategoryCanceled, "CLIENT_CANCELED", telemetryevent.OutcomeCanceled
	}
	var me *meerrors.MeegleError
	if frameworkerrors.SafeAs(err, &me) && me != nil {
		code = sanitizeErrorCode(me.Code)
		switch {
		case strings.Contains(code, "CANCELLED"), strings.Contains(code, "CANCELED"):
			return telemetryevent.CategoryCanceled, code, telemetryevent.OutcomeCanceled
		case code == "CLIENT_FILE_SIGN_MISMATCH", code == "CLIENT_FILE_SIGN_UNVERIFIED":
			return telemetryevent.CategoryInternal, code, telemetryevent.OutcomeCLIPostprocessError
		case strings.HasPrefix(code, "AUTH_"), strings.HasPrefix(code, "OAUTH_"):
			return telemetryevent.CategoryAuth, code, telemetryevent.OutcomeAuthError
		case strings.Contains(code, "TIMEOUT"), strings.Contains(code, "UNAVAILABLE"):
			return telemetryevent.CategoryTransport, code, telemetryevent.OutcomeTransportError
		case me.HTTPStatus > 0, strings.HasPrefix(code, "SERVER_"), strings.HasPrefix(code, "HANDOFF_API_HTTP"):
			return telemetryevent.CategoryBackend, code, telemetryevent.OutcomeBackendError
		case strings.HasPrefix(code, "CLIENT_"), strings.HasPrefix(code, "CONFIG_"), strings.HasPrefix(code, "HOST_"):
			return telemetryevent.CategoryUser, code, telemetryevent.OutcomeClientError
		default:
			return telemetryevent.CategoryInternal, code, telemetryevent.OutcomeCLIPostprocessError
		}
	}
	if cli := frameworkerrors.As(err); cli != nil {
		code = sanitizeErrorCode(cli.Code)
		switch cli.Category {
		case frameworkerrors.CategoryAuth:
			return telemetryevent.CategoryAuth, code, telemetryevent.OutcomeAuthError
		case frameworkerrors.CategoryServer:
			return telemetryevent.CategoryTransport, code, telemetryevent.OutcomeTransportError
		case frameworkerrors.CategoryUser, frameworkerrors.CategoryConfig:
			return telemetryevent.CategoryUser, code, telemetryevent.OutcomeClientError
		default:
			return telemetryevent.CategoryInternal, code, telemetryevent.OutcomeCLIPostprocessError
		}
	}
	return telemetryevent.CategoryInternal, "INTERNAL_ERROR", telemetryevent.OutcomeCLIPostprocessError
}

func stageCannotReachBackend(stage string) bool {
	switch stage {
	case telemetryevent.StageCLIStart, telemetryevent.StagePreflight, telemetryevent.StageAuthPrepare,
		telemetryevent.StageRequestBuild, telemetryevent.StageLocalFileVerify:
		return true
	default:
		return false
	}
}

func inferReachability(err error) string {
	var me *meerrors.MeegleError
	if frameworkerrors.SafeAs(err, &me) && me != nil && (me.HTTPStatus > 0 || me.LogID != "" || me.Code == "SERVER_CALL_FAILED") {
		return telemetryevent.ReachabilityYes
	}
	return telemetryevent.ReachabilityUnknown
}

func resolveFailureStage(snapshot observability.Snapshot) string {
	if snapshot.Stage != "" {
		return snapshot.Stage
	}
	switch snapshot.Step {
	case "param_merge", "structured_flag_name_normalize", "runtime_validate":
		return telemetryevent.StagePreflight
	case "runtime_session":
		return telemetryevent.StageAuthPrepare
	case "runtime_executor":
		return telemetryevent.StageRequestSend
	case "output":
		return telemetryevent.StageLocalOutput
	default:
		return telemetryevent.StageCLIStart
	}
}

func normalizeReachability(value string) string {
	switch value {
	case telemetryevent.ReachabilityNo, telemetryevent.ReachabilityYes, telemetryevent.ReachabilityUnknown:
		return value
	default:
		return telemetryevent.ReachabilityUnknown
	}
}

func sanitizeErrorCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = regexp.MustCompile(`[^A-Z0-9_]+`).ReplaceAllString(code, "_")
	code = strings.Trim(code, "_")
	if code == "" {
		return "INTERNAL_ERROR"
	}
	if code[0] < 'A' || code[0] > 'Z' {
		code = "E_" + code
	}
	if len(code) > 64 {
		code = code[:64]
	}
	return code
}

func fallbackCommandAndFlags(args []string) (string, []string) {
	// Before the registry exists there is no safe way to distinguish a
	// command token from a separated flag value. Keep the fallback constant.
	return "unknown", nil
}

func knownFlagNames(root, cmd *cobra.Command, args []string) []string {
	seen := make(map[string]struct{})
	lookup := func(name string) *pflag.Flag {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			return flag
		}
		if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
			return flag
		}
		return root.PersistentFlags().Lookup(name)
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if lookup(name) != nil {
			seen[name] = struct{}{}
		}
	}
	flags := make([]string, 0, len(seen))
	for name := range seen {
		flags = append(flags, name)
	}
	sort.Strings(flags)
	return flags
}

func classifyHost(host string) string {
	host = strings.ToLower(sanitizeHost(host))
	switch {
	case host == "":
		return telemetryevent.HostClassUnresolved
	case host == "project.feishu.cn" || strings.HasSuffix(host, ".project.feishu.cn"):
		return telemetryevent.HostClassLarkProject
	case host == "meegle.com" || strings.HasSuffix(host, ".meegle.com"):
		return telemetryevent.HostClassMeegle
	case strings.Contains(host, "meego"):
		return telemetryevent.HostClassMeego
	default:
		return telemetryevent.HostClassCustom
	}
}

func randomTelemetryCallID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return sanitizeErrorCode(time.Now().UTC().Format("20060102T150405.000000000"))
}
