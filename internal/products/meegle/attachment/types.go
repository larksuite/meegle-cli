// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package attachment contains the upload/download flow composed by the
// `meegle attachment +upload` / `+download` shortcut commands. Each shortcut
// chains one preprocess call (surfaced separately as `prepare-upload` /
// `prepare-download` basic commands) with one or more out-of-band HTTP
// requests to object storage.
//
// The split into `Preprocess*` (preprocess-only) + `Execute*` (HTTP-only)
// lets the shortcut step reuse the same param construction and response
// parsing the basic commands rely on.
package attachment

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Resource type constants match the backend contract:
//
//	13 = comment attachment
//	14 = comment image
//	15 = workitem attachment field
//	16 = workitem rich-text field image
const (
	ResourceTypeCommentAttachment  = 13
	ResourceTypeCommentImage       = 14
	ResourceTypeAttachmentField    = 15
	ResourceTypeRichTextFieldImage = 16
)

// ValidateResourceType reports whether rt is a recognised backend resource_type.
func ValidateResourceType(rt int) error {
	switch rt {
	case ResourceTypeCommentAttachment, ResourceTypeCommentImage,
		ResourceTypeAttachmentField, ResourceTypeRichTextFieldImage:
		return nil
	default:
		return fmt.Errorf("unknown --resource-type %d; expected one of 13|14|15|16", rt)
	}
}

// RequiresFieldKey reports whether the resource_type targets a workitem field
// (and therefore needs --field-key). Comment-scope resource types (13/14) do
// not target a specific workitem field.
func RequiresFieldKey(rt int) bool {
	return rt == ResourceTypeAttachmentField || rt == ResourceTypeRichTextFieldImage
}

// MCPClient is the narrow slice of *mcpclient.Client used by the upload and
// download flows. Tests substitute fakes without touching HTTP transport.
type MCPClient interface {
	CallTool(ctx context.Context, name string, params map[string]any) (*MCPResponse, error)
}

// MCPResponse is the only field of *mcpclient.Response the flows consume.
// The adapter in the commands/ layer narrows the real client's richer type
// down to this.
type MCPResponse struct {
	Data any
}

// HTTPDoer is the narrow slice of *http.Client needed for the object-storage
// POST/GET calls. Tests plug in httptest.Server-backed clients.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// UploadInput carries all the per-upload parameters. File must be seekable so
// chunk ranges can be read without reopening the file.
//
// WorkItemID takes precedence over WorkItemType — always supply WorkItemID
// when the target workitem exists (update / comment scenarios). WorkItemType
// is ONLY for the create-with-attachment path where the workitem doesn't
// exist yet and we need to scope the preprocess by type instead. If both
// are provided, WorkItemID wins (type is dropped before sending downstream).
type UploadInput struct {
	ResourceType int
	ProjectKey   string
	WorkItemID   string // preferred when available; uniquely identifies the target
	WorkItemType string // fallback ONLY when no WorkItemID exists yet (create path)
	FieldKey     string // required iff RequiresFieldKey(ResourceType)
	File         io.ReadSeeker
	FileSize     int64
	Filename     string
	ContentType  string
}

// UploadResult is returned on success. FileToken is the identifier downstream
// commands (add_comment, workitem update, etc.) plug into their arguments.
type UploadResult struct {
	FileToken string `json:"file_token"`
	FileURL   string `json:"file_url"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
}

// DownloadInput carries all the per-download parameters.
type DownloadInput struct {
	ProjectKey string
	WorkItemID string
	FileURL    string // opaque reference received from another MCP tool
	Output     string // local destination path
	Overwrite  bool
	// Headers are extra HTTP headers set on each object-storage GET — used to
	// carry environment / lane routing headers so a download can be pinned to
	// the same lane as the preprocess call. The caller is
	// responsible for stripping auth-bearing headers so the Meegle token is
	// never leaked to the object-storage host. The signature header always
	// takes precedence over anything supplied here.
	Headers map[string]string
}

// DownloadResult is returned on success. Name / MimeType are best-effort from
// response headers and may be empty when the object store doesn't surface them.
type DownloadResult struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// partNumberPlaceholder is the literal the MCP backend embeds in upload_url /
// download_url for CLI-side substitution. Keeping it as a single constant
// keeps the two flows in sync if the backend ever changes the placeholder.
const partNumberPlaceholder = ":part_number"

// signHeader is the HTTP header carrying the per-request signature returned by
// the MCP preprocess tool. The upload and download flows send it on each
// object-storage request; on download the backend also echoes it back on the
// response, and +download compares the echoed value against the requested
// signature to verify the file's correctness — guarding against a CDN cache
// occasionally serving the wrong object. The guard is fail-closed: a missing
// or mismatched header aborts the download before any bytes reach the
// destination. HTTP header names are case-insensitive (http.Header.Get
// canonicalises), and the value is compared case-insensitively too.
const signHeader = "X-Meego-File-Sign"
