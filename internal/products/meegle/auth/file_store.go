// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

type encryptedFile struct {
	Salt string `json:"salt"`
	IV   string `json:"iv"`
	Tag  string `json:"tag"`
	Data string `json:"data"`
}

type FileStore struct {
	dir      string
	filename string
}

func NewFileStore(dir string, profile string) *FileStore {
	if dir == "" {
		dir = defaultConfigDir()
	}
	filename := "credentials.enc"
	if profile != "" && profile != "default" {
		filename = "credentials-" + profile + ".enc"
	}
	return &FileStore{dir: dir, filename: filename}
}

func defaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meegle")
}

func (s *FileStore) filePath() string { return filepath.Join(s.dir, s.filename) }

const machineKeyFile = ".machine-key"

func getOrCreateMachineKey(dir string) (string, error) {
	keyPath := filepath.Join(dir, machineKeyFile)
	data, err := os.ReadFile(keyPath)
	if err == nil {
		return string(data), nil
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	keyHex := hex.EncodeToString(key)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(keyPath, []byte(keyHex), 0600); err != nil {
		return "", err
	}
	return keyHex, nil
}

func deriveKey(salt []byte, machineKey string) []byte {
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = "unknown"
	}
	material := hostname + ":" + username + ":" + machineKey + ":meegle-cli"
	return pbkdf2.Key([]byte(material), salt, 100000, 32, sha256.New)
}

func (s *FileStore) Load() (*TokenData, error) {
	data, err := os.ReadFile(s.filePath())
	if err != nil {
		return nil, nil
	}
	machineKey, err := getOrCreateMachineKey(s.dir)
	if err != nil {
		return nil, nil
	}
	var ef encryptedFile
	if err := json.Unmarshal(data, &ef); err != nil {
		return nil, nil
	}
	salt, _ := hex.DecodeString(ef.Salt)
	iv, _ := hex.DecodeString(ef.IV)
	tag, _ := hex.DecodeString(ef.Tag)
	encrypted, _ := hex.DecodeString(ef.Data)
	key := deriveKey(salt, machineKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, nil
	}
	ciphertext := append(encrypted, tag...)
	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, nil
	}
	var td TokenData
	if err := json.Unmarshal(plaintext, &td); err != nil {
		return nil, nil
	}
	return &td, nil
}

func (s *FileStore) Save(data *TokenData) error {
	machineKey, err := getOrCreateMachineKey(s.dir)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	iv := make([]byte, 12)
	_, _ = rand.Read(iv)
	key := deriveKey(salt, machineKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(data)
	if err != nil {
		return err
	}
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	tagSize := gcm.Overhead()
	ciphertext := sealed[:len(sealed)-tagSize]
	tagBytes := sealed[len(sealed)-tagSize:]
	ef := encryptedFile{
		Salt: hex.EncodeToString(salt), IV: hex.EncodeToString(iv),
		Tag: hex.EncodeToString(tagBytes), Data: hex.EncodeToString(ciphertext),
	}
	b, err := json.MarshalIndent(ef, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(s.filePath(), b, 0600)
}

func (s *FileStore) Clear() error { os.Remove(s.filePath()); return nil }
