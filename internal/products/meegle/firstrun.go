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
	"github.com/larksuite/meegle-cli/internal/products/meegle/prompt"
)

// NormalizeHost extracts the host from a URL or returns the raw string.
// Exported so that sub-packages (e.g. commands) can reuse if needed.
func NormalizeHost(raw string) string {
	return sanitizeHost(raw)
}

// CheckFirstRun ensures the CLI is configured and authenticated.
// Skipped for config, auth, and help commands.
func CheckFirstRun(profileName string) error {
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

	ident, err := ResolveIdentity(cfg, profileName)
	if err != nil {
		return err
	}

	needsHost := ident.Host == ""
	needsAuth := needsHost || ident.Token == ""

	if !needsHost && !needsAuth {
		return nil
	}

	// Check if terminal is interactive
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "\n  Error: Meegle CLI is not initialized and the current environment does not support interactive setup.\n\n")
		fmt.Fprintln(os.Stderr, "  Please run in an interactive terminal: meegle config init && meegle auth login")
		os.Exit(1)
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
		ctx := context.Background()
		store := auth.CreateTokenStore(profileName)
		tokenManager := auth.NewTokenManager(store, ident.Host)
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
	os.Exit(0)
	return nil
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
