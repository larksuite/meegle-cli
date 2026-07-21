// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"testing"
	"time"
)

func TestFileStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir, "test")
	data := &TokenData{AccessToken: "access-123", RefreshToken: "refresh-456", ExpiresAt: 1234567890000, ClientID: "client-789"}
	if err := store.Save(data); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded nil")
	}
	if loaded.AccessToken != "access-123" {
		t.Errorf("expected access-123, got %s", loaded.AccessToken)
	}
	if loaded.RefreshToken != "refresh-456" {
		t.Errorf("expected refresh-456, got %s", loaded.RefreshToken)
	}
	if loaded.ClientID != "client-789" {
		t.Errorf("expected client-789, got %s", loaded.ClientID)
	}
}

func TestFileStoreClear(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir, "test")
	store.Save(&TokenData{AccessToken: "x"})
	store.Clear()
	loaded, _ := store.Load()
	if loaded != nil {
		t.Error("expected nil after clear")
	}
}

func TestFileStoreLoadMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir, "missing")
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil for missing file")
	}
}

func TestFileStoreRefreshLockSerializesInstances(t *testing.T) {
	dir := t.TempDir()
	first := NewFileStore(dir, "shared")
	second := NewFileStore(dir, "shared")
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.WithRefreshLock(func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.WithRefreshLock(func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second store entered while the first held the refresh lock")
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}
