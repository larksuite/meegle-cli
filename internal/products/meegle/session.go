// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"net/http"
	"os"

	"golang.org/x/term"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

// SessionStep resolves one protocol-neutral command identity. CommandRuntime
// implementations decide whether the command needs a session and how to
// project it into backend-specific client configuration.
type SessionStep struct {
	Identity   *ResolvedIdentity
	HTTPClient *http.Client

	isInteractive func() bool
	checkFirstRun func(context.Context, string, ResolvedIdentity) error
}

func (s *SessionStep) Name() string { return "session" }

func (s *SessionStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || len(state.Parsed.FullPath) == 0 {
		return nil
	}
	if isDryRunFlag(state.Parsed) {
		return nil
	}
	topCmd := state.Parsed.FullPath[0]
	skip := map[string]bool{
		"auth": true, "config": true, "help": true,
		"version": true, "inspect": true, "completion": true,
	}
	if skip[topCmd] {
		return nil
	}

	if state.OutputConfig == nil {
		state.OutputConfig = make(map[string]any)
	}

	profileName, _ := state.Parsed.Flags["profile"].(string)
	if profileName == "" {
		profileName, _ = GetCurrentProfileName()
	}

	var ident ResolvedIdentity
	if s.Identity != nil {
		ident = *s.Identity
	} else {
		cfg, err := LoadConfig(profileName)
		if err != nil {
			return err
		}
		ident, err = ResolveIdentity(cfg, profileName)
		if err != nil {
			return meerrors.NewClientError("CONFIG_ENV_UNRESOLVED", err.Error()).
				WithSuggestion("set the referenced environment variable, or run 'meegle config set' to update the profile")
		}
	}
	effectiveProfileName := profileName
	if ident.ProfileName != "" {
		effectiveProfileName = ident.ProfileName
	}
	interactive := isInteractiveTerminal
	if s.isInteractive != nil {
		interactive = s.isInteractive
	}
	firstRun := CheckFirstRunWithIdentity
	if s.checkFirstRun != nil {
		firstRun = s.checkFirstRun
	}

	if ident.Host == "" {
		if !interactive() {
			return meerrors.NewClientError("HOST_NOT_CONFIGURED", "host not configured").
				WithSuggestion(meerrors.HintNonInteractiveSetup)
		}
		return firstRun(ctx, effectiveProfileName, ident)
	}

	if ident.Token == "" {
		if !interactive() {
			return meerrors.NewClientError("AUTH_REQUIRED", "authentication required").
				WithSuggestion(meerrors.HintDeviceCodeRequired)
		}
		return firstRun(ctx, effectiveProfileName, ident)
	}

	state.OutputConfig["session.host"] = ident.Host
	state.OutputConfig["session.token"] = ident.Token
	state.OutputConfig["session.headers"] = ident.Headers
	state.OutputConfig["session.identity_source"] = ident.Source
	state.OutputConfig["session.access_token_header"] = ident.AccessTokenHeader
	state.OutputConfig["session.profile"] = profileName
	if s.HTTPClient != nil {
		state.OutputConfig["session.http_client"] = s.HTTPClient
	}
	userAgent := ident.MCPUserAgent
	if userAgent == "" && ident.UserAgentCaller != "" {
		userAgent = mcpclient.BuildUserAgent(ident.UserAgentCaller)
	}
	if userAgent != "" {
		state.OutputConfig["session.user_agent"] = userAgent
	}
	if ident.Source == SourceStore {
		state.OutputConfig["session.store"] = ident.Store
		state.OutputConfig["session.token_manager"] = ident.TokenManager
	}
	return nil
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
