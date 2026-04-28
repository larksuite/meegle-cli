// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

// TestDefaultTokenStoreFactory_ReturnsFallbackStore guards the regression
// from issue #3: defaultTokenStoreFactory must not hand back a bare
// KeychainStore / SecretToolStore — sandboxed and headless environments need
// the FallbackStore wrapper so a Save that hits the OS-native store and
// fails can transparently land in the encrypted file store instead.
func TestDefaultTokenStoreFactory_ReturnsFallbackStore(t *testing.T) {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
	default:
		t.Skipf("no native credential store wired up for %s", runtime.GOOS)
	}

	store := defaultTokenStoreFactory("test-fallback-wrapper")
	if _, ok := store.(*FallbackStore); !ok {
		t.Errorf("expected *FallbackStore on %s, got %T", runtime.GOOS, store)
	}
}

// runShExitOne shells out to `sh -c` so the returned err is a real
// *exec.ExitError populated by os/exec, including ExitError.Stderr — the
// thing isSecretToolLookupNotFound keys off of. Tests skip on platforms
// without sh.
func runShExitOne(t *testing.T, script string) ([]byte, error) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not available on %s: %v", runtime.GOOS, err)
	}
	return exec.Command("sh", "-c", script).Output()
}

// TestIsSecretToolLookupNotFound_EmptyStderrIsNotFound mirrors libsecret's
// `value == NULL && error == NULL` path: exit 1, no stdout, no stderr.
// FallbackStore must not flip its sticky-failure bit in this case.
func TestIsSecretToolLookupNotFound_EmptyStderrIsNotFound(t *testing.T) {
	out, err := runShExitOne(t, "exit 1")
	if !isSecretToolLookupNotFound(out, err) {
		t.Errorf("exit 1 with empty stdout/stderr should be classified as not-found, got out=%q err=%v", out, err)
	}
}

// TestIsSecretToolLookupNotFound_NonEmptyStderrIsRealError mirrors
// libsecret's `error != NULL` path (e.g. dbus unreachable): exit 1 with a
// diagnostic line on stderr. Must surface as a real error so FallbackStore
// switches to the file store.
func TestIsSecretToolLookupNotFound_NonEmptyStderrIsRealError(t *testing.T) {
	out, err := runShExitOne(t, "echo 'secret-tool: org.freedesktop.DBus.Error.ServiceUnknown' >&2; exit 1")
	if isSecretToolLookupNotFound(out, err) {
		t.Errorf("exit 1 with stderr should be a real error, got classified as not-found (out=%q)", out)
	}
}

// TestIsSecretToolLookupNotFound_NonExitErrorIsRealError covers the case
// where exec returns something that isn't *exec.ExitError at all (e.g.
// binary missing). Must not be silently treated as not-found.
func TestIsSecretToolLookupNotFound_NonExitErrorIsRealError(t *testing.T) {
	if isSecretToolLookupNotFound(nil, errors.New("exec: not started")) {
		t.Errorf("non-ExitError must not be classified as not-found")
	}
}
