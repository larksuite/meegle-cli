// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type MeegleConfig struct {
	Host        string            `json:"host,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	AccessToken string            `json:"user_access_token,omitempty"`
	// AccessTokenHeader overrides the HTTP header that carries the access
	// token. Empty = default "Authorization: Bearer <token>". Non-empty sends
	// the raw token in the named header with no prefix and suppresses
	// Authorization entirely. Use for backends that require a custom auth
	// header instead of standard Bearer.
	AccessTokenHeader string `json:"access_token_header,omitempty"`
	// UserAgent, when non-empty, is appended to the default User-Agent as a
	// caller identifier: "meegle-cli[/version] <UserAgent>". Supports the
	// ${VAR} placeholder via ExpandEnv. Used by CLI; SDK callers pass a
	// caller via WithUserAgent.
	UserAgent string `json:"user_agent,omitempty"`
}

// envTemplateRE matches a string that is entirely a ${VAR} placeholder.
// Only whole-string placeholders are supported; mixed forms like "Bearer ${X}"
// are treated as literal values.
var envTemplateRE = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// ExpandEnvTemplate resolves a single ${VAR} placeholder against os environment.
// Returns the raw string unchanged when it is not a placeholder.
// Returns an error when the placeholder names an unset or empty variable.
func ExpandEnvTemplate(raw string) (string, error) {
	m := envTemplateRE.FindStringSubmatch(raw)
	if m == nil {
		return raw, nil
	}
	name := m[1]
	val, ok := os.LookupEnv(name)
	if !ok || val == "" {
		return "", fmt.Errorf("environment variable %q referenced by config is not set", name)
	}
	return val, nil
}

// ExpandEnv returns a copy of the config with all ${VAR} placeholders in
// string fields (Host, AccessToken, Headers values) resolved against os env.
// Fails fast on the first unresolved placeholder, reporting the field path.
func (c MeegleConfig) ExpandEnv() (MeegleConfig, error) {
	out := c
	if v, err := ExpandEnvTemplate(c.Host); err != nil {
		return MeegleConfig{}, fmt.Errorf("config field host: %w", err)
	} else {
		out.Host = v
	}
	if v, err := ExpandEnvTemplate(c.AccessToken); err != nil {
		return MeegleConfig{}, fmt.Errorf("config field user_access_token: %w", err)
	} else {
		out.AccessToken = v
	}
	if v, err := ExpandEnvTemplate(c.AccessTokenHeader); err != nil {
		return MeegleConfig{}, fmt.Errorf("config field access_token_header: %w", err)
	} else {
		out.AccessTokenHeader = v
	}
	if v, err := ExpandEnvTemplate(c.UserAgent); err != nil {
		return MeegleConfig{}, fmt.Errorf("config field user_agent: %w", err)
	} else {
		out.UserAgent = v
	}
	if len(c.Headers) > 0 {
		out.Headers = make(map[string]string, len(c.Headers))
		for k, raw := range c.Headers {
			v, err := ExpandEnvTemplate(raw)
			if err != nil {
				return MeegleConfig{}, fmt.Errorf("config field headers.%s: %w", k, err)
			}
			out.Headers[k] = v
		}
	}
	return out, nil
}

type ProfileList struct {
	Names   []string
	Current string
}

// rootConfig stores profiles as map[string]any to support arbitrary keys.
type rootConfig struct {
	Current  string                    `json:"current,omitempty"`
	Profiles map[string]map[string]any `json:"profiles,omitempty"`
}

var (
	configDir  = filepath.Join(homeDir(), ".meegle")
	configPath = filepath.Join(configDir, "config.json")
)

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func GetConfigDir() string { return configDir }
func GetCacheDir() string  { return filepath.Join(configDir, "cache") }

func readRootConfig() (rootConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return rootConfig{}, nil
	}
	var root rootConfig
	if err := json.Unmarshal(data, &root); err != nil {
		return rootConfig{}, nil
	}
	return root, nil
}

func writeRootConfig(root rootConfig) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}

func GetCurrentProfileName() (string, error) {
	root, err := readRootConfig()
	if err != nil {
		return "default", err
	}
	if root.Current == "" {
		return "default", nil
	}
	return root.Current, nil
}

func SetCurrentProfileName(name string) error {
	root, err := readRootConfig()
	if err != nil {
		return err
	}
	root.Current = name
	return writeRootConfig(root)
}

// profileToMap converts MeegleConfig to map[string]any for storage.
// String fields are written verbatim; ${VAR} placeholders are preserved in the
// file and only resolved on read via ExpandEnv.
func profileToMap(cfg MeegleConfig) map[string]any {
	m := make(map[string]any)
	if cfg.Host != "" {
		m["host"] = cfg.Host
	}
	if len(cfg.Headers) > 0 {
		m["headers"] = cfg.Headers
	}
	if cfg.AccessToken != "" {
		m["user_access_token"] = cfg.AccessToken
	}
	if cfg.AccessTokenHeader != "" {
		m["access_token_header"] = cfg.AccessTokenHeader
	}
	if cfg.UserAgent != "" {
		m["user_agent"] = cfg.UserAgent
	}
	return m
}

// mapToProfile converts a raw map to MeegleConfig without resolving templates.
func mapToProfile(m map[string]any) MeegleConfig {
	cfg := MeegleConfig{}
	if v, ok := m["host"]; ok {
		if s, ok := v.(string); ok {
			cfg.Host = s
		}
	}
	if v, ok := m["headers"]; ok {
		switch h := v.(type) {
		case map[string]string:
			cfg.Headers = h
		case map[string]any:
			cfg.Headers = make(map[string]string, len(h))
			for k, val := range h {
				cfg.Headers[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	if v, ok := m["user_access_token"]; ok {
		if s, ok := v.(string); ok {
			cfg.AccessToken = s
		}
	}
	if v, ok := m["access_token_header"]; ok {
		if s, ok := v.(string); ok {
			cfg.AccessTokenHeader = s
		}
	}
	if v, ok := m["user_agent"]; ok {
		if s, ok := v.(string); ok {
			cfg.UserAgent = s
		}
	}
	return cfg
}

func LoadProfileConfig(profile string) (MeegleConfig, error) {
	root, err := readRootConfig()
	if err != nil {
		return MeegleConfig{}, err
	}
	if root.Profiles == nil {
		return MeegleConfig{}, nil
	}
	m := root.Profiles[profile]
	if m == nil {
		return MeegleConfig{}, nil
	}
	return mapToProfile(m), nil
}

func SaveProfileConfig(profile string, cfg MeegleConfig) error {
	root, err := readRootConfig()
	if err != nil {
		return err
	}
	if root.Profiles == nil {
		root.Profiles = make(map[string]map[string]any)
	}
	// Merge: keep existing extra keys, overwrite known fields
	existing := root.Profiles[profile]
	if existing == nil {
		existing = make(map[string]any)
	}
	newMap := profileToMap(cfg)
	for k, v := range newMap {
		existing[k] = v
	}
	// Clear known fields if they are zero
	if cfg.Host == "" {
		delete(existing, "host")
	}
	if len(cfg.Headers) == 0 {
		delete(existing, "headers")
	}
	root.Profiles[profile] = existing
	if root.Current == "" {
		root.Current = profile
	}
	return writeRootConfig(root)
}

func GetProfileValue(profile, key string) (any, error) {
	root, err := readRootConfig()
	if err != nil {
		return nil, err
	}
	if root.Profiles == nil {
		return nil, nil
	}
	m := root.Profiles[profile]
	if m == nil {
		return nil, nil
	}
	return m[key], nil
}

func SetProfileValue(profile, key, value string) error {
	root, err := readRootConfig()
	if err != nil {
		return err
	}
	if root.Profiles == nil {
		root.Profiles = make(map[string]map[string]any)
	}
	m := root.Profiles[profile]
	if m == nil {
		m = make(map[string]any)
	}

	// Try JSON.parse on the value first (like TS), fallback to string
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		parsed = value
	}
	m[key] = parsed

	root.Profiles[profile] = m
	if root.Current == "" {
		root.Current = profile
	}
	return writeRootConfig(root)
}

func ListProfiles() (ProfileList, error) {
	root, err := readRootConfig()
	if err != nil {
		return ProfileList{Current: "default"}, err
	}
	current := root.Current
	if current == "" {
		current = "default"
	}
	var names []string
	for name := range root.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return ProfileList{Names: names, Current: current}, nil
}

func DeleteProfile(name string) error {
	root, err := readRootConfig()
	if err != nil {
		return err
	}
	delete(root.Profiles, name)
	if root.Current == name {
		remaining := make([]string, 0, len(root.Profiles))
		for k := range root.Profiles {
			remaining = append(remaining, k)
		}
		sort.Strings(remaining)
		if len(remaining) > 0 {
			root.Current = remaining[0]
		} else {
			root.Current = "default"
		}
	}
	return writeRootConfig(root)
}

func LoadConfig(profile string) (MeegleConfig, error) {
	if profile == "" {
		name, err := GetCurrentProfileName()
		if err != nil {
			return MeegleConfig{}, err
		}
		profile = name
	}
	return LoadProfileConfig(profile)
}

func sanitizeHost(host string) string {
	if strings.Contains(host, "://") {
		if u, err := url.Parse(host); err == nil {
			return u.Host
		}
	}
	return host
}

func GetServerURL(cfg MeegleConfig) string {
	host := cfg.Host
	if host == "" {
		host = "meegle.com"
	}
	host = sanitizeHost(host)
	return fmt.Sprintf("https://%s/mcp_server/v1", host)
}

func IsConfigured(profile string) (bool, error) {
	cfg, err := LoadConfig(profile)
	if err != nil {
		return false, err
	}
	return cfg.Host != "", nil
}
