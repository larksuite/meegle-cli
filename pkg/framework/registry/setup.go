// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

import "context"

type RegistrySetup interface {
	Setup(ctx context.Context) (*CommandTree, error)
}

type DynamicSetup interface {
	RegistrySetup
	OnChange(ctx context.Context, notify func()) error
}
