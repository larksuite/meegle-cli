// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"bytes"
	"strings"
	"testing"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
)

func newTV() TableView {
	v := DefaultTableView()
	v.Stderr = &bytes.Buffer{} // swallow hints in tests
	return v
}

func TestTable_ArrayOfObjects_DefaultColumns(t *testing.T) {
	data := []any{
		map[string]any{"id": 1, "name": "a", "extra": "x"},
		map[string]any{"id": 2, "name": "b", "extra": "y"},
	}
	out, err := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "ID") && !strings.Contains(s, "id") {
		t.Fatalf("missing id column: %s", s)
	}
	if !strings.Contains(s, "NAME") && !strings.Contains(s, "name") {
		t.Fatalf("missing name column: %s", s)
	}
}

func TestTable_SingleObject_KV(t *testing.T) {
	data := map[string]any{"id": 1, "name": "alice"}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table"})
	s := string(out)
	if !strings.Contains(s, "id") || !strings.Contains(s, "alice") {
		t.Fatalf("%s", s)
	}
}

func TestTable_PrimitiveArray_SingleColumn(t *testing.T) {
	data := []any{"a", "b", "c"}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table"})
	s := strings.TrimSpace(string(out))
	lines := strings.Split(s, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), s)
	}
}

func TestTable_Primitive(t *testing.T) {
	out, _ := newTV().Encode("hello", &frameworkoutput.FormatOptions{Mode: "table"})
	if strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("%q", out)
	}
}

func TestTable_Empty(t *testing.T) {
	out, _ := newTV().Encode([]any{}, &frameworkoutput.FormatOptions{Mode: "table"})
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("want empty stdout: %q", out)
	}
}

func TestTable_SelectHonored(t *testing.T) {
	data := []any{
		map[string]any{"id": 1, "name": "a", "extra": "x"},
		map[string]any{"id": 2, "name": "b", "extra": "y"},
	}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"extra", "id"}})
	s := string(out)
	// extra appears before id when explicit-selected
	extraIdx := strings.Index(strings.ToLower(s), "extra")
	idIdx := strings.Index(strings.ToLower(s), "id")
	if extraIdx < 0 || idIdx < 0 {
		t.Fatalf("missing columns: %s", s)
	}
	if extraIdx > idIdx {
		t.Fatalf("want extra before id, got %d vs %d\n%s", extraIdx, idIdx, s)
	}
}

func TestTable_NestedObjectAsCompactJSON(t *testing.T) {
	data := []any{
		map[string]any{"id": 1, "creator": map[string]any{"name": "alice", "email": "a@x"}},
	}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"id", "creator"}})
	s := string(out)
	if !strings.Contains(s, `{"email":"a@x","name":"alice"}`) && !strings.Contains(s, `{"name":"alice"`) {
		t.Fatalf("want inline JSON in cell: %s", s)
	}
}

func TestTable_PriorityOrder(t *testing.T) {
	// id is in priority list; avatar_url isn't. id should come first.
	data := []any{
		map[string]any{"avatar_url": "x", "id": 1, "zebra": "z"},
	}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table"})
	s := strings.ToLower(string(out))
	idIdx := strings.Index(s, "id")
	avatarIdx := strings.Index(s, "avatar")
	if idIdx < 0 || avatarIdx < 0 {
		t.Fatalf("missing: %s", s)
	}
	if idIdx > avatarIdx {
		t.Fatalf("id should precede avatar_url: %s", s)
	}
}

// Per spec §"形状 A": --select=a,b,c renders the keys in order; missing keys
// produce empty cells, never silently dropped. Before the #3 fix, `missing`
// was filtered out and the user never saw they typed a bad key.
func TestTable_SelectPartialMiss_RendersEmptyCells(t *testing.T) {
	data := []any{
		map[string]any{"id": 1, "name": "a"},
		map[string]any{"id": 2, "name": "b"},
	}
	tv := DefaultTableView()
	stderr := &bytes.Buffer{}
	tv.Stderr = stderr
	out, err := tv.Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"id", "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Both requested columns must appear in headers (case-insensitive match
	// because tablewriter uppercases).
	if !strings.Contains(strings.ToLower(s), "missing") {
		t.Fatalf("want missing column header, got:\n%s", s)
	}
	if !strings.Contains(strings.ToLower(s), "id") {
		t.Fatalf("want id column header, got:\n%s", s)
	}
	// Stderr hint must call out the dropped key so typos are not silent.
	if !strings.Contains(stderr.String(), "ignoring unknown --select keys: missing") {
		t.Fatalf("expected stderr hint for dropped key, got: %q", stderr.String())
	}
}

// KV mode honors the same contract: --select=id,missing on a single object
// renders both rows (missing value empty) and hints on stderr.
func TestTable_KV_SelectPartialMiss_RendersEmptyRow(t *testing.T) {
	data := map[string]any{"id": 1, "name": "alice"}
	tv := DefaultTableView()
	stderr := &bytes.Buffer{}
	tv.Stderr = stderr
	out, _ := tv.Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"id", "missing"}})
	s := string(out)
	// Both keys should appear as rows (lower-cased for tablewriter-agnostic
	// assertion; tablewriter uppercases headers but preserves data rows).
	if !strings.Contains(s, "id") || !strings.Contains(s, "missing") {
		t.Fatalf("want both id and missing rows:\n%s", s)
	}
	if !strings.Contains(stderr.String(), "ignoring unknown --select keys: missing") {
		t.Fatalf("expected stderr hint, got %q", stderr.String())
	}
}

// All-stale --select is the post-unwrap signal: fall through to auto-derive
// and DO NOT emit the "ignoring" hint, because those keys were legitimately
// peeled upstream by UnwrapWrapperObject, not user typos.
func TestTable_SelectAllStale_FallsThroughSilently(t *testing.T) {
	// Simulates: user ran --select=results on {"results":[{id:1,name:"a"}]}
	// → upstream unwrap peeled `results` → TableView sees [{id:1,name:"a"}]
	// and selected=["results"] is now stale.
	data := []any{
		map[string]any{"id": 1, "name": "a"},
	}
	tv := DefaultTableView()
	stderr := &bytes.Buffer{}
	tv.Stderr = stderr
	out, err := tv.Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"results"}})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(out))
	// Auto-derived columns are id + name, not the stale `results`.
	if !strings.Contains(s, "id") || !strings.Contains(s, "name") {
		t.Fatalf("want auto-derived columns id/name, got:\n%s", out)
	}
	if strings.Contains(s, "results") {
		t.Fatalf("stale select key should not appear:\n%s", out)
	}
	// Must NOT hint — post-unwrap is not a user error.
	if strings.Contains(stderr.String(), "ignoring unknown --select keys") {
		t.Fatalf("unexpected stderr hint on post-unwrap path: %q", stderr.String())
	}
}

func TestTable_NestedSelectViaDotPath(t *testing.T) {
	data := []any{
		map[string]any{"id": 1, "creator": map[string]any{"email": "a@x"}},
		map[string]any{"id": 2, "creator": map[string]any{"email": "b@x"}},
	}
	out, _ := newTV().Encode(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"id", "creator.email"}})
	s := string(out)
	if !strings.Contains(s, "a@x") || !strings.Contains(s, "b@x") {
		t.Fatalf("dot-path did not resolve: %s", s)
	}
}
