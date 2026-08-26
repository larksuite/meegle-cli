// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"strings"
	"testing"
)

func TestCheckCLIVersion(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		{constraint: ">=1.2.0 <2.0.0", version: "1.8.3+build.7", want: true},
		{constraint: ">= 1.2.0 < 2.0.0", version: "1.8.3", want: true},
		{constraint: ">=1.2.0 <2.0.0", version: "2.0.0", want: false},
		{constraint: "=1.2.3", version: "v1.2.3", want: true},
		{constraint: ">=1.2.3", version: "1.2.3-beta.1", want: false},
		{constraint: "", version: "dev", want: true},
	}
	for _, test := range tests {
		if got := checkCLIVersion(test.constraint, test.version) == nil; got != test.want {
			t.Errorf("checkCLIVersion(%q, %q) success = %t, want %t", test.constraint, test.version, got, test.want)
		}
	}
}

func TestCheckCLIVersion_DevBuildExplainsEnterpriseVersionEntry(t *testing.T) {
	err := checkCLIVersion(">=1.2.0", "dev")
	if err == nil {
		t.Fatal("dev build unexpectedly satisfied a version constraint")
	}
	for _, fragment := range []string{"ExecuteWithVersion", "semantic version", `"dev"`} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("dev compatibility error is missing %q: %v", fragment, err)
		}
	}
}
