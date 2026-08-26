// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"net/http"
	"testing"

	"github.com/larksuite/meegle-cli/internal/products/meegle/attachment"
	"github.com/larksuite/meegle-cli/internal/products/meegle/types"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
)

func TestAttachmentShortcutStep_UsesSessionHTTPClient(t *testing.T) {
	want := &http.Client{}
	state := &pipeline.PipelineContext{OutputConfig: map[string]any{"mcp.http_client": want}}
	if got := (&AttachmentShortcutStep{}).resolveDoer(state); got != want {
		t.Fatalf("resolved HTTP client = %p, want %p", got, want)
	}
}

// attachmentDiscoveryFixture mimics the MappedCommands that dynamic.MapTools
// produces from MCP discovery for upload_file / get_download_url (via the
// fallback table). Tests use this so the attachment group + basic prepare-*
// nodes exist before injectAttachmentCommands runs.
func attachmentDiscoveryFixture() []types.MappedCommand {
	return []types.MappedCommand{
		{Resource: "attachment", Method: "prepare-upload", ToolName: attachment.DefaultUploadTool, Description: "upload"},
		{Resource: "attachment", Method: "prepare-download", ToolName: attachment.DefaultDownloadTool, Description: "download"},
	}
}

// TestInjectAttachmentCommands_ShortcutInheritsHandlerRefFromSibling is the
// load-bearing assertion for the sibling-based shortcut design: the +-prefixed
// shortcut nodes must carry the SAME HandlerRef as their basic prepare-*
// sibling so that renaming the underlying MCP tool only requires touching
// dynamic.fallbackTable.
//
// Mirrors the corresponding +batch-get → workitem.get inheritance contract.
func TestInjectAttachmentCommands_ShortcutInheritsHandlerRefFromSibling(t *testing.T) {
	tree := buildCommandTree(attachmentDiscoveryFixture())
	group := findNodeByName(tree.Nodes, "attachment")
	if group == nil {
		t.Fatal("attachment group missing — expected MCP discovery to produce it")
	}

	cases := []struct {
		shortcut   string
		sibling    string
		wantHandle string // not just "any non-empty" — pin to the documented MCP tool
	}{
		{shortcut: "+upload", sibling: "prepare-upload", wantHandle: attachment.DefaultUploadTool},
		{shortcut: "+download", sibling: "prepare-download", wantHandle: attachment.DefaultDownloadTool},
	}
	for _, c := range cases {
		t.Run(c.shortcut, func(t *testing.T) {
			sib := findChild(group, c.sibling)
			sc := findChild(group, c.shortcut)
			if sib == nil || sc == nil {
				t.Fatalf("missing nodes: sibling=%v shortcut=%v", sib, sc)
			}
			if sib.HandlerRef != c.wantHandle {
				t.Errorf("sibling %s HandlerRef=%q, want %q", c.sibling, sib.HandlerRef, c.wantHandle)
			}
			if sc.HandlerRef != sib.HandlerRef {
				t.Errorf("shortcut %s did not inherit HandlerRef from sibling: shortcut=%q sibling=%q",
					c.shortcut, sc.HandlerRef, sib.HandlerRef)
			}
			if sc.Meta.Tags[TagAttachmentShortcut] == "" {
				t.Errorf("shortcut %s missing %s tag — McpExecutorStep would not short-circuit", c.shortcut, TagAttachmentShortcut)
			}
		})
	}
}

// TestInjectAttachmentCommands_NoOpWhenAttachmentGroupMissing verifies the
// silent no-op contract: when MCP discovery does not surface an attachment
// resource (auth failure, backend dropped the tools), inject must NOT
// fabricate a dangling group with orphan shortcuts.
func TestInjectAttachmentCommands_NoOpWhenAttachmentGroupMissing(t *testing.T) {
	tree := buildCommandTree(nil)
	if findNodeByName(tree.Nodes, "attachment") != nil {
		t.Fatal("attachment group should not exist when no MCP attachment tools are discovered")
	}
}

// TestInjectAttachmentCommands_ShortcutSkipsWhenSiblingMissing covers the
// per-shortcut silent no-op (mirrors batch.go). If only one of the two basic
// commands is discovered, only the matching shortcut is injected; the orphan
// shortcut is dropped rather than producing a broken node.
func TestInjectAttachmentCommands_ShortcutSkipsWhenSiblingMissing(t *testing.T) {
	commands := []types.MappedCommand{
		{Resource: "attachment", Method: "prepare-upload", ToolName: attachment.DefaultUploadTool, Description: "upload"},
		// prepare-download intentionally absent
	}
	tree := buildCommandTree(commands)
	group := findNodeByName(tree.Nodes, "attachment")
	if group == nil {
		t.Fatal("attachment group should be present (prepare-upload was discovered)")
	}
	if findChild(group, "+upload") == nil {
		t.Error("+upload should be injected when its prepare-upload sibling is present")
	}
	if findChild(group, "+download") != nil {
		t.Error("+download should be skipped when its prepare-download sibling is missing")
	}
}

// TestInjectAttachmentCommands_AppendsShortcutsToExistingGroup makes sure that
// the shortcuts are appended to the MCP-discovered attachment group rather
// than producing a duplicate top-level node.
func TestInjectAttachmentCommands_AppendsShortcutsToExistingGroup(t *testing.T) {
	commands := []types.MappedCommand{
		{Resource: "attachment", Method: "list", ToolName: "list_attachments", Description: "List"},
		{Resource: "attachment", Method: "prepare-upload", ToolName: attachment.DefaultUploadTool, Description: "upload"},
		{Resource: "attachment", Method: "prepare-download", ToolName: attachment.DefaultDownloadTool, Description: "download"},
	}
	tree := buildCommandTree(commands)

	count := 0
	for _, n := range tree.Nodes {
		if n != nil && n.Name == "attachment" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one attachment group, got %d", count)
	}
	group := findNodeByName(tree.Nodes, "attachment")
	// 3 discovered (list + prepare-upload + prepare-download) + 2 shortcuts = 5 children.
	if len(group.Children) != 5 {
		t.Errorf("expected 5 children (3 discovered + 2 shortcut), got %d", len(group.Children))
	}
}
