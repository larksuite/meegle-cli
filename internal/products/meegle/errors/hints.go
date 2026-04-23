// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

// HintNonInteractiveSetup is shown when host configuration or login is
// required but the current process cannot drive an interactive prompt
// (CI, agent shells like Claude Code, pipes, etc.). It lists the two
// non-interactive paths supported by the CLI without binding to any
// specific tenant.
const HintNonInteractiveSetup = `Non-interactive environment detected (CI / agent shell / pipe). Choose one of:
  1. Pass it inline:    meegle auth login --device-code --host <host>
  2. Persist it:        meegle config set host <host> && meegle auth login --device-code
Examples of <host>: project.feishu.cn, meegle.com, your-tenant.example.com`

// HintDeviceCodeRequired is shown when the default Authorization Code flow
// cannot run because the environment is non-interactive — the local browser
// callback would never be reached.
const HintDeviceCodeRequired = `Re-run with --device-code, e.g.: meegle auth login --device-code --host <host>`

// HintConfigTokenRejected is shown when the MCP server rejects a token that
// came from config.json or an environment variable (MEEGLE_USER_ACCESS_TOKEN).
// This path has no local refresh, so the caller must rotate the value.
const HintConfigTokenRejected = `Token rejected by server. Since this token came from config.json or an env var, rotate the value:
  1. Env var path:    export MEEGLE_USER_ACCESS_TOKEN=<fresh-token>
  2. Config path:     meegle config set user_access_token <fresh-token>
  3. Placeholder:     re-export the env var referenced by the ${VAR} in config.json
Or switch to the managed keychain flow: meegle auth login`
