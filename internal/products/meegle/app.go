// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	platformapi "github.com/larksuite/meegle-cli/extension/platform"
	platformruntime "github.com/larksuite/meegle-cli/internal/extension/platform"
	exttransport "github.com/larksuite/meegle-cli/internal/extension/transport"
	"github.com/larksuite/meegle-cli/internal/products/meegle/auth"
	"github.com/larksuite/meegle-cli/internal/products/meegle/discovery"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
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

var credentialDeferredRootCommands = map[string]struct{}{
	"auth": {}, "completion": {}, "config": {}, "extension": {},
	"help": {}, "inspect": {}, "url": {}, "version": {},
}

// shouldResolveCredentialAtBootstrap reports whether command discovery needs
// an identity before Cobra can route the invocation. Known local roots defer
// identity resolution to their own handlers; dynamic or unknown command roots
// keep the fail-closed startup path so MCP commands can be discovered.
func shouldResolveCredentialAtBootstrap(args []string) bool {
	if len(args) == 0 {
		return false
	}
	longFlags := make(map[string]registry.FlagDef, len(registry.BuiltinFlags)+len(MeegleGlobalFlags))
	shortFlags := make(map[string]registry.FlagDef)
	for _, flag := range append(append([]registry.FlagDef(nil), registry.BuiltinFlags...), MeegleGlobalFlags...) {
		longFlags[flag.Name] = flag
		if flag.Short != "" {
			shortFlags[flag.Short] = flag
		}
	}

	rootCommand := ""
	expectsValue := false
	for _, arg := range args {
		if expectsValue {
			expectsValue = false
			continue
		}
		if arg == "--" {
			// Everything after the terminator is positional input, not a command
			// or a global meta flag. If no command was found, Cobra will reject it
			// without needing credentials.
			return rootCommand != ""
		}
		if enabledBooleanFlag(arg, "help", "h") {
			// Root help is local. Help for a dynamic root still needs discovery;
			// local roots have already returned above when they were encountered.
			return rootCommand != ""
		}
		if enabledBooleanFlag(arg, "version", "") {
			return false
		}
		if name, hasValue, ok := splitLongFlag(arg); ok {
			flag, known := longFlags[name]
			if !known {
				return rootCommand != ""
			}
			if !hasValue && flag.Type != registry.FlagTypeBool {
				expectsValue = true
			}
			continue
		}
		if name, hasValue, ok := splitShortFlag(arg); ok {
			flag, known := shortFlags[name]
			if !known {
				return rootCommand != ""
			}
			if !hasValue && flag.Type != registry.FlagTypeBool {
				expectsValue = true
			}
			continue
		}
		if rootCommand == "" {
			rootCommand = arg
			if _, deferred := credentialDeferredRootCommands[rootCommand]; deferred {
				return false
			}
		}
	}
	return rootCommand != ""
}

func enabledBooleanFlag(arg, longName, shortName string) bool {
	if arg == "--"+longName || (shortName != "" && arg == "-"+shortName) {
		return true
	}
	value, ok := strings.CutPrefix(arg, "--"+longName+"=")
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

func splitLongFlag(arg string) (name string, hasValue bool, ok bool) {
	value, ok := strings.CutPrefix(arg, "--")
	if !ok || value == "" {
		return "", false, false
	}
	name, _, hasValue = strings.Cut(value, "=")
	return name, hasValue, true
}

func splitShortFlag(arg string) (name string, hasValue bool, ok bool) {
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return "", false, false
	}
	name = string(arg[1])
	return name, len(arg) > 2, true
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

// profileNameFromArgs applies the persistent --profile flag before the Cobra
// tree and its profile-specific discovery cache are built.
func profileNameFromArgs(args []string, fallback string) string {
	profile := fallback
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if value, ok := strings.CutPrefix(arg, "--profile="); ok {
			if value != "" {
				profile = value
			}
			continue
		}
		if arg == "--profile" && index+1 < len(args) && args[index+1] != "" && !strings.HasPrefix(args[index+1], "--") {
			profile = args[index+1]
			index++
		}
	}
	return profile
}

// finalizePlugins preserves a business-command failure. A fail-closed
// Shutdown error is returned only when the command itself succeeded.
func finalizePlugins(runErr, shutdownErr error) error {
	if cliapp.IsSuccessfulExit(runErr) {
		if shutdownErr != nil {
			return shutdownErr
		}
		return runErr
	}
	if runErr != nil {
		if shutdownErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "[meegle] shutdown hook failed after command error: %v\n", shutdownErr)
		}
		return runErr
	}
	return shutdownErr
}

func lifecycleCommandError(runErr error) error {
	if cliapp.IsSuccessfulExit(runErr) {
		return nil
	}
	return runErr
}

// StaticCommands allows external callers (cmd/meegle) to inject static subcommands, avoiding circular imports.
type StaticCommands struct {
	Auth       *cobra.Command
	Config     *cobra.Command
	Inspect    *cobra.Command
	Completion *cobra.Command
	URL        *cobra.Command
}

// NewCLIApp assembles the meegle CLI application, injecting the four customization points.
// staticCmds are optional static subcommands (auth/config/inspect) used to break circular dependencies.
func NewCLIApp(version string, staticCmds *StaticCommands) (*cliapp.App, error) {
	// 1. Load default configuration (graceful: use empty config on failure)
	profileName, _ := GetCurrentProfileName()
	profileName = profileNameFromArgs(os.Args[1:], profileName)
	cfg, _ := LoadConfig(profileName)

	// 2. Build MCP client (if host + token are configured)
	ctx := context.Background()
	httpClient, transportDiagnostics := exttransport.NewHTTPClientWithDiagnostics(ctx, http.DefaultClient)
	plugins, err := platformruntime.Install(version)
	if err != nil {
		return nil, extensionStartupError("CLIENT_EXTENSION_INSTALL_FAILED", "failed to install CLI platform extensions", err)
	}
	var client *mcpclient.Client
	var ident ResolvedIdentity
	var identitySnapshot *ResolvedIdentity
	var bootstrapErr error
	if shouldResolveCredentialAtBootstrap(os.Args[1:]) {
		client, ident, err = buildCLIClient(ctx, cfg, profileName, httpClient, version)
		if err != nil && !isCLIConfigResolutionError(err) {
			return nil, extensionStartupError("CLIENT_CREDENTIAL_RESOLUTION_FAILED", "failed to resolve CLI credentials", err)
		}
		if err == nil {
			identitySnapshot = &ident
		} else {
			bootstrapErr = err
		}
	}

	// 3. Build ToolCache
	effectiveProfile := profileName
	if ident.ProfileName != "" {
		effectiveProfile = ident.ProfileName
	}
	cache := NewToolCache(GetCacheDir(), effectiveProfile, DefaultTTL)

	// 4. Create DynamicRegistrySetup (client may be nil; avoid the typed nil interface pitfall)
	var lister discovery.ToolLister
	if client != nil {
		lister = client
	}
	// Pre-scan argv: cobra hasn't parsed yet but resolveTools needs to know
	// whether to bypass the cache before the command tree is built.
	forceRefresh := hasRefreshFlag(os.Args[1:])
	dynamicSetup := NewDynamicRegistrySetup(
		lister, cache,
		WithGlobalFlags(MeegleGlobalFlags),
		WithTokenManager(ident.TokenManager),
		WithActiveToken(ident.Token),
		WithIdentitySource(ident.Source),
		WithForceRefresh(forceRefresh),
		WithDiscoveryFailureDegradation(true),
		WithBootstrapError(bootstrapErr),
	)
	registrySetup := registry.NewCompositeSetup(dynamicSetup, NewMeegleLocalSetup())

	// 5. Placeholder executor (the product pipeline owns runtime execution; this is just a fallback)
	placeholderExec := executor.Func(func(_ context.Context, _ *executor.Request) (*executor.RawResult, error) {
		return nil, fmt.Errorf("executor not implemented: commands should be executed via pipeline steps")
	})

	// 6. Assemble cliapp
	customize := rootCustomizer(staticCmds, client, dynamicSetup, plugins.Diagnostics(), ident, transportDiagnostics)
	return cliapp.New(
		cliapp.WithAppName("meegle"),
		cliapp.WithVersion(version),
		cliapp.WithSetup(registrySetup),
		cliapp.WithExecutor(placeholderExec),
		cliapp.WithPipelineFactory(newPipelineFactory(dynamicSetup, identitySnapshot, httpClient)),
		cliapp.WithContextDecorator(func(ctx context.Context) context.Context {
			ctx = auth.WithHTTPClient(ctx, httpClient)
			ctx = withCLIProfile(ctx, effectiveProfile)
			if identitySnapshot != nil {
				ctx = WithCLIIdentity(ctx, ident)
			}
			return ctx
		}),
		cliapp.WithRootCommandCustomizer(func(root *cobra.Command, meta cliapp.RootCommandMetadata) {
			customize(root, meta)
			plugins.Apply(root)
		}),
		cliapp.WithExecutionHooks(
			func(ctx context.Context) error {
				return plugins.Emit(ctx, platformapi.Startup, nil)
			},
			func(ctx context.Context, runErr error) error {
				return finalizePlugins(runErr, plugins.Emit(ctx, platformapi.Shutdown, lifecycleCommandError(runErr)))
			},
		),
	)
}

func extensionStartupError(code, message string, cause error) error {
	if cause == nil {
		return nil
	}
	return meerrors.NewClientError(code, message).WithCause(cause)
}

// ResolveMappedCommands resolves mapped commands under the current configuration (used by the inspect command).
func ResolveMappedCommands(ctx context.Context) []types.MappedCommand {
	httpClient := auth.HTTPClient(ctx)
	ident, hasSnapshot := CLIIdentityFromContext(ctx)
	profileName, hasProfile := cliProfileFromContext(ctx)
	if ident.ProfileName != "" {
		profileName = ident.ProfileName
		hasProfile = true
	}
	var client *mcpclient.Client
	if hasSnapshot {
		client = NewMCPClientFromIdentity(ident, mcpclient.WithHTTPClient(httpClient))
	} else {
		if !hasProfile {
			profileName, _ = GetCurrentProfileName()
		}
		cfg, _ := LoadConfig(profileName)
		var err error
		client, ident, err = buildCLIClient(ctx, cfg, profileName, httpClient, "")
		if err != nil {
			return nil
		}
	}
	if profileName == "" {
		profileName, _ = GetCurrentProfileName()
	}

	cache := NewToolCache(GetCacheDir(), profileName, DefaultTTL)
	var lister discovery.ToolLister
	if client != nil {
		lister = client
	}
	setup := NewDynamicRegistrySetup(lister, cache,
		WithTokenManager(ident.TokenManager),
		WithActiveToken(ident.Token),
		WithIdentitySource(ident.Source),
	)
	tools, _ := setup.resolveTools(ctx)
	commands, _ := mapAndSanitizeTools(tools, MeegleGlobalFlags)
	return append(commands, localMappedCommands()...)
}

// rootCustomizer sets the root command description and adds static subcommands.
// setup is consulted after Init runs so we can detect a stale-token (401) state
// and surface the same login hint as the no-client path.
func rootCustomizer(staticCmds *StaticCommands, client *mcpclient.Client, setup *DynamicRegistrySetup, diagnostics platformruntime.Diagnostics, identity ResolvedIdentity, transportDiagnostics ...exttransport.Diagnostics) cliapp.RootCommandCustomizer {
	return func(root *cobra.Command, meta cliapp.RootCommandMetadata) {
		if root == nil {
			return
		}
		root.Short = "Agent-First CLI for Meegle (Lark Project)"
		root.Version = meta.Version
		root.SetVersionTemplate("{{.Version}}\n")
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
		var mappingIssues []ToolMappingIssue
		if setup != nil {
			mappingIssues = setup.MappingIssues()
		}
		root.AddCommand(newExtensionCommand(diagnostics, identity, mappingIssues, transportDiagnostics...))
		annotateStaticCommands(root)
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

func annotateStaticCommands(root *cobra.Command) {
	if root == nil {
		return
	}
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, command := range parent.Commands() {
			if command.RunE != nil {
				if command.Annotations == nil {
					command.Annotations = map[string]string{}
				}
				parts := strings.Fields(command.CommandPath())
				if len(parts) > 1 {
					parts = parts[1:]
				}
				path := strings.Join(parts, "/")
				if command.Annotations["command_id"] == "" {
					command.Annotations["command_id"] = "static:" + path
				}
				if command.Annotations["command_source"] == "" {
					command.Annotations["command_source"] = "static"
				}
				if command.Annotations["risk_level"] == "" {
					command.Annotations["risk_level"] = staticCommandRisk(path)
				}
			}
			walk(command)
		}
	}
	walk(root)
}

func staticCommandRisk(path string) string {
	switch path {
	case "version", "inspect", "auth/status", "config/show", "config/get",
		"config/profile/list", "config/profile/current", "url/decode",
		"extension/doctor", "extension/credentials", "extension/transport",
		"extension/plugins", "extension/policy", "extension/discovery":
		return "read"
	default:
		return "write"
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

func buildCLIClient(ctx context.Context, cfg MeegleConfig, profileName string, httpClient *http.Client, version string) (*mcpclient.Client, ResolvedIdentity, error) {
	ctx = auth.WithHTTPClient(ctx, httpClient)
	ident, err := ResolveCLIIdentity(ctx, cfg, profileName)
	if err != nil {
		return nil, ResolvedIdentity{}, err
	}
	if ident.Host == "" || ident.Token == "" {
		return nil, ident, nil
	}
	ident.MCPUserAgent = mcpclient.BuildUserAgentForVersion(version, ident.UserAgentCaller)
	return NewMCPClientFromIdentity(ident, mcpclient.WithHTTPClient(httpClient)), ident, nil
}

// NewMCPClientFromIdentity builds an MCP client directly from an already
// resolved identity. Callers that have already resolved the identity (e.g.
// per-command paths that read it from ResolveIdentity to inspect Source /
// Token before deciding what to do) reuse this to avoid the redundant
// resolution that buildMcpClient performs internally.
//
// Requires ident.Host and ident.Token to be non-empty; returns nil otherwise.
func NewMCPClientFromIdentity(ident ResolvedIdentity, extraOpts ...mcpclient.Option) *mcpclient.Client {
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
	if ident.MCPUserAgent != "" {
		opts = append(opts, mcpclient.WithUserAgent(ident.MCPUserAgent))
	} else if ident.UserAgentCaller != "" {
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
	opts = append(opts, extraOpts...)
	return mcpclient.New(baseURL, opts...)
}
