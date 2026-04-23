// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGenerateCodeVerifier(t *testing.T) {
	v := GenerateCodeVerifier()
	if len(v) < 43 {
		t.Errorf("verifier too short: %d", len(v))
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test-verifier-string"
	challenge := GenerateCodeChallenge(verifier)
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != expected {
		t.Errorf("expected %s, got %s", expected, challenge)
	}
}

func TestCodeVerifierUniqueness(t *testing.T) {
	a := GenerateCodeVerifier()
	b := GenerateCodeVerifier()
	if a == b {
		t.Error("two verifiers should be different")
	}
}
