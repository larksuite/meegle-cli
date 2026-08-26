// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	meeglecmd "github.com/larksuite/meegle-cli/cmd"
)

func main() {
	os.Exit(meeglecmd.Execute())
}
