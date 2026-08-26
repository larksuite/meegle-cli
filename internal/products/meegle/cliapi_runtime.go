// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/cliapiclient"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

const (
	aiHandoffEnv               = "MEEGLE_AI_HANDOFF"
	aiHandoffDisabledValue     = "disabled"
	aiHandoffLocalDisabledCode = "LOCAL_DISABLED"
	aiHandoffLocalDisabledMsg  = "AI handoff is disabled in this installation environment"
)

// cliCommandAPI contains only server operations exposed through CLI commands.
// The CLI's own remote configuration is loaded separately by CLIConfigProvider.
type cliCommandAPI interface {
	CreateLink(context.Context, cliapiclient.CreateLinkRequest) (*cliapiclient.CreateLinkResponse, error)
	UpdatePreference(context.Context, string) (*cliapiclient.PreferenceUpdateResult, error)
}

// newCLIConfigCacheFromState builds the per-profile CLI configuration cache. It
// prefers the profile SessionStep already resolved (session.profile in
// OutputConfig) and only falls back to resolving again when that is absent
// (e.g. SDK-injected paths that bypass SessionStep). Resolution is cheap and
// side-effect free, so the fallback is safe.
func newCLIConfigCacheFromState(state *pipeline.PipelineContext) *CLIConfigCache {
	return NewCLIConfigCache(GetCacheDir(), resolveCacheProfile(state), DefaultCLIConfigTTL)
}

// resolveCacheProfile returns the profile name to key the cache on, reusing the
// one SessionStep resolved when available.
func resolveCacheProfile(state *pipeline.PipelineContext) string {
	if state != nil && state.OutputConfig != nil {
		if profile, _ := state.OutputConfig["session.profile"].(string); profile != "" {
			return profile
		}
	}
	profileName := ""
	if state != nil && state.Parsed != nil {
		profileName, _ = state.Parsed.Flags["profile"].(string)
	}
	if profileName == "" {
		profileName, _ = GetCurrentProfileName()
	}
	return profileName
}

// CLIAPIRuntime executes locally registered commands against the Meegle CLI
// HTTP API. HandlerRef selects the domain handler after the command runtime has
// already been resolved to the CLI API backend.
type CLIAPIRuntime struct {
	Session               *SessionStep
	ClientFactory         func(*pipeline.PipelineContext) cliCommandAPI
	ConfigProviderFactory func(*pipeline.PipelineContext) *CLIConfigProvider
}

func (runtime *CLIAPIRuntime) Validate(state *pipeline.PipelineContext) error {
	return validateMeegleCommand(state)
}

func (runtime *CLIAPIRuntime) PrepareSession(ctx context.Context, state *pipeline.PipelineContext) error {
	if state != nil {
		if injected, _ := state.OutputConfig["session.injected"].(bool); injected {
			return nil
		}
	}
	if isHandoffHandler(state) && isAIHandoffLocallyDisabled() {
		return nil
	}
	return runtime.sessionStep().Execute(ctx, state)
}

func (runtime *CLIAPIRuntime) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	switch state.Parsed.Node.HandlerRef {
	case handlerHandoffAvailability, handlerHandoffCreateLink:
		return runtime.executeHandoff(ctx, state)
	case handlerPreferenceAuto, handlerPreferenceAsk, handlerPreferenceOff:
		return runtime.executePreference(ctx, state)
	default:
		return meerrors.NewClientError("CLIENT_MISCONFIGURED", "unknown CLI API command handler")
	}
}

func (runtime *CLIAPIRuntime) executeHandoff(ctx context.Context, state *pipeline.PipelineContext) error {
	if isAIHandoffLocallyDisabled() {
		switch state.Parsed.Node.HandlerRef {
		case handlerHandoffAvailability:
			state.Result = localResult(&cliapiclient.Availability{
				Available:  false,
				RejectCode: aiHandoffLocalDisabledCode,
				RejectMsg:  aiHandoffLocalDisabledMsg,
			}, state)
			return nil
		case handlerHandoffCreateLink:
			state.Result = localResult(&cliapiclient.CreateLinkResponse{
				Available:  false,
				RejectCode: aiHandoffLocalDisabledCode,
				RejectMsg:  aiHandoffLocalDisabledMsg,
			}, state)
			return nil
		}
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}

	switch state.Parsed.Node.HandlerRef {
	case handlerHandoffAvailability:
		if isDryRunFlag(state.Parsed) {
			setLocalDryRunResult(state, http.MethodGet, "/goapi/v5/meeglecli/config", nil)
			return nil
		}
		response, err := runtime.configProvider(state).Get(ctx)
		if err != nil {
			return err
		}
		if response == nil {
			return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", "CLI config API returned an empty response")
		}
		state.Result = localResultWithLogID(&response.HandoffSuggestion, state, response.LogID)
		return nil
	case handlerHandoffCreateLink:
		request, err := buildCreateLinkRequest(state.Values)
		if err != nil {
			return err
		}
		if isDryRunFlag(state.Parsed) {
			setLocalDryRunResult(state, http.MethodPost, "/goapi/v5/meeglecli/handoff/link", request)
			return nil
		}
		response, err := runtime.client(state).CreateLink(ctx, request)
		if err != nil {
			return err
		}
		if response == nil {
			return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", "handoff API returned an empty response")
		}
		if !response.Available {
			// The server is the authority: a create-link rejection means any
			// cached availability=true is stale (e.g. the user disabled handoff
			// on another device). Drop it so the next availability call refetches
			// instead of serving the stale allow for the rest of the TTL.
			_ = runtime.configProvider(state).Clear()
			return handoffRejectedError(response.RejectCode, response.RejectMsg, response.LogID)
		}
		if strings.TrimSpace(response.URL) == "" {
			return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", "handoff API returned no URL")
		}
		loginHost, err := activeLoginHost(state)
		if err != nil {
			return err
		}
		rewrittenURL, err := replaceURLHost(response.URL, loginHost)
		if err != nil {
			return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", "handoff API returned an invalid URL: "+err.Error())
		}
		response.URL = rewrittenURL
		state.Result = localResultWithLogID(response, state, response.LogID)
		return nil
	default:
		return meerrors.NewClientError("CLIENT_MISCONFIGURED", "unknown handoff command handler")
	}
}

func isAIHandoffLocallyDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(aiHandoffEnv)), aiHandoffDisabledValue)
}

func activeLoginHost(state *pipeline.PipelineContext) (string, error) {
	if state == nil || state.OutputConfig == nil {
		return "", meerrors.NewClientError("CLIENT_MISCONFIGURED", "active login host is unavailable")
	}
	for _, key := range []string{"session.host", "mcp.host"} {
		rawHost, _ := state.OutputConfig[key].(string)
		host := strings.TrimSpace(sanitizeHost(strings.TrimSpace(rawHost)))
		if host == "" {
			continue
		}
		parsed, err := url.Parse("//" + host)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", meerrors.NewClientError("CLIENT_MISCONFIGURED", "active login host is invalid")
		}
		return parsed.Host, nil
	}
	return "", meerrors.NewClientError("CLIENT_MISCONFIGURED", "active login host is unavailable")
}

func replaceURLHost(rawURL, host string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("URL must be absolute")
	}
	parsed.Host = host
	return parsed.String(), nil
}

func (runtime *CLIAPIRuntime) client(state *pipeline.PipelineContext) cliCommandAPI {
	if runtime != nil && runtime.ClientFactory != nil {
		return runtime.ClientFactory(state)
	}
	return newCLIAPIClientFromState(state)
}

func (runtime *CLIAPIRuntime) configProvider(state *pipeline.PipelineContext) *CLIConfigProvider {
	if runtime != nil && runtime.ConfigProviderFactory != nil {
		if provider := runtime.ConfigProviderFactory(state); provider != nil {
			return provider
		}
	}
	return NewCLIConfigProvider(newCLIAPIClientFromState(state), newCLIConfigCacheFromState(state))
}

func (runtime *CLIAPIRuntime) executePreference(ctx context.Context, state *pipeline.PipelineContext) error {
	var (
		response *cliapiclient.PreferenceUpdateResult
		err      error
		// mutated tracks whether this call changed the preference, so the local
		// CLI config cache is invalidated only after a successful write.
		mutated bool
	)
	switch state.Parsed.Node.HandlerRef {
	case handlerPreferenceAuto:
		if isDryRunFlag(state.Parsed) {
			setLocalDryRunResult(state, http.MethodPost, "/goapi/v5/meeglecli/preference",
				map[string]any{"preferences": []map[string]string{{"type": "handoff_suggestions", "payload": `{"mode":"auto"}`}}})
			return nil
		}
		response, err = runtime.client(state).UpdatePreference(ctx, "auto")
		mutated = true
	case handlerPreferenceAsk:
		if isDryRunFlag(state.Parsed) {
			setLocalDryRunResult(state, http.MethodPost, "/goapi/v5/meeglecli/preference",
				map[string]any{"preferences": []map[string]string{{"type": "handoff_suggestions", "payload": `{"mode":"ask"}`}}})
			return nil
		}
		response, err = runtime.client(state).UpdatePreference(ctx, "ask")
		mutated = true
	case handlerPreferenceOff:
		if isDryRunFlag(state.Parsed) {
			setLocalDryRunResult(state, http.MethodPost, "/goapi/v5/meeglecli/preference",
				map[string]any{"preferences": []map[string]string{{"type": "handoff_suggestions", "payload": `{"mode":"off"}`}}})
			return nil
		}
		response, err = runtime.client(state).UpdatePreference(ctx, "off")
		mutated = true
	default:
		return meerrors.NewClientError("CLIENT_MISCONFIGURED", "unknown preference command handler")
	}
	if err != nil {
		return err
	}
	if response == nil {
		return meerrors.NewServerError("HANDOFF_API_INVALID_RESPONSE", "preference API returned an empty response")
	}
	// A successful mode update actively invalidates the cached CLI config
	// so the local preference change takes effect immediately, forcing the next
	// availability call to refetch instead of serving a decision from before the
	// change. A failed Clear is non-fatal: at worst the stale entry expires on
	// its own within the TTL.
	if mutated {
		_ = runtime.configProvider(state).Clear()
	}
	state.Result = localResultWithLogID(response, state, response.LogID)
	return nil
}

func (runtime *CLIAPIRuntime) sessionStep() *SessionStep {
	if runtime != nil && runtime.Session != nil {
		return runtime.Session
	}
	return &SessionStep{}
}

func isHandoffHandler(state *pipeline.PipelineContext) bool {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return false
	}
	switch state.Parsed.Node.HandlerRef {
	case handlerHandoffAvailability, handlerHandoffCreateLink:
		return true
	default:
		return false
	}
}

func newCLIAPIClientFromState(state *pipeline.PipelineContext) *cliapiclient.Client {
	config := map[string]any{}
	if state != nil && state.OutputConfig != nil {
		config = state.OutputConfig
	}
	host, _ := config["session.host"].(string)
	token, _ := config["session.token"].(string)
	headers, _ := config["session.headers"].(map[string]string)
	authHeader, _ := config["session.access_token_header"].(string)
	userAgent, _ := config["session.user_agent"].(string)
	tokenManager, _ := config["session.token_manager"].(*auth.TokenManager)
	httpClient, _ := config["session.http_client"].(*http.Client)

	httpHeaders := make(http.Header)
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") || (authHeader != "" && strings.EqualFold(key, authHeader)) {
			continue
		}
		httpHeaders.Set(key, value)
	}
	tokenFunc := func() (string, error) { return token, nil }
	var refreshFunc func() error
	if tokenManager != nil {
		tokenFunc = tokenManager.GetToken
		refreshFunc = tokenManager.TryRefresh
	}
	if userAgent == "" {
		userAgent = mcpclient.DefaultUserAgent()
	}
	return cliapiclient.New(cliapiclient.Config{
		BaseURL:    GetAPIBaseURL(MeegleConfig{Host: host}),
		Token:      tokenFunc,
		Refresh:    refreshFunc,
		Headers:    httpHeaders,
		AuthHeader: authHeader,
		UserAgent:  userAgent,
		HTTPClient: httpClient,
	})
}

func buildCreateLinkRequest(values map[string]any) (cliapiclient.CreateLinkRequest, error) {
	query := ""
	if value, ok := values["query"]; ok && value != nil {
		query = strings.TrimSpace(fmt.Sprint(value))
	}
	if query == "" {
		return cliapiclient.CreateLinkRequest{}, meerrors.NewClientError(
			"CLIENT_INVALID_QUERY", "invalid --query: value is required")
	}
	contextItems, err := parseRelatedContext(values["related-context"])
	if err != nil {
		return cliapiclient.CreateLinkRequest{}, meerrors.NewClientError(
			"CLIENT_INVALID_RELATED_CONTEXT", "invalid --related-context: "+err.Error())
	}
	return cliapiclient.CreateLinkRequest{UserQuery: query, RelatedContext: contextItems}, nil
}

func parseRelatedContext(value any) ([]cliapiclient.RelatedContext, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return decodeRelatedContextJSON([]byte(typed))
	case []string:
		var result []cliapiclient.RelatedContext
		for _, item := range typed {
			decoded, err := decodeRelatedContextJSON([]byte(item))
			if err != nil {
				return nil, err
			}
			result = append(result, decoded...)
		}
		return result, nil
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode structured value: %w", err)
		}
		return decodeRelatedContextJSON(payload)
	}
}

func decodeRelatedContextJSON(payload []byte) ([]cliapiclient.RelatedContext, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []cliapiclient.RelatedContext
		if err := decodeStrictJSON(trimmed, &items); err != nil {
			return nil, err
		}
		return normalizeRelatedContextItems(items)
	}
	var item cliapiclient.RelatedContext
	if err := decodeStrictJSON(trimmed, &item); err != nil {
		return nil, err
	}
	return normalizeRelatedContextItems([]cliapiclient.RelatedContext{item})
}

// decodeStrictJSON rejects unknown fields and trailing JSON values so callers
// fail fast on typos or malformed --related-context input.
func decodeStrictJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func normalizeRelatedContextItems(items []cliapiclient.RelatedContext) ([]cliapiclient.RelatedContext, error) {
	for index := range items {
		if err := normalizeRelatedContextItem(&items[index]); err != nil {
			return nil, fmt.Errorf("related_context[%d]: %w", index, err)
		}
	}
	return items, nil
}

func normalizeRelatedContextItem(item *cliapiclient.RelatedContext) error {
	payloadCount := 0
	for _, set := range []bool{
		item.Project != nil, item.WorkItem != nil,
		item.View != nil, item.MeasureChart != nil,
	} {
		if set {
			payloadCount++
		}
	}
	if payloadCount != 1 {
		return fmt.Errorf("exactly one context payload is required, got %d", payloadCount)
	}

	switch item.Type {
	case cliapiclient.ContextTypeProject:
		if item.Project == nil {
			return fmt.Errorf("type Project requires project payload")
		}
		item.Project.ProjectKey = strings.TrimSpace(item.Project.ProjectKey)
		return requireContextFields(contextField{"project_key", item.Project.ProjectKey})
	case cliapiclient.ContextTypeWorkItem:
		if item.WorkItem == nil {
			return fmt.Errorf("type WorkItem requires work_item payload")
		}
		item.WorkItem.ProjectKey = strings.TrimSpace(item.WorkItem.ProjectKey)
		item.WorkItem.WorkItemTypeKey = strings.TrimSpace(item.WorkItem.WorkItemTypeKey)
		item.WorkItem.WorkItemID = strings.TrimSpace(item.WorkItem.WorkItemID)
		return requireContextFields(
			contextField{"project_key", item.WorkItem.ProjectKey},
			contextField{"work_item_type_key", item.WorkItem.WorkItemTypeKey},
			contextField{"work_item_id", item.WorkItem.WorkItemID},
		)
	case cliapiclient.ContextTypeView:
		if item.View == nil {
			return fmt.Errorf("type View requires view payload")
		}
		item.View.ProjectKey = strings.TrimSpace(item.View.ProjectKey)
		item.View.WorkItemTypeKey = strings.TrimSpace(item.View.WorkItemTypeKey)
		item.View.ViewID = strings.TrimSpace(item.View.ViewID)
		return requireContextFields(
			contextField{"project_key", item.View.ProjectKey},
			contextField{"view_id", item.View.ViewID},
		)
	case cliapiclient.ContextTypeMeasureChart:
		if item.MeasureChart == nil {
			return fmt.Errorf("type MeasureChart requires measure_chart payload")
		}
		item.MeasureChart.ProjectKey = strings.TrimSpace(item.MeasureChart.ProjectKey)
		item.MeasureChart.ChartID = strings.TrimSpace(item.MeasureChart.ChartID)
		return requireContextFields(
			contextField{"project_key", item.MeasureChart.ProjectKey},
			contextField{"chart_id", item.MeasureChart.ChartID},
		)
	default:
		return fmt.Errorf("unsupported context type %d", item.Type)
	}
}

type contextField struct {
	name  string
	value string
}

func requireContextFields(fields ...contextField) error {
	for _, field := range fields {
		if field.value == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	return nil
}

func handoffRejectedError(code string, message string, logID string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "UNKNOWN"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = code
	}
	err := meerrors.NewClientError("HANDOFF_REJECTED", "AI handoff was rejected: "+message).WithLogID(logID)
	if code == "USER_DISABLED" {
		return err.WithSuggestion("run 'meegle preference handoff auto' or 'meegle preference handoff ask' to enable recommendations")
	}
	return err
}

func localResult(data any, state *pipeline.PipelineContext) *executor.RawResult {
	return &executor.RawResult{
		Data: data,
		Metadata: map[string]any{
			"tool":    state.Parsed.Node.HandlerRef,
			"command": strings.Join(state.Parsed.FullPath, " "),
			"backend": state.Parsed.Node.Meta.Tags[TagExecutorKind],
		},
	}
}

func localResultWithLogID(data any, state *pipeline.PipelineContext, logID string) *executor.RawResult {
	result := localResult(data, state)
	if logID = strings.TrimSpace(logID); logID != "" {
		result.Metadata["logid"] = logID
	}
	return result
}

func setLocalDryRunResult(state *pipeline.PipelineContext, method, path string, body any) {
	data := map[string]any{"method": method, "path": path, "dry_run": true}
	if body != nil {
		data["body"] = body
	}
	state.Result = localResult(data, state)
}
