// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	"github.com/larksuite/meegle-cli/internal/products/meegle/mcpclient"
)

// ---------------------------------------------------------------------------
// Attachment domain registration.
//
// The two basic commands (`attachment prepare-upload`, `attachment prepare-download`)
// come from MCP discovery — `upload_file` and `get_download_url` are mapped
// to the attachment resource via dynamic.fallbackTable, so they appear in
// `meegle inspect` like any other discovered tool. Two scenario shortcuts
// (`+upload`, `+download`) compose those basic operations with
// the out-of-band HTTP I/O against object storage and are injected here.
// ---------------------------------------------------------------------------

// TagAttachmentShortcut marks a node as a +upload / +download
// shortcut that AttachmentShortcutStep should orchestrate. Value carries the
// kind so the step branches without re-parsing the node name.
//
// Shortcut nodes inherit their HandlerRef from the basic prepare-upload /
// prepare-download sibling (mirroring how +batch-get inherits from `get`).
// McpExecutorStep keys on this tag — not on HandlerRef — to short-circuit so
// the shared HandlerRef does not cause the basic MCP path to re-fire.
const (
	TagAttachmentShortcut = "mcp_attachment_shortcut"

	attachmentShortcutUpload   = "upload"
	attachmentShortcutDownload = "download"
)

// attachmentShortcut defines a +-prefixed scenario command that orchestrates
// a basic prepare-* sibling plus the out-of-band HTTP I/O. Mirrors batch.go's
// batchCommand contract: the shortcut's MCP wiring (HandlerRef +
// mcp_param_types) is INHERITED from SiblingName at injection time, so a
// rename of the underlying MCP tool only requires touching the basic command.
type attachmentShortcut struct {
	Name        string
	Aliases     []string
	Brief       string
	Long        string
	Kind        string // attachmentShortcutUpload | attachmentShortcutDownload
	SiblingName string // basic prepare-* command to inherit HandlerRef from
	Args        []registry.ArgDef
	Flags       []registry.FlagDef
}

var attachmentShortcuts = []attachmentShortcut{
	{
		Name:        "+upload",
		Brief:       "Upload a local file end-to-end (preprocess + signed HTTP POST)",
		Long:        "Composes `attachment prepare-upload` with the signed HTTP POST(s) to object storage and returns the resulting file_token alongside the file metadata.\n\nresource-type values:\n  13  comment attachment\n  14  comment image\n  15  workitem attachment field\n  16  workitem rich-text field image",
		Kind:        attachmentShortcutUpload,
		SiblingName: "prepare-upload",
		Args: []registry.ArgDef{
			{Name: "source-path", Required: true, Description: "Local file path to upload"},
		},
		Flags: []registry.FlagDef{
			{Name: "resource-type", Type: registry.FlagTypeString, Required: true, Description: "Backend resource type — 13=comment-attachment | 14=comment-image | 15=attachment-field | 16=rich-text-field-image"},
			{Name: "project-key", Type: registry.FlagTypeString, Required: true, Description: "Lark project key"},
			{Name: "work-item-id", Type: registry.FlagTypeString, Description: "Target work item ID — preferred whenever the workitem already exists (takes precedence if both flags are given)"},
			{Name: "work-item-type", Type: registry.FlagTypeString, Description: "Workitem type key — fallback for create-with-attachment only (no workitem id yet); ignored when --work-item-id is set"},
			{Name: "field-key", Type: registry.FlagTypeString, Description: "Field key (required for --resource-type 15 / 16)"},
			{Name: "filename", Type: registry.FlagTypeString, Description: "Override file name sent to backend (default: base name of <source-path>)"},
			{Name: "content-type", Type: registry.FlagTypeString, Description: "Override MIME type (default: detected from extension, fallback application/octet-stream)"},
		},
	},
	{
		Name:        "+download",
		Brief:       "Download an attachment end-to-end (preprocess + signed HTTP GET + atomic write)",
		Long:        "Composes `attachment prepare-download` with the signed HTTP GET(s) and writes via a `.partial` temp file + os.Rename so a failed download never leaves a half-written file at the destination.",
		Kind:        attachmentShortcutDownload,
		SiblingName: "prepare-download",
		Args: []registry.ArgDef{
			{Name: "file-url", Required: true, Description: "Opaque file URL embedded in another command's response"},
		},
		Flags: []registry.FlagDef{
			{Name: "project-key", Type: registry.FlagTypeString, Required: true, Description: "Lark project key"},
			{Name: "work-item-id", Type: registry.FlagTypeString, Required: true, Description: "Source work item ID"},
			{Name: "output", Type: registry.FlagTypeString, Required: true, Description: "Local destination path"},
			{Name: "overwrite", Type: registry.FlagTypeBool, Description: "Allow overwriting an existing file at --output"},
		},
	},
}

// injectAttachmentCommands attaches the +upload / +download
// shortcuts under the MCP-discovered attachment group. The basic prepare-*
// commands themselves come from MCP discovery via dynamic.fallbackTable
// (upload_file / get_download_url → attachment.prepare-upload / prepare-download),
// so this only adds the scenario shortcuts that compose them with HTTP I/O.
//
// Silent no-op when the attachment group is absent (MCP discovery failed or
// the backend dropped these tools) — shortcuts can't dispatch without their
// basic siblings, and showing dangling shortcut nodes would mislead users.
//
// Returns the nodes slice unchanged for symmetry with injectBatchCommands.
func injectAttachmentCommands(nodes []*registry.CommandNode) []*registry.CommandNode {
	group := findNodeByName(nodes, "attachment")
	if group == nil {
		return nodes
	}

	for _, sc := range attachmentShortcuts {
		if hasChildNamed(group, sc.Name) {
			continue
		}
		// Sibling-based wiring (mirrors batch.go::injectBatchCommands):
		// inherit HandlerRef + mcp_param_types from the prepare-* basic
		// command so a backend rename of the underlying MCP tool only
		// requires updating dynamic.fallbackTable. Silent no-op when the
		// sibling is missing keeps the tree well-formed.
		sibling := findChild(group, sc.SiblingName)
		if sibling == nil {
			continue
		}
		tags := map[string]string{TagAttachmentShortcut: sc.Kind}
		// Copy MCP parameter metadata so any future direct-MCP path the
		// shortcut might need stays byte-identical to the basic command's
		// wire payload. Mirrors deriveBatchFlagsAndTags in batch.go.
		for _, key := range []string{"mcp_param_types", "mcp_param_items", "mcp_fixed_params"} {
			if raw, ok := sibling.Meta.Tags[key]; ok && raw != "" {
				tags[key] = raw
			}
		}
		group.Children = append(group.Children, &registry.CommandNode{
			Name:    sc.Name,
			Aliases: sc.Aliases,
			Help: registry.HelpText{
				Brief: sc.Brief,
				Long:  sc.Long,
			},
			Args:       sc.Args,
			Flags:      sc.Flags,
			HandlerRef: sibling.HandlerRef, // inherited from the basic prepare-* command
			Meta:       registry.NodeMeta{Tags: tags},
		})
	}

	return nodes
}

// ---------------------------------------------------------------------------
// AttachmentShortcutStep — orchestrates +upload / +download.
//
// Activates when the routed node carries TagAttachmentShortcut. McpExecutorStep
// short-circuits on the same tag, so basic commands continue through the
// standard MCP path and shortcuts run here instead.
// ---------------------------------------------------------------------------

// AttachmentShortcutStep dispatches +upload / +download by
// composing PreprocessUpload+ExecuteUpload (or PreprocessDownload+ExecuteDownload).
// Test seams allow substituting the MCP client and HTTP doer; production
// builds the MCP client via newMcpClientFromState (the shared session-aware
// constructor that BatchExecutorStep also uses).
type AttachmentShortcutStep struct {
	clientFactory func(*pipeline.PipelineContext) attachment.MCPClient
	httpFactory   func() attachment.HTTPDoer
}

func (s *AttachmentShortcutStep) Name() string { return "attachment_shortcut" }

func (s *AttachmentShortcutStep) Execute(ctx context.Context, state *pipeline.PipelineContext) error {
	if state == nil || state.Parsed == nil || state.Parsed.Node == nil {
		return nil
	}
	kind := state.Parsed.Node.Meta.Tags[TagAttachmentShortcut]
	if kind == "" {
		return nil
	}
	if state.Values == nil {
		state.Values = pipeline.BuildInputValues(state.Parsed)
	}

	// Validate flag combinations BEFORE the dry-run short-circuit so a
	// `--dry-run` invocation surfaces business-rule errors (mutual-required
	// work-item-id/type, conditional field-key) instead of silently echoing
	// the broken input. This is also the only spot that runs before runUpload
	// opens the local file — fail-fast keeps the error message about the
	// missing flag, not an unrelated open(2) failure later in the chain.
	if err := validateAttachmentShortcutInputs(state, kind); err != nil {
		return err
	}

	// --dry-run preview the structured input the shortcut would consume,
	// without opening files or performing MCP/HTTP. Mirrors McpExecutorStep's
	// dry-run shape so `--format json --dry-run` output is comparable.
	if isDryRunFlag(state.Parsed) {
		state.Result = &executor.RawResult{
			Data: map[string]any{
				"shortcut": state.Parsed.Node.FullPathString(),
				"kind":     kind,
				"values":   state.Values,
				"dry_run":  true,
			},
		}
		return nil
	}

	switch kind {
	case attachmentShortcutUpload:
		return s.runUpload(ctx, state)
	case attachmentShortcutDownload:
		return s.runDownload(ctx, state)
	}
	return meerrors.NewClientError("CLIENT_MISCONFIGURED",
		fmt.Sprintf("unknown attachment shortcut kind %q", kind))
}

// validateAttachmentShortcutInputs enforces the business rules the framework
// can't express through FlagDef.Required:
//   - mutual-required pairs (work-item-id ⊕ work-item-type for upload)
//   - conditional-required (field-key when scene needs it)
//   - --resource-type must parse as int and be one of 13/14/15/16
//
// MeegleValidateStep already covers FlagDef.Required and ArgDef.Required for
// the entire tree; this layer only adds the attachment-specific contracts.
func validateAttachmentShortcutInputs(state *pipeline.PipelineContext, kind string) error {
	switch kind {
	case attachmentShortcutUpload:
		return validateUploadShortcutInputs(state)
	case attachmentShortcutDownload:
		return validateDownloadShortcutInputs(state)
	}
	return nil
}

func validateUploadShortcutInputs(state *pipeline.PipelineContext) error {
	rt, err := strconv.Atoi(stringFlag(state, "resource-type"))
	if err != nil {
		return meerrors.NewClientError("CLIENT_INVALID_VALUE",
			fmt.Sprintf("--resource-type must be an integer (13|14|15|16), got %q", stringFlag(state, "resource-type"))).
			WithSuggestion("meegle attachment +upload --help")
	}
	if err := attachment.ValidateResourceType(rt); err != nil {
		return meerrors.NewClientError("CLIENT_INVALID_VALUE", err.Error()).
			WithSuggestion("meegle attachment +upload --help")
	}
	if stringFlag(state, "work-item-id") == "" && stringFlag(state, "work-item-type") == "" {
		return meerrors.NewClientError("CLIENT_MISSING_REQUIRED",
			"one of --work-item-id or --work-item-type is required "+
				"(use --work-item-id to attach to an existing workitem, or --work-item-type "+
				"when preparing an attachment for a workitem you're about to create)").
			WithSuggestion("meegle attachment +upload --help")
	}
	if attachment.RequiresFieldKey(rt) && stringFlag(state, "field-key") == "" {
		return meerrors.NewClientError("CLIENT_MISSING_REQUIRED",
			fmt.Sprintf("--field-key is required for --resource-type %d", rt)).
			WithSuggestion("meegle attachment +upload --help")
	}
	return nil
}

func validateDownloadShortcutInputs(state *pipeline.PipelineContext) error {
	// All download flags are FlagDef.Required + ArgDef.Required (file-url),
	// so MeegleValidateStep already covers them. Reserved as a hook for any
	// future business-rule additions without changing the dispatch shape.
	return nil
}

func (s *AttachmentShortcutStep) runUpload(ctx context.Context, state *pipeline.PipelineContext) error {
	// Pre-flight validation already covered resource-type / mutual-required
	// flags / conditional field-key in validateUploadShortcutInputs (run from
	// Execute), and MeegleValidateStep covered FlagDef.Required +
	// ArgDef.Required. By this point all flag-shape errors have been raised.
	sourcePath, _ := state.Values["source-path"].(string)
	rt, _ := strconv.Atoi(stringFlag(state, "resource-type"))

	f, err := os.Open(sourcePath)
	if err != nil {
		return meerrors.NewClientError("CLIENT_INVALID_PARAM",
			fmt.Sprintf("open %s: %s", sourcePath, err))
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", sourcePath, err)
	}
	if st.IsDir() {
		return meerrors.NewClientError("CLIENT_INVALID_PARAM",
			fmt.Sprintf("%s is a directory", sourcePath))
	}

	filename := stringFlag(state, "filename")
	if filename == "" {
		filename = filepath.Base(sourcePath)
	}
	contentType := stringFlag(state, "content-type")
	if contentType == "" {
		contentType = attachment.DefaultContentType(sourcePath)
	}

	in := attachment.UploadInput{
		ResourceType: rt,
		ProjectKey:   stringFlag(state, "project-key"),
		WorkItemID:   stringFlag(state, "work-item-id"),
		WorkItemType: stringFlag(state, "work-item-type"),
		FieldKey:     stringFlag(state, "field-key"),
		File:         f,
		FileSize:     st.Size(),
		Filename:     filename,
		ContentType:  contentType,
	}

	mcp := s.resolveClient(state)
	doer := s.resolveDoer()

	// Inherited from the prepare-upload sibling at injection time. Passing
	// it explicitly keeps "what tool gets called" anchored to the basic
	// command's HandlerRef — the single source of truth.
	toolName := state.Parsed.Node.HandlerRef

	_, pre, err := attachment.PreprocessUpload(ctx, mcp, toolName, in)
	if err != nil {
		return err
	}
	result, err := attachment.ExecuteUpload(ctx, doer, pre, in)
	if err != nil {
		return err
	}

	state.Result = &executor.RawResult{Data: result}
	return nil
}

func (s *AttachmentShortcutStep) runDownload(ctx context.Context, state *pipeline.PipelineContext) error {
	// Pre-flight validation already covered required flags + ArgDef.Required.
	fileURL, _ := state.Values["file-url"].(string)

	in := attachment.DownloadInput{
		ProjectKey: stringFlag(state, "project-key"),
		WorkItemID: stringFlag(state, "work-item-id"),
		FileURL:    fileURL,
		Output:     stringFlag(state, "output"),
		Overwrite:  boolFlag(state, "overwrite"),
		Headers:    downloadGETHeaders(state),
	}

	mcp := s.resolveClient(state)
	doer := s.resolveDoer()

	// Inherited from the prepare-download sibling at injection time.
	toolName := state.Parsed.Node.HandlerRef

	_, pre, err := attachment.PreprocessDownload(ctx, mcp, toolName, in)
	if err != nil {
		return err
	}
	result, err := attachment.ExecuteDownload(ctx, doer, pre, in)
	if err != nil {
		return mapDownloadError(err)
	}

	state.Result = &executor.RawResult{Data: result}
	return nil
}

// mapDownloadError translates the attachment package's file-signature integrity
// guard failures into a user-facing CLIENT_* error with a retry hint. The bug
// it guards against — a CDN cache occasionally serving the wrong object —
// usually clears on retry, so both variants steer the user to re-run the
// command. Any other error passes through unchanged.
func mapDownloadError(err error) error {
	switch {
	case stderrors.Is(err, attachment.ErrFileSignMismatch):
		return meerrors.NewClientError("CLIENT_FILE_SIGN_MISMATCH",
			fmt.Sprintf("downloaded file failed its integrity check (%v); download failed", err)).
			WithSuggestion("The download returned content for a different signature, likely a stale CDN cache. Please retry `meegle attachment +download`.")
	case stderrors.Is(err, attachment.ErrFileSignMissing):
		return meerrors.NewClientError("CLIENT_FILE_SIGN_UNVERIFIED",
			fmt.Sprintf("cannot verify the downloaded file's integrity (%v); download failed", err)).
			WithSuggestion("The download response did not include the signature header needed to verify the file. Please retry `meegle attachment +download`.")
	default:
		return err
	}
}

// downloadGETHeaders returns the profile's configured custom headers for use on
// the object-storage download GET, with auth-bearing headers stripped so the
// Meegle token is never leaked to that host. This lets environment / lane
// routing headers configured for the MCP calls also pin the download GET to the
// same lane. Mirrors the auth-stripping in newMcpClientFromState. Returns nil
// when nothing forwardable remains.
func downloadGETHeaders(state *pipeline.PipelineContext) map[string]string {
	hdrs, _ := state.OutputConfig["mcp.headers"].(map[string]string)
	if len(hdrs) == 0 {
		return nil
	}
	authHeader, _ := state.OutputConfig["mcp.access_token_header"].(string)
	out := make(map[string]string, len(hdrs))
	for k, v := range hdrs {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		if authHeader != "" && strings.EqualFold(k, authHeader) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveClient returns the test-injected MCPClient or, in production, wraps
// the session-bound mcpclient.Client with the narrow attachment.MCPClient
// adapter. Mirrors BatchExecutorStep.resolveClient.
func (s *AttachmentShortcutStep) resolveClient(state *pipeline.PipelineContext) attachment.MCPClient {
	if s.clientFactory != nil {
		return s.clientFactory(state)
	}
	return attachmentMCPClientAdapter{inner: newMcpClientFromState(state)}
}

func (s *AttachmentShortcutStep) resolveDoer() attachment.HTTPDoer {
	if s.httpFactory != nil {
		return s.httpFactory()
	}
	return http.DefaultClient
}

// attachmentMCPClientAdapter narrows *mcpclient.Client.CallTool to the
// attachment.MCPClient interface so the attachment package stays decoupled
// from the MCP transport implementation. Distinct from batch.go's
// mcpClientAdapter (which returns *mcpResponse) to avoid forcing one of them
// to depend on the other's type.
type attachmentMCPClientAdapter struct {
	inner *mcpclient.Client
}

func (a attachmentMCPClientAdapter) CallTool(ctx context.Context, name string, params map[string]any) (*attachment.MCPResponse, error) {
	resp, err := a.inner.CallTool(ctx, name, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return &attachment.MCPResponse{Data: resp.Data}, nil
}

// stringFlag reads a string flag from state.Values, defaulting to "".
func stringFlag(state *pipeline.PipelineContext, name string) string {
	if state == nil || state.Values == nil {
		return ""
	}
	s, _ := state.Values[name].(string)
	return s
}

// boolFlag reads a bool flag from state.Values, defaulting to false.
func boolFlag(state *pipeline.PipelineContext, name string) bool {
	if state == nil || state.Values == nil {
		return false
	}
	b, _ := state.Values[name].(bool)
	return b
}
