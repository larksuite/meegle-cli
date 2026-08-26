// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/prompt"
)

var presetHosts = []struct {
	name  string
	value string
}{
	{"Feishu Project (project.feishu.cn)", "project.feishu.cn"},
	{"Meegle (meegle.com)", "meegle.com"},
}

func NewConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	configCmd.AddCommand(newConfigInitCmd())
	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigSetCmd())
	configCmd.AddCommand(newConfigGetCmd())
	configCmd.AddCommand(newProfileCmd())

	return configCmd
}

func newConfigInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}
			host, _ := cmd.Flags().GetString("host")
			if err := saveProfileConfig(profileName, meegleConfig{Host: host}); err != nil {
				return err
			}
			fmt.Printf("✓ [%s] Configuration initialized: host=%s\n", profileName, host)
			return nil
		},
	}
	cmd.Flags().String("host", "meegle.com", "Host domain")
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}
			cfg, err := loadProfileConfig(profileName)
			if err != nil {
				return err
			}
			fmt.Printf("Profile: %s\n", profileName)
			b, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}
			if err := setProfileValue(profileName, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("✓ [%s] %s=%s\n", profileName, args[0], args[1])
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
			if profileName == "" {
				var err error
				profileName, err = getCurrentProfileName()
				if err != nil {
					return err
				}
			}
			val, err := getProfileValue(profileName, args[0])
			if err != nil {
				return err
			}
			if val == nil || val == "" {
				fmt.Println("(not set)")
			} else {
				fmt.Println(val)
			}
			return nil
		},
	}
}

func newProfileCmd() *cobra.Command {
	profileCmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage profiles",
	}

	profileCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := listProfiles()
			if err != nil {
				return err
			}
			if len(profiles.Names) == 0 {
				fmt.Println("No profiles found. Create one with: meegle config profile create <name>")
				return nil
			}
			for _, name := range profiles.Names {
				marker := ""
				if name == profiles.Current {
					marker = " (current)"
				}
				fmt.Printf("  %s%s\n", name, marker)
			}
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "current",
		Short: "Show current profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := getCurrentProfileName()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "use <name>",
		Short: "Switch to a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := listProfiles()
			if err != nil {
				return err
			}
			found := false
			for _, n := range profiles.Names {
				if n == args[0] {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("profile %q does not exist", args[0])
			}
			if err := setCurrentProfileName(args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Switched to %s\n", args[0])
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			profiles, err := listProfiles()
			if err != nil {
				return err
			}
			for _, n := range profiles.Names {
				if n == name {
					return fmt.Errorf("profile %q already exists", name)
				}
			}

			// Interactive host selection (matching TS behavior)
			host, err := promptHost()
			if err != nil {
				return err
			}

			if err := saveProfileConfig(name, meegleConfig{Host: host}); err != nil {
				return err
			}
			fmt.Printf("✓ Profile %q created (host=%s)\n", name, host)

			// Ask whether to login immediately
			shouldLogin, err := promptLogin()
			if err != nil {
				return err
			}
			if shouldLogin {
				ctx := cmd.Context()
				tokenData, err := auth.StartAuthCodeFlow(ctx, host)
				if err != nil {
					return err
				}
				store := auth.CreateTokenStore(name)
				tokenManager := auth.NewTokenManager(store, host)
				if err := tokenManager.SaveToken(tokenData); err != nil {
					return err
				}
				fmt.Println("✓ Login successful!")
			}

			if err := setCurrentProfileName(name); err != nil {
				return err
			}
			fmt.Printf("✓ Switched to %s\n", name)
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a profile and its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			profiles, err := listProfiles()
			if err != nil {
				return err
			}
			if name == profiles.Current {
				return fmt.Errorf("cannot delete the active profile %q; switch to another profile first", name)
			}
			store := auth.CreateTokenStore(name)
			_ = store.Clear()
			if err := deleteProfile(name); err != nil {
				return err
			}
			fmt.Printf("✓ Profile %q deleted\n", name)
			return nil
		},
	})

	return profileCmd
}

// promptHost asks the user to select or enter a host.
// Use ↑/↓ (or j/k) to navigate and Enter to confirm. The last entry lets
// the user type a custom domain.
func promptHost() (string, error) {
	options := make([]string, 0, len(presetHosts)+1)
	for _, h := range presetHosts {
		options = append(options, h.name)
	}
	options = append(options, "Custom domain…")

	idx, err := prompt.SelectOne("  Select a host (use ↑/↓, Enter to confirm):", options)
	if err != nil {
		if errors.Is(err, prompt.ErrInterrupted) {
			return "", fmt.Errorf("host selection cancelled")
		}
		return "", err
	}

	if idx < len(presetHosts) {
		return presetHosts[idx].value, nil
	}

	domain, err := prompt.ReadLine("  Enter custom domain (e.g. example.com): ")
	if err != nil {
		return "", err
	}
	if domain == "" {
		return "", fmt.Errorf("no host entered")
	}
	return normalizeHost(domain), nil
}

// promptLogin asks the user whether to login immediately after profile creation.
func promptLogin() (bool, error) {
	idx, err := prompt.SelectOne("  Log in now? (use ↑/↓, Enter to confirm):", []string{"Yes", "No"})
	if err != nil {
		return false, nil // default to no on cancel/error
	}
	return idx == 0, nil
}
