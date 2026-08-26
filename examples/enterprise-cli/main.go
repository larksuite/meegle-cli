// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	meeglecmd "github.com/larksuite/meegle-cli/cmd"

	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/credential"
	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/governance"
	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/transport"
)

var version = "0.1.0"

func main() {
	os.Exit(meeglecmd.ExecuteWithVersion(version))
}
