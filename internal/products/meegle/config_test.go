// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
)

func setupTestDir(t *testing.T) string {
	dir := t.TempDir()
	origDir := configDir
	origPath := configPath
	t.Cleanup(func() { configDir = origDir; configPath = origPath })
	configDir = dir
	configPath = filepath.Join(dir, "config.json")
	// Clear direct-injection env vars so tests that exercise profile-only
	// behavior do not pick up values leaked from the developer's shell.
	// Tests that need these set should call t.Setenv after setupTestDir.
	t.Setenv("MEEGLE_HOST", "")
	t.Setenv("MEEGLE_USER_ACCESS_TOKEN", "")
	t.Setenv("MEEGLE_USER_AGENT", "")
	// Route every auth.CreateTokenStore call through an empty FileStore rooted
	// at the temp dir. Without this, ResolveIdentity's fallback path would hit
	// the developer's real macOS Keychain / Linux secret-service and surface
	// a stale token — the bug tracked by the previously flaky
	// TestResolveIdentity_UnsetWhenNothingConfigured.
	restore := auth.SetTokenStoreFactory(func(profile string) auth.TokenStore {
		return auth.NewFileStore(dir, profile)
	})
	t.Cleanup(restore)
	return dir
}

func TestLoadProfileConfigEmpty(t *testing.T) {
	setupTestDir(t)
	cfg, err := LoadProfileConfig("default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "" {
		t.Errorf("expected empty host, got %s", cfg.Host)
	}
}

func TestSaveAndLoadProfileConfig(t *testing.T) {
	setupTestDir(t)
	if err := SaveProfileConfig("test", MeegleConfig{Host: "meegle.com"}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	cfg, err := LoadProfileConfig("test")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.Host != "meegle.com" {
		t.Errorf("expected meegle.com, got %s", cfg.Host)
	}
}

func TestCurrentProfile(t *testing.T) {
	setupTestDir(t)
	name, _ := GetCurrentProfileName()
	if name != "default" {
		t.Errorf("expected default, got %s", name)
	}
	SetCurrentProfileName("staging")
	name, _ = GetCurrentProfileName()
	if name != "staging" {
		t.Errorf("expected staging, got %s", name)
	}
}

func TestListProfiles(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("alpha", MeegleConfig{Host: "a.com"})
	SaveProfileConfig("beta", MeegleConfig{Host: "b.com"})
	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles.Names) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles.Names))
	}
}

func TestDeleteProfile(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("todelete", MeegleConfig{Host: "x.com"})
	SaveProfileConfig("keep", MeegleConfig{Host: "y.com"})
	SetCurrentProfileName("keep")
	DeleteProfile("todelete")
	profiles, _ := ListProfiles()
	for _, n := range profiles.Names {
		if n == "todelete" {
			t.Error("profile should have been deleted")
		}
	}
}

func TestGetServerUrl(t *testing.T) {
	url := GetServerURL(MeegleConfig{Host: "meegle.com"})
	if url != "https://meegle.com/mcp_server/v1" {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestGetServerUrlDefault(t *testing.T) {
	url := GetServerURL(MeegleConfig{})
	if url != "https://meegle.com/mcp_server/v1" {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestIsConfigured(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("default", MeegleConfig{Host: "meegle.com"})
	ok, _ := IsConfigured("")
	if !ok {
		t.Error("expected configured")
	}
}

func TestSetProfileValueArbitraryKey(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("test", MeegleConfig{Host: "meegle.com"})
	if err := SetProfileValue("test", "custom_key", "custom_value"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	val, err := GetProfileValue("test", "custom_key")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if val != "custom_value" {
		t.Errorf("expected custom_value, got %v", val)
	}
	// Verify host is preserved
	cfg, _ := LoadProfileConfig("test")
	if cfg.Host != "meegle.com" {
		t.Errorf("expected host preserved, got %s", cfg.Host)
	}
}

func TestSetProfileValueJSONParse(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("test", MeegleConfig{Host: "meegle.com"})
	if err := SetProfileValue("test", "count", "42"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	val, err := GetProfileValue("test", "count")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	// JSON.parse("42") returns float64 in Go
	if val != float64(42) {
		t.Errorf("expected 42 (float64), got %v (%T)", val, val)
	}
}

func TestSetProfileValueBoolJSON(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("test", MeegleConfig{Host: "meegle.com"})
	if err := SetProfileValue("test", "debug", "true"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	val, _ := GetProfileValue("test", "debug")
	if val != true {
		t.Errorf("expected true, got %v", val)
	}
}

func TestDeleteProfileDeterministicFallback(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("charlie", MeegleConfig{Host: "c.com"})
	SaveProfileConfig("alpha", MeegleConfig{Host: "a.com"})
	SaveProfileConfig("bravo", MeegleConfig{Host: "b.com"})
	SetCurrentProfileName("charlie")

	// Delete current profile should pick first alphabetically
	DeleteProfile("charlie")
	name, _ := GetCurrentProfileName()
	if name != "alpha" {
		t.Errorf("expected deterministic fallback to alpha, got %s", name)
	}
}

func TestExpandEnvTemplate_Literal(t *testing.T) {
	got, err := ExpandEnvTemplate("plain-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain-value" {
		t.Errorf("expected plain-value, got %s", got)
	}
}

func TestExpandEnvTemplate_Resolved(t *testing.T) {
	t.Setenv("MEEGLE_TEST_TOKEN", "pat_abc")
	got, err := ExpandEnvTemplate("${MEEGLE_TEST_TOKEN}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "pat_abc" {
		t.Errorf("expected pat_abc, got %s", got)
	}
}

func TestExpandEnvTemplate_MissingFailsFast(t *testing.T) {
	// Ensure the var is unset.
	_ = os.Unsetenv("MEEGLE_TEST_TOKEN_MISSING")
	_, err := ExpandEnvTemplate("${MEEGLE_TEST_TOKEN_MISSING}")
	if err == nil {
		t.Fatal("expected error for unset env var, got nil")
	}
	if !strings.Contains(err.Error(), "MEEGLE_TEST_TOKEN_MISSING") {
		t.Errorf("expected error to mention var name, got: %v", err)
	}
}

func TestExpandEnvTemplate_EmptyFailsFast(t *testing.T) {
	t.Setenv("MEEGLE_TEST_TOKEN_EMPTY", "")
	_, err := ExpandEnvTemplate("${MEEGLE_TEST_TOKEN_EMPTY}")
	if err == nil {
		t.Fatal("expected error for empty env var, got nil")
	}
}

func TestExpandEnvTemplate_MixedTreatedAsLiteral(t *testing.T) {
	t.Setenv("MEEGLE_TEST_X", "abc")
	// Strict mode: mixed forms are not expanded, returned verbatim.
	got, err := ExpandEnvTemplate("Bearer ${MEEGLE_TEST_X}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Bearer ${MEEGLE_TEST_X}" {
		t.Errorf("expected literal passthrough, got %s", got)
	}
}

func TestExpandEnv_AllStringFields(t *testing.T) {
	t.Setenv("MEEGLE_TEST_HOST", "h.example.com")
	t.Setenv("MEEGLE_TEST_TOKEN", "tok_xyz")
	t.Setenv("MEEGLE_TEST_HDR", "hv")
	cfg := MeegleConfig{
		Host:        "${MEEGLE_TEST_HOST}",
		AccessToken: "${MEEGLE_TEST_TOKEN}",
		Headers:     map[string]string{"X-Trace": "${MEEGLE_TEST_HDR}", "X-Static": "literal"},
	}
	out, err := cfg.ExpandEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Host != "h.example.com" {
		t.Errorf("host expand failed: %s", out.Host)
	}
	if out.AccessToken != "tok_xyz" {
		t.Errorf("user_access_token expand failed: %s", out.AccessToken)
	}
	if out.Headers["X-Trace"] != "hv" {
		t.Errorf("header expand failed: %v", out.Headers)
	}
	if out.Headers["X-Static"] != "literal" {
		t.Errorf("static header mutated: %v", out.Headers)
	}
}

func TestExpandEnv_ReportsFieldPath(t *testing.T) {
	_ = os.Unsetenv("MEEGLE_TEST_UNSET_1")
	cfg := MeegleConfig{AccessToken: "${MEEGLE_TEST_UNSET_1}"}
	_, err := cfg.ExpandEnv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "user_access_token") {
		t.Errorf("expected error to mention user_access_token, got: %v", err)
	}
}

// access_token_header must round-trip through the JSON store so that
// `meegle config set access_token_header <name>` is durable across CLI
// invocations — otherwise the user silently falls back to Authorization.
func TestSaveLoadProfile_AccessTokenHeader_RoundTrip(t *testing.T) {
	setupTestDir(t)
	in := MeegleConfig{
		Host:              "meegle.example.com",
		AccessToken:       "tok",
		AccessTokenHeader: "x-meegle-auth",
	}
	if err := SaveProfileConfig("p", in); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	got, err := LoadProfileConfig("p")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got.AccessTokenHeader != "x-meegle-auth" {
		t.Errorf("expected access_token_header to persist, got %q", got.AccessTokenHeader)
	}
}

// The ${VAR} placeholder rule must also apply to access_token_header, so
// operators can parameterize it per-environment without committing the
// concrete header name.
func TestExpandEnv_AccessTokenHeader_Placeholder(t *testing.T) {
	t.Setenv("MEEGLE_TEST_AUTH_HDR", "x-custom-auth")
	cfg := MeegleConfig{AccessTokenHeader: "${MEEGLE_TEST_AUTH_HDR}"}
	out, err := cfg.ExpandEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AccessTokenHeader != "x-custom-auth" {
		t.Errorf("expected expansion, got %q", out.AccessTokenHeader)
	}
}

func TestSaveProfilePreservesTemplate(t *testing.T) {
	setupTestDir(t)
	in := MeegleConfig{Host: "meegle.com", AccessToken: "${CI_TOKEN}"}
	if err := SaveProfileConfig("t", in); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	// LoadProfileConfig returns raw placeholder for audit/display.
	raw, _ := LoadProfileConfig("t")
	if raw.AccessToken != "${CI_TOKEN}" {
		t.Errorf("expected raw template preserved, got %s", raw.AccessToken)
	}
}

func TestListProfilesSorted(t *testing.T) {
	setupTestDir(t)
	SaveProfileConfig("zebra", MeegleConfig{Host: "z.com"})
	SaveProfileConfig("alpha", MeegleConfig{Host: "a.com"})
	SaveProfileConfig("mid", MeegleConfig{Host: "m.com"})
	profiles, _ := ListProfiles()
	if len(profiles.Names) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles.Names))
	}
	if profiles.Names[0] != "alpha" || profiles.Names[1] != "mid" || profiles.Names[2] != "zebra" {
		t.Errorf("expected sorted order [alpha mid zebra], got %v", profiles.Names)
	}
}

// user_agent must round-trip through the JSON store so operators can persist
// their caller identity once and reuse across invocations.
func TestSaveLoadProfile_UserAgent_RoundTrip(t *testing.T) {
	setupTestDir(t)
	in := MeegleConfig{
		Host:      "meegle.example.com",
		UserAgent: "my-service/1.0",
	}
	if err := SaveProfileConfig("p", in); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	got, err := LoadProfileConfig("p")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got.UserAgent != "my-service/1.0" {
		t.Errorf("expected user_agent to persist, got %q", got.UserAgent)
	}
}

// ExpandEnv must resolve ${VAR} in user_agent the same way it does for the
// other string fields, so operators can parameterize per-environment.
func TestExpandEnv_UserAgent_Placeholder(t *testing.T) {
	t.Setenv("MEEGLE_TEST_UA_CALLER", "my-svc/1.0")
	cfg := MeegleConfig{UserAgent: "${MEEGLE_TEST_UA_CALLER}"}
	out, err := cfg.ExpandEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UserAgent != "my-svc/1.0" {
		t.Errorf("expected expansion, got %q", out.UserAgent)
	}
}

// An unresolved ${VAR} in user_agent must fail fast with a field-path error,
// matching the behavior for host/user_access_token/access_token_header.
func TestExpandEnv_UserAgent_MissingFailsFast(t *testing.T) {
	_ = os.Unsetenv("MEEGLE_TEST_UA_MISSING")
	cfg := MeegleConfig{UserAgent: "${MEEGLE_TEST_UA_MISSING}"}
	_, err := cfg.ExpandEnv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "user_agent") {
		t.Errorf("expected error to mention user_agent field, got: %v", err)
	}
}
