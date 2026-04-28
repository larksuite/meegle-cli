// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	meegle "github.com/larksuite/meegle-cli/internal/products/meegle"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/output/formatter"
	"github.com/larksuite/meegle-cli/pkg/integrations/formatting"
)

// meegleConfig is a local alias for the parent package's MeegleConfig type.
type meegleConfig = meegle.MeegleConfig

// Thin wrappers around the meegle package config functions so that
// commands in this package do not need a direct import alias everywhere.

func getCurrentProfileName() (string, error) {
	return meegle.GetCurrentProfileName()
}

func loadConfig(profile string) (meegle.MeegleConfig, error) {
	return meegle.LoadConfig(profile)
}

func loadProfileConfig(profile string) (meegle.MeegleConfig, error) {
	return meegle.LoadProfileConfig(profile)
}

func saveProfileConfig(profile string, cfg meegle.MeegleConfig) error {
	return meegle.SaveProfileConfig(profile, cfg)
}

func setProfileValue(profile, key, value string) error {
	return meegle.SetProfileValue(profile, key, value)
}

func getProfileValue(profile, key string) (any, error) {
	return meegle.GetProfileValue(profile, key)
}

func listProfiles() (meegle.ProfileList, error) {
	return meegle.ListProfiles()
}

func setCurrentProfileName(name string) error {
	return meegle.SetCurrentProfileName(name)
}

func deleteProfile(name string) error {
	return meegle.DeleteProfile(name)
}

// invalidateToolCache wipes the per-profile tool cache so the next CLI
// invocation re-discovers commands. Called after a successful auth login
// because the new identity may see a different command set than the one
// cached for the previous (or expired) token. Errors are best-effort:
// a stale cache only delays the next refresh, it does not break login.
func invalidateToolCache(profile string) {
	cache := meegle.NewToolCache(meegle.GetCacheDir(), profile, meegle.DefaultTTL)
	_ = cache.Clear()
}

// renderPayload formats data according to the requested mode and returns the
// output as a string (trailing newline stripped so the caller can use
// fmt.Println without doubling). Used by auth/config/etc. commands that do
// not go through the pipeline's OutputStep.
func renderPayload(data any, mode string) (string, error) {
	if mode == "" {
		mode = "json"
	}
	f := formatting.NewComposite(formatter.Standard{}, formatting.DefaultTableView())
	out, err := f.Format(data, &frameworkoutput.FormatOptions{Mode: mode})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// printCompletionHint prompts the user to enable shell completion after login (first time only).
func printCompletionHint() {
	// Skip if completion is already installed
	configs := detectShellConfigs()
	for _, cfg := range configs {
		if completionInstalledIn(cfg.rcFile) {
			return
		}
	}
	fmt.Println("\nTip: Run meegle completion install to enable command auto-completion")
}

func completionInstalledIn(rcFile string) bool {
	f, err := os.Open(rcFile)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "meegle completion") {
			return true
		}
	}
	return false
}
