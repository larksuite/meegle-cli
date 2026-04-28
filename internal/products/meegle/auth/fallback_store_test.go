// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"errors"
	"testing"
)

type fakeStore struct {
	loadData *TokenData
	loadErr  error
	saveErr  error
	clearErr error

	loadCalls  int
	saveCalls  int
	clearCalls int

	saved *TokenData
}

func (f *fakeStore) Load() (*TokenData, error) {
	f.loadCalls++
	return f.loadData, f.loadErr
}

func (f *fakeStore) Save(d *TokenData) error {
	f.saveCalls++
	f.saved = d
	return f.saveErr
}

func (f *fakeStore) Clear() error {
	f.clearCalls++
	return f.clearErr
}

func TestFallbackStoreSave_PrimarySuccessClearsFallback(t *testing.T) {
	primary := &fakeStore{}
	fallback := &fakeStore{}
	s := NewFallbackStore(primary, fallback)

	if err := s.Save(&TokenData{AccessToken: "x"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if primary.saveCalls != 1 {
		t.Errorf("expected primary.Save called once, got %d", primary.saveCalls)
	}
	if fallback.saveCalls != 0 {
		t.Errorf("expected fallback.Save not called, got %d", fallback.saveCalls)
	}
	if fallback.clearCalls != 1 {
		t.Errorf("expected fallback.Clear called once after primary success, got %d", fallback.clearCalls)
	}
}

func TestFallbackStoreSave_PrimaryFailsFallsBack(t *testing.T) {
	primary := &fakeStore{saveErr: errors.New("keychain locked")}
	fallback := &fakeStore{}
	s := NewFallbackStore(primary, fallback)

	if err := s.Save(&TokenData{AccessToken: "x"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if fallback.saved == nil || fallback.saved.AccessToken != "x" {
		t.Errorf("expected token to land in fallback, got %v", fallback.saved)
	}
}

func TestFallbackStoreSave_BothFailReturnsAggregateError(t *testing.T) {
	primary := &fakeStore{saveErr: errors.New("keychain locked")}
	fallback := &fakeStore{saveErr: errors.New("disk full")}
	s := NewFallbackStore(primary, fallback)

	err := s.Save(&TokenData{AccessToken: "x"})
	if err == nil {
		t.Fatal("expected error when both stores fail")
	}
	if !errors.Is(err, fallback.saveErr) {
		t.Errorf("expected wrapped fallback error, got %v", err)
	}
}

// TestFallbackStoreSave_PrimarySticksAfterFailure guards against re-spawning
// `security` / `secret-tool` (and re-prompting the user for a keychain
// unlock dialog) on every Save once the primary has already failed once in
// this process.
func TestFallbackStoreSave_PrimarySticksAfterFailure(t *testing.T) {
	primary := &fakeStore{saveErr: errors.New("keychain locked")}
	fallback := &fakeStore{}
	s := NewFallbackStore(primary, fallback)

	if err := s.Save(&TokenData{AccessToken: "x"}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.Save(&TokenData{AccessToken: "y"}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if primary.saveCalls != 1 {
		t.Errorf("expected primary.Save called exactly once, got %d", primary.saveCalls)
	}
	if fallback.saveCalls != 2 {
		t.Errorf("expected fallback.Save called twice, got %d", fallback.saveCalls)
	}
}

func TestFallbackStoreLoad_PrimaryHit(t *testing.T) {
	primary := &fakeStore{loadData: &TokenData{AccessToken: "from-primary"}}
	fallback := &fakeStore{loadData: &TokenData{AccessToken: "from-fallback"}}
	s := NewFallbackStore(primary, fallback)

	data, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data == nil || data.AccessToken != "from-primary" {
		t.Errorf("expected primary token, got %+v", data)
	}
	if fallback.loadCalls != 0 {
		t.Errorf("expected fallback.Load not called when primary returns data, got %d", fallback.loadCalls)
	}
}

// TestFallbackStoreLoad_PrimaryEmptyFallbackHit covers the case where the
// primary store is reachable but holds no token (e.g. fresh keychain after a
// prior FileStore-only login session): we still need to surface whatever the
// fallback has, without flipping the sticky bit.
func TestFallbackStoreLoad_PrimaryEmptyFallbackHit(t *testing.T) {
	primary := &fakeStore{}
	fallback := &fakeStore{loadData: &TokenData{AccessToken: "from-fallback"}}
	s := NewFallbackStore(primary, fallback)

	data, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data == nil || data.AccessToken != "from-fallback" {
		t.Errorf("expected fallback token, got %+v", data)
	}
	if s.primaryFailed.Load() {
		t.Error("primary returning (nil, nil) must not flip sticky bit — keychain may be reachable but empty")
	}
}

func TestFallbackStoreLoad_PrimaryErrorFallsBack(t *testing.T) {
	primary := &fakeStore{loadErr: errors.New("dbus unreachable")}
	fallback := &fakeStore{loadData: &TokenData{AccessToken: "from-fallback"}}
	s := NewFallbackStore(primary, fallback)

	data, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data == nil || data.AccessToken != "from-fallback" {
		t.Errorf("expected fallback token after primary error, got %+v", data)
	}
}

// TestFallbackStoreLoad_PrimarySticksAfterError guards Load from re-trying
// the primary once it has already failed in this process — same motivation
// as the Save sticky-bit case.
func TestFallbackStoreLoad_PrimarySticksAfterError(t *testing.T) {
	primary := &fakeStore{loadErr: errors.New("dbus unreachable")}
	fallback := &fakeStore{loadData: &TokenData{AccessToken: "from-fallback"}}
	s := NewFallbackStore(primary, fallback)

	if _, err := s.Load(); err != nil {
		t.Fatalf("first load: %v", err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if primary.loadCalls != 1 {
		t.Errorf("expected primary.Load called exactly once, got %d", primary.loadCalls)
	}
	if fallback.loadCalls != 2 {
		t.Errorf("expected fallback.Load called twice, got %d", fallback.loadCalls)
	}
}

// TestFallbackStoreSaveAfterLoadError guards the cross-operation sticky bit:
// once Load discovers the primary is unreachable, Save must not retry it.
func TestFallbackStoreSaveAfterLoadError(t *testing.T) {
	primary := &fakeStore{loadErr: errors.New("dbus unreachable")}
	fallback := &fakeStore{}
	s := NewFallbackStore(primary, fallback)

	_, _ = s.Load()
	if err := s.Save(&TokenData{AccessToken: "x"}); err != nil {
		t.Fatalf("save after load error: %v", err)
	}
	if primary.saveCalls != 0 {
		t.Errorf("expected primary.Save skipped after Load error, got %d calls", primary.saveCalls)
	}
	if fallback.saveCalls != 1 {
		t.Errorf("expected fallback.Save called once, got %d", fallback.saveCalls)
	}
}

func TestFallbackStoreClear_ClearsBoth(t *testing.T) {
	primary := &fakeStore{}
	fallback := &fakeStore{}
	s := NewFallbackStore(primary, fallback)

	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if primary.clearCalls != 1 || fallback.clearCalls != 1 {
		t.Errorf("expected both stores cleared (primary=%d fallback=%d)", primary.clearCalls, fallback.clearCalls)
	}
}

func TestFallbackStoreClear_AggregatesErrors(t *testing.T) {
	primary := &fakeStore{clearErr: errors.New("keychain locked")}
	fallback := &fakeStore{clearErr: errors.New("disk full")}
	s := NewFallbackStore(primary, fallback)

	err := s.Clear()
	if err == nil {
		t.Fatal("expected error when both Clear fail")
	}
	if !errors.Is(err, primary.clearErr) || !errors.Is(err, fallback.clearErr) {
		t.Errorf("expected aggregated error to wrap both, got %v", err)
	}
}
