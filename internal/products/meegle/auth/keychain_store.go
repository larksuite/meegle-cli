// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"strings"
)

const serviceName = "meegle-cli"

type KeychainStore struct{ account string }

func NewKeychainStore(profile string) *KeychainStore {
	return &KeychainStore{account: profile}
}

func (s *KeychainStore) IsAvailable() bool {
	return exec.Command("security", "help").Run() == nil
}

func (s *KeychainStore) Load() (*TokenData, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", serviceName, "-a", s.account, "-w").Output()
	if err != nil {
		return nil, nil
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
	// Delete existing entry first (ignore error if not found)
	_ = exec.Command("security", "delete-generic-password", "-s", serviceName, "-a", s.account).Run()
	// Use -X with hex-encoded data passed as command arguments (not via -i
	// interactive mode) to avoid command injection through the account name.
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

func (s *SecretToolStore) IsAvailable() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

func (s *SecretToolStore) Load() (*TokenData, error) {
	out, err := exec.Command("secret-tool", "lookup", "service", serviceName, "account", s.account).Output()
	if err != nil {
		return nil, nil
	}
	var data TokenData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, nil
	}
	return &data, nil
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
