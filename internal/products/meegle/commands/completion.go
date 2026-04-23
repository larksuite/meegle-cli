// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewCompletionCmd generates shell completion scripts.
func NewCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|install]",
		Short: "Generate or install shell completion scripts",
		Long: `Generate or install shell completion scripts.

  # Auto-install (detects shell, writes to rc file)
  meegle completion install

  # Manual install
  source <(meegle completion zsh)    # Zsh
  source <(meegle completion bash)   # Bash
  meegle completion fish | source    # Fish`,
		ValidArgs:             []string{"bash", "zsh", "fish", "install"},
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return genZshCompletionCustom(root)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "install":
				return installCompletion()
			default:
				return fmt.Errorf("unsupported shell: %s; supported: bash, zsh, fish, install", args[0])
			}
		},
	}
	return cmd
}

// genZshCompletionCustom generates a zsh completion script and removes the "--" separator from _describe.
func genZshCompletionCustom(root *cobra.Command) error {
	var buf bytes.Buffer
	if err := root.GenZshCompletion(&buf); err != nil {
		return err
	}
	// Replace compadd -x "--" with a no-op (removes the separator line)
	script := strings.ReplaceAll(buf.String(), `compadd -x "--"`, `true`)
	_, err := os.Stdout.WriteString(script)
	return err
}

// completionSnippet is the marker line written to rc files.
const completionMarker = "# meegle shell completion"

// shellConfig maps shell name to rc file path and source snippet.
type shellConfig struct {
	rcFile  string
	snippet string
}

func detectShellConfigs() []shellConfig {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		return []shellConfig{{
			rcFile:  filepath.Join(home, ".zshrc"),
			snippet: "source <(meegle completion zsh)\ncompdef _meegle meegle",
		}}
	case "bash":
		// macOS uses .bash_profile, Linux uses .bashrc
		rc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(filepath.Join(home, ".bash_profile")); err == nil {
			rc = filepath.Join(home, ".bash_profile")
		}
		return []shellConfig{{
			rcFile:  rc,
			snippet: `source <(meegle completion bash)`,
		}}
	case "fish":
		return []shellConfig{{
			rcFile:  filepath.Join(home, ".config", "fish", "config.fish"),
			snippet: `meegle completion fish | source`,
		}}
	default:
		return nil
	}
}

func installCompletion() error {
	configs := detectShellConfigs()
	if len(configs) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "(not detected)"
		}
		return fmt.Errorf("cannot auto-install completion: unsupported shell %s\n\nPlease add manually to your shell config file:\n  Zsh:  source <(meegle completion zsh)\n  Bash: source <(meegle completion bash)\n  Fish: meegle completion fish | source", shell)
	}

	for _, cfg := range configs {
		if alreadyInstalled(cfg.rcFile) {
			fmt.Printf("✓ Completion already installed in %s\n", cfg.rcFile)
			continue
		}
		if err := appendToFile(cfg.rcFile, cfg.snippet); err != nil {
			return fmt.Errorf("failed to write %s: %w", cfg.rcFile, err)
		}
		fmt.Printf("✓ Completion written to %s\n", cfg.rcFile)
	}

	fmt.Println("\nRestart your terminal or run the source command to activate.")
	return nil
}

func alreadyInstalled(rcFile string) bool {
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

func appendToFile(path, snippet string) error {
	// Ensure parent directory exists (for fish config)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n%s\n%s\n", completionMarker, snippet)
	return err
}
