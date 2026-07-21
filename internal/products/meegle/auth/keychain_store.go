// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

const serviceName = "meegle-cli"

// errSecItemNotFound is the macOS Security framework's exit code for
// `security find-generic-password` when the item is not present. We treat it
// as a normal "no token stored yet" signal and surface it to the caller as
// (nil, nil); other exit codes (sandboxed environment, locked keychain, etc.)
// are returned as real errors so FallbackStore can switch to the file store.
const errSecItemNotFound = 44

type KeychainStore struct{ account string }

func NewKeychainStore(profile string) *KeychainStore {
	return &KeychainStore{account: profile}
}

func (s *KeychainStore) Load() (*TokenData, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", serviceName, "-a", s.account, "-w").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == errSecItemNotFound {
			return nil, nil
		}
		return nil, err
	}
	var data TokenData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, nil
	}
	return &data, nil
}

func (s *KeychainStore) Save(data *TokenData) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// Use -X with hex-encoded data passed as command arguments (not via -i
	// interactive mode) to avoid command injection through the account name.
	// -U updates an existing item atomically, so do not delete it first: a
	// concurrent reader must never observe a temporary "no credentials" gap.
	hexData := hex.EncodeToString(b)
	cmd := exec.Command("security",
		"add-generic-password",
		"-s", serviceName,
		"-a", s.account,
		"-X", hexData,
		"-U",
	)
	return cmd.Run()
}

func (s *KeychainStore) Clear() error {
	_ = exec.Command("security", "delete-generic-password", "-s", serviceName, "-a", s.account).Run()
	return nil
}

type SecretToolStore struct{ account string }

func NewSecretToolStore(profile string) *SecretToolStore {
	return &SecretToolStore{account: profile}
}

func (s *SecretToolStore) Load() (*TokenData, error) {
	out, err := exec.Command("secret-tool", "lookup", "service", serviceName, "account", s.account).Output()
	if err != nil {
		if isSecretToolLookupNotFound(out, err) {
			return nil, nil
		}
		return nil, err
	}
	var data TokenData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, nil
	}
	return &data, nil
}

// isSecretToolLookupNotFound reports whether the result of
// `secret-tool lookup` should be treated as "item absent from keyring"
// rather than a real failure. libsecret's tool exits 1 in *both* cases:
// the `value == NULL && error == NULL` path (item not present) writes
// nothing to either stream, while genuine failures (no dbus session,
// org.freedesktop.secrets unavailable, locked keyring, etc.) always
// write a diagnostic line to stderr first. Distinguishing on empty
// stderr keeps FallbackStore's sticky-failure bit from flipping on the
// very first Load of a fresh user, when the keyring is reachable but
// has no meegle-cli entry yet.
func isSecretToolLookupNotFound(out []byte, err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 1 && len(out) == 0 && len(exitErr.Stderr) == 0
}

func (s *SecretToolStore) Save(data *TokenData) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	cmd := exec.Command("secret-tool", "store", "--label=meegle-cli", "service", serviceName, "account", s.account)
	cmd.Stdin = strings.NewReader(string(b))
	return cmd.Run()
}

func (s *SecretToolStore) Clear() error {
	_ = exec.Command("secret-tool", "clear", "service", serviceName, "account", s.account).Run()
	return nil
}
