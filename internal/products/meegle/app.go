// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/meegle-cli/internal/products/meegle/discovery"
	"github.com/larksuite/meegle-cli/internal/products/meegle/dynamic"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// MeegleGlobalFlags defines the global flags for the meegle product.
// Framework builtins already provide format/select/verbose/params/set/dry-run.
var MeegleGlobalFlags = []registry.FlagDef{
	{
		Name:        "profile",
		Type:        registry.FlagTypeString,
		Description: "Specify the configuration profile name",
	},
	{
		Name:        "refresh",
		Type:        registry.FlagTypeBool,
		Description: "Refresh cached commands from server",
	},
}

// hasRefreshFlag scans raw argv for the --refresh boolean flag before cobra
// parses. We need it pre-parse because the command tree is built (and the
// cache lookup happens) inside Setup, which runs before flag parsing.
//
// Recognised forms: "--refresh", "--refresh=true". Anything after the "--"
// end-of-flags marker is ignored so a positional that happens to be the
// literal cannot trigger a refresh.
func hasRefreshFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--refresh" || a == "--refresh=true" {
			return true
		}
	}
	return false
}

// StaticCommands allows external callers (cmd/meegle) to inject static subcommands, avoiding circular imports.
type StaticCommands struct {
	Auth       *cobra.Command
	Config     *cobra.Command
	Inspect    *cobra.Command
	Completion *cobra.Command
	URL        *cobra.Command
}

// offlineCommands are top-level commands that don't need the dynamically
// discovered tool tree, so NewCLIApp skips startup MCP discovery for them.
// `completion` is the key case: shell rc files run `meegle completion zsh` on
// every new shell, where a discovery round-trip would stall startup. (auth
// still makes its own token calls at run time; only tree discovery is skipped.)
// Excluded — they need or show the tree: help, inspect, the runtime
// __complete, and all business commands.
var offlineCommands = map[string]bool{
	"completion": true,
	"auth":       true,
	"config":     true,
	"url":        true,
	"version":    true,
}

// isOfflineCommand reports whether argv invokes an offlineCommands entry. Keyed
// on args[0]: leading global flags (e.g. `--profile p config get`) fall through
// to discovery — at worst one redundant lookup, never a missing tree.
func isOfflineCommand(args []string) bool {
	return len(args) > 0 && offlineCommands[args[0]]
}

// NewCLIApp assembles the meegle CLI application, injecting the four customization points.
// staticCmds are optional static subcommands (auth/config/inspect) used to break circular dependencies.
func NewCLIApp(version string, staticCmds *StaticCommands) (*cliapp.App, error) {
	// Skip identity resolution + MCP discovery for commands that need only the
	// static command set — chiefly `completion`, re-run on every shell startup.
	skipDiscovery := isOfflineCommand(os.Args[1:])

	var (
		client *mcpclient.Client
		ident  ResolvedIdentity
		cache  *ToolCache
	)
	if !skipDiscovery {
		// 1. Load default configuration (graceful: use empty config on failure)
		profileName, _ := GetCurrentProfileName()
		cfg, _ := LoadConfig(profileName)

		// 2. Build MCP client (if host + token are configured)
		client, ident = buildMcpClient(cfg, profileName)

		// 3. Build ToolCache
		cache = NewToolCache(GetCacheDir(), profileName, DefaultTTL)
	}

	// 4. Create DynamicRegistrySetup (client may be nil; avoid the typed nil interface pitfall)
	var lister discovery.ToolLister
	if client != nil {
		lister = client
	}
	// Pre-scan argv: cobra hasn't parsed yet but resolveTools needs to know
	// whether to bypass the cache before the command tree is built.
	forceRefresh := hasRefreshFlag(os.Args[1:])
	registrySetup := NewDynamicRegistrySetup(
		lister, cache,
		WithGlobalFlags(MeegleGlobalFlags),
		WithTokenManager(ident.TokenManager),
		WithIdentitySource(ident.Source),
		WithForceRefresh(forceRefresh),
		WithDiscoveryFailureDegradation(true),
	)

	// 5. Placeholder executor (pipeline already contains McpExecutorStep; this is just a fallback)
	placeholderExec := executor.Func(func(_ context.Context, _ *executor.Request) (*executor.RawResult, error) {
		return nil, fmt.Errorf("executor not implemented: commands should be executed via pipeline steps")
	})

	// 6. Assemble cliapp
	return cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion(version),
		cliapp.WithSetup(registrySetup),
		cliapp.WithExecutor(placeholderExec),
		cliapp.WithPipelineFactory(newPipelineFactory(registrySetup)),
		cliapp.WithRootCommandCustomizer(rootCustomizer(staticCmds, client, registrySetup)),
	)
}

// ResolveMappedCommands resolves mapped commands under the current configuration (used by the inspect command).
func ResolveMappedCommands() []types.MappedCommand {
	profileName, _ := GetCurrentProfileName()
	cfg, _ := LoadConfig(profileName)

	client, ident := buildMcpClient(cfg, profileName)

	cache := NewToolCache(GetCacheDir(), profileName, DefaultTTL)
	var lister discovery.ToolLister
	if client != nil {
		lister = client
	}
	setup := NewDynamicRegistrySetup(lister, cache,
		WithTokenManager(ident.TokenManager),
		WithIdentitySource(ident.Source),
	)
	tools, _ := setup.resolveTools(context.Background())
	return dynamic.MapTools(tools)
}

// rootCustomizer sets the root command description and adds static subcommands.
// setup is consulted after Init runs so we can detect a stale-token (401) state
// and surface the same login hint as the no-client path.
func rootCustomizer(staticCmds *StaticCommands, client *mcpclient.Client, setup *DynamicRegistrySetup) cliapp.RootCommandCustomizer {
	return func(root *cobra.Command, meta cliapp.RootCommandMetadata) {
		if root == nil {
			return
		}
		root.Short = "Agent-First CLI for Meegle (Lark Project)"
		root.Long = `Agent-First CLI for Meegle (Lark Project)

Domain Model:
  workitem ─ Core entity, identified by work-item-id
  ├─ workflow ─ Node transitions & state transitions, depends on work-item-id
  │    └─ subtask ─ Subtasks under a node, depends on node-id
  ├─ comment ─ Comments on work items, depends on work-item-id
  ├─ workhour ─ Time tracking & scheduling, depends on work-item-id
  └─ relation ─ Relationships between work items

  mywork ─ Cross-space personal to-do / done items
  view ─ Fixed views, filtered views
  chart ─ Charts under a view, depends on view-id
  team / user ─ Teams & users
  project ─ Project space information

Typical Workflows:
  Create a work item:         workitem meta-types → workitem meta-create-fields → workitem create
  Transition a node:          workflow get-node → workflow list-state-transitions → workflow transition
  View relations:             relation meta-definitions → relation list`

		if staticCmds != nil {
			if staticCmds.Auth != nil {
				root.AddCommand(staticCmds.Auth)
			}
			if staticCmds.Config != nil {
				root.AddCommand(staticCmds.Config)
			}
			if staticCmds.Inspect != nil {
				root.AddCommand(staticCmds.Inspect)
			}
			if staticCmds.Completion != nil {
				root.AddCommand(staticCmds.Completion)
			}
			if staticCmds.URL != nil {
				root.AddCommand(staticCmds.URL)
			}
		}
		// When not logged in — or the active token was rejected by the server —
		// append a source-specific note so the user knows which knob to rotate.
		// All offline commands (--help, auth, config, completion, inspect) stay
		// usable regardless of the token's fate.
		if client == nil || (setup != nil && setup.AuthFailed()) {
			src := SourceUnset
			if setup != nil {
				src = setup.IdentitySource()
			}
			root.Long += "\n\n" + notLoggedInNote(src)
		}

		RegisterFlagCompletions(root, client)
	}
}

// notLoggedInNote returns the source-specific "Not logged in" note appended to
// the root help output. It serves two states:
//   - No token at all (SourceUnset) — tell the user to log in.
//   - A token was present but rejected (AuthFailed) — lead with the source
//     the CLI can't own (rotate env / config) and also surface `auth login`
//     as a fallback path, since the user may not have a fresh token on hand.
//
// For env/config sources the auth login line includes the unset/clear step:
// ResolveIdentity priority is env > config > store, so a stale env/config
// value would still shadow any new keychain token otherwise.
func notLoggedInNote(src IdentitySource) string {
	switch src {
	case SourceEnv:
		return "Note: Token from MEEGLE_USER_ACCESS_TOKEN was rejected. Only basic commands are shown. Choose one:\n" +
			"  1. Rotate env var:         export MEEGLE_USER_ACCESS_TOKEN=<fresh-token>\n" +
			"  2. Switch to managed flow: unset MEEGLE_USER_ACCESS_TOKEN && meegle auth login"
	case SourceConfig:
		return "Note: Token from config.json was rejected. Only basic commands are shown. Choose one:\n" +
			"  1. Rotate config value:    meegle config set user_access_token <fresh-token>\n" +
			"  2. Switch to managed flow: meegle config set user_access_token \"\" && meegle auth login"
	default:
		return "Note: Not logged in. Only basic commands are shown. Run the following to access all commands:\n" +
			"  meegle auth login"
	}
}

// BuildRegistrySetup builds a DynamicRegistrySetup from host + headers.
// Used by both NewCLIApp and the SDK.
// extraMcpOpts are appended after the base options (token, headers) — callers
// can pass mcpclient.WithUserAgent, mcpclient.WithHTTPClient, etc. directly.
func BuildRegistrySetup(host string, headers map[string]string, extraMcpOpts ...mcpclient.Option) *DynamicRegistrySetup {
	var client *mcpclient.Client
	if host != "" {
		baseURL := GetServerURL(MeegleConfig{Host: host})
		var mcpOpts []mcpclient.Option

		httpHeaders := make(http.Header)
		for k, v := range headers {
			httpHeaders.Set(k, v)
		}

		// Extract token from headers
		if authHeader := httpHeaders.Get("Authorization"); authHeader != "" {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			mcpOpts = append(mcpOpts, mcpclient.WithToken(func() (string, error) {
				return token, nil
			}))
		}
		if len(httpHeaders) > 0 {
			mcpOpts = append(mcpOpts, mcpclient.WithHeaders(httpHeaders))
		}
		mcpOpts = append(mcpOpts, extraMcpOpts...)

		client = mcpclient.New(baseURL, mcpOpts...)
	}

	// No file cache in SDK mode: the command tree is built once in NewClient,
	// and subsequent Execute calls reuse the in-memory cobra routing.
	return NewDynamicRegistrySetup(client, nil,
		WithGlobalFlags(MeegleGlobalFlags),
	)
}

// buildMcpClient builds an MCP client from configuration (used for dynamic
// discovery). Returns (nil, zero-value) when no usable identity is available.
// The second return carries the resolved identity so callers can propagate
// Source / TokenManager into the registry setup for source-aware 401 handling.
//
// This function is intentionally thin: ResolveIdentity owns the priority
// semantics (env > config > store). The only startup-specific policy here is
// degrading silently instead of raising — business commands re-run resolution
// via SessionStep and produce user-facing errors there.
func buildMcpClient(cfg MeegleConfig, profileName string) (*mcpclient.Client, ResolvedIdentity) {
	ident, err := ResolveIdentity(cfg, profileName)
	if err != nil {
		return nil, ResolvedIdentity{}
	}
	if ident.Host == "" || ident.Token == "" {
		// Still return ident so the caller can access Host (for zero-config
		// diagnostics) even though no client could be built.
		return nil, ident
	}
	return NewMCPClientFromIdentity(ident), ident
}

// NewMCPClientFromIdentity builds an MCP client directly from an already
// resolved identity. Callers that have already resolved the identity (e.g.
// per-command paths that read it from ResolveIdentity to inspect Source /
// Token before deciding what to do) reuse this to avoid the redundant
// resolution that buildMcpClient performs internally.
//
// Requires ident.Host and ident.Token to be non-empty; returns nil otherwise.
func NewMCPClientFromIdentity(ident ResolvedIdentity) *mcpclient.Client {
	if ident.Host == "" || ident.Token == "" {
		return nil
	}

	baseURL := GetServerURL(MeegleConfig{Host: ident.Host})
	httpHeaders := make(http.Header)
	for k, v := range ident.Headers {
		httpHeaders.Set(k, v)
	}

	opts := []mcpclient.Option{mcpclient.WithHeaders(httpHeaders)}
	if ident.AccessTokenHeader != "" {
		opts = append(opts, mcpclient.WithAuthHeader(ident.AccessTokenHeader))
	}
	if ident.UserAgentCaller != "" {
		opts = append(opts, mcpclient.WithUserAgent(mcpclient.BuildUserAgent(ident.UserAgentCaller)))
	}
	if ident.Source == SourceStore {
		// tokenFunc reads live from the TokenManager so a successful refresh
		// (triggered by refreshFunc) is picked up on retry.
		opts = append(opts,
			mcpclient.WithToken(ident.TokenManager.GetToken),
			mcpclient.WithRefreshFunc(ident.TokenManager.TryRefresh),
		)
	} else {
		// Env / config mode: token is not refreshable locally. A server 401
		// surfaces to the caller so they can rotate the source value.
		token := ident.Token
		opts = append(opts, mcpclient.WithToken(func() (string, error) { return token, nil }))
	}
	return mcpclient.New(baseURL, opts...)
}
