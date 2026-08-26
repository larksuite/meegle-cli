// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/prompt"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// ErrFirstRunSetupComplete tells the public process entry that interactive
// setup succeeded and the user should re-run the original command. It avoids
// calling os.Exit from library code embedded by an enterprise binary and is
// marked as successful control flow so the CLI runtime does not render it to
// stderr as a false error.
var ErrFirstRunSetupComplete = cliapp.SuccessfulExit(errors.New("first-run setup complete"))

// NormalizeHost extracts the host from a URL or returns the raw string.
// Exported so that sub-packages (e.g. commands) can reuse if needed.
func NormalizeHost(raw string) string {
	return sanitizeHost(raw)
}

// CheckFirstRun ensures the CLI is configured and authenticated.
// Skipped for config, auth, and help commands.
func CheckFirstRun(ctx context.Context, profileName string) error {
	return checkFirstRun(ctx, profileName, nil)
}

// CheckFirstRunWithIdentity continues first-run setup from the identity
// snapshot already resolved at CLI startup. It prevents stateful credential
// providers from being invoked twice during one terminal execution.
func CheckFirstRunWithIdentity(ctx context.Context, profileName string, identity ResolvedIdentity) error {
	return checkFirstRun(ctx, profileName, &identity)
}

func checkFirstRun(ctx context.Context, profileName string, identity *ResolvedIdentity) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if profileName == "" {
		var err error
		profileName, err = GetCurrentProfileName()
		if err != nil {
			return err
		}
	}

	cfg, err := LoadConfig(profileName)
	if err != nil {
		return err
	}

	var ident ResolvedIdentity
	if identity != nil {
		ident = *identity
	} else {
		ident, err = ResolveCLIIdentity(ctx, cfg, profileName)
		if err != nil {
			return err
		}
	}

	needsHost := ident.Host == ""
	needsAuth := needsHost || ident.Token == ""

	if !needsHost && !needsAuth {
		return nil
	}

	// Check if terminal is interactive
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return meerrors.NewClientError("FIRST_RUN_INTERACTIVE_REQUIRED",
			"Meegle CLI is not initialized and the current environment does not support interactive setup").
			WithSuggestion("run in an interactive terminal: meegle config init && meegle auth login")
	}

	fmt.Printf("\n  Welcome to Meegle CLI! First-time setup is required.\n\n")

	if needsHost {
		host, err := PromptHost()
		if err != nil {
			return err
		}
		cfg.Host = host
		if err := SaveProfileConfig(profileName, cfg); err != nil {
			return err
		}
		ident.Host = host
		fmt.Printf("\n  ✓ Configuration saved: host=%s\n\n", host)
	}

	if needsAuth {
		store := auth.CreateTokenStore(profileName)
		tokenManager := auth.NewTokenManager(store, ident.Host).WithHTTPClient(auth.HTTPClient(ctx))
		tokenData, err := auth.StartAuthCodeFlow(ctx, ident.Host)
		if err != nil {
			return err
		}
		if err := tokenManager.SaveToken(tokenData); err != nil {
			return err
		}
		fmt.Printf("\n  ✓ Login successful!\n\n")
	}

	fmt.Printf("  Setup complete. Please re-run your command.\n\n")
	return ErrFirstRunSetupComplete
}

var firstrunPresetHosts = []struct {
	name  string
	value string
}{
	{"Feishu Project (project.feishu.cn)", "project.feishu.cn"},
	{"Meegle (meegle.com)", "meegle.com"},
}

// PromptHost asks the user to select or enter a host interactively.
// Use ↑/↓ (or j/k) to navigate and Enter to confirm. The last entry lets
// the user type a custom domain.
func PromptHost() (string, error) {
	options := make([]string, 0, len(firstrunPresetHosts)+1)
	for _, h := range firstrunPresetHosts {
		options = append(options, h.name)
	}
	options = append(options, "Custom domain…")

	idx, err := prompt.SelectOne("  Select a site (use ↑/↓, Enter to confirm):", options)
	if err != nil {
		if errors.Is(err, prompt.ErrInterrupted) {
			return "", fmt.Errorf("site selection cancelled")
		}
		return "", err
	}

	if idx < len(firstrunPresetHosts) {
		return firstrunPresetHosts[idx].value, nil
	}

	domain, err := prompt.ReadLine("  Enter custom domain (e.g. example.com): ")
	if err != nil {
		return "", err
	}
	if domain == "" {
		return "", fmt.Errorf("no domain entered")
	}
	return sanitizeHost(domain), nil
}
