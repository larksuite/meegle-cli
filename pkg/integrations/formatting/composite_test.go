// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package formatting

import (
	"strings"
	"testing"

	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/output/formatter"
)

func TestComposite_DelegatesJSONToFramework(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	out, err := c.Format(map[string]any{"a": 1}, &frameworkoutput.FormatOptions{Mode: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"a"`) {
		t.Fatalf("%s", out)
	}
}

func TestComposite_DelegatesNDJSONToFramework(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	out, err := c.Format(
		[]any{map[string]any{"a": 1}, map[string]any{"a": 2}},
		&frameworkoutput.FormatOptions{Mode: "ndjson"},
	)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 ndjson lines, got %d: %q", len(lines), out)
	}
}

func TestComposite_UsesTableViewForTable(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	out, err := c.Format(
		[]any{map[string]any{"id": 1, "name": "a"}},
		&frameworkoutput.FormatOptions{Mode: "table"},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(strings.ToLower(s), "id") {
		t.Fatalf("want table headers: %s", s)
	}
}

func TestComposite_DefaultModeIsJSON(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	out, err := c.Format(map[string]any{"a": 1}, &frameworkoutput.FormatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"a"`) {
		t.Fatalf("default should be json: %s", out)
	}
}

// Unwrap behavior after the metadata-preserving redesign:
//   - Multi-key wrappers ({list, total}) are never peeled — every format
//     sees the full response, callers drill in explicitly with --select.
//   - Single-key wrappers ({list: [...]}) are still peeled before
//     ndjson/table rendering because that peel is loss-less.
//   - JSON never peels regardless of key count.

// Multi-key wrapper under ndjson stays as a single JSON object line so
// total/pagination metadata is preserved.
func TestComposite_NDJSON_KeepsMultiKeyWrapper(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"list": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
			map[string]any{"id": 3},
		},
		"total": 3,
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "ndjson"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("multi-key wrapper → 1 line (metadata preserved), got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"total":3`) {
		t.Fatalf("total sibling must survive: %q", lines[0])
	}
	if !strings.Contains(lines[0], `"list"`) {
		t.Fatalf("list key must survive: %q", lines[0])
	}
}

// Single-key wrapper under ndjson still peels so each record streams on
// its own line — the peel is loss-less (no siblings to drop).
func TestComposite_NDJSON_UnwrapsSingleKeyWrapper(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"list": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "ndjson"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("single-key wrapper → per-record lines, got %d: %q", len(lines), out)
	}
}

func TestComposite_JSON_NeverUnwraps(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"list":  []any{map[string]any{"id": 1}},
		"total": 1,
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "json"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"list"`) || !strings.Contains(s, `"total"`) {
		t.Fatalf("json must preserve wrapper: %s", s)
	}
}

// Multi-key wrapper under table renders as a 2-row KV so total/pagination
// are visible — users drill into the records with --select=list.
func TestComposite_Table_KeepsMultiKeyAsKV(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"list": []any{
			map[string]any{"id": 1, "name": "a"},
			map[string]any{"id": 2, "name": "b"},
		},
		"total": 2,
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "list") || !strings.Contains(s, "total") {
		t.Fatalf("both wrapper keys must appear as KV rows: %s", s)
	}
	if !strings.Contains(s, "2") {
		t.Fatalf("total value must appear: %s", s)
	}
}

// Single-key wrapper under table still peels to row format — loss-less
// and preserves the existing drill-through UX when the user has already
// narrowed down with --select.
func TestComposite_Table_UnwrapsSingleKeyToRows(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"list": []any{
			map[string]any{"id": 1, "name": "a"},
			map[string]any{"id": 2, "name": "b"},
		},
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(strings.ToLower(s), "id") || !strings.Contains(strings.ToLower(s), "name") {
		t.Fatalf("single-key wrapper should unwrap to rows, got: %s", s)
	}
}

// With --envelope the wrapper is the envelope itself; DO NOT unwrap it.
func TestComposite_EnvelopeDisablesUnwrap(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	// Simulate EnvelopeHook output.
	envelope := map[string]any{
		"data":  []any{map[string]any{"id": 1}},
		"meta":  map[string]any{"tool": "list_items"},
		"error": nil,
	}
	out, err := c.Format(envelope, &frameworkoutput.FormatOptions{Mode: "ndjson", Envelope: true})
	if err != nil {
		t.Fatal(err)
	}
	// Envelope must survive: one line containing {data, meta, error}.
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("envelope mode must keep 1 line, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"meta"`) {
		t.Fatalf("envelope meta must be preserved: %q", lines[0])
	}
}

// Single-object responses (e.g. workitem get) should NOT be unwrapped —
// UnwrapWrapperObject only triggers on {wrapperKey: []}, not on plain maps.
func TestComposite_NDJSON_SingleObjectStillOneLine(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{"work_item_id": 1, "name": "foo"}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "ndjson"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("single object → 1 line, got %d: %q", len(lines), out)
	}
}

// Post-select drill-in: --select=work_item_attribute selects a nested
// object out of a multi-key record; CompositeFormatter must peel the
// single-key projection so table shows the inner KV rows instead of
// stuffing the whole object into one cell.
func TestComposite_Table_SelectObjectFieldDrillsIn(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	// Shape after FieldSelectorHook on a workitem-get-style payload.
	data := map[string]any{
		"creator": map[string]any{"name": "alice", "email": "a@x"},
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Expect KV rows for name/email, not a one-row "creator → {...}".
	if !strings.Contains(s, "alice") || !strings.Contains(s, "a@x") {
		t.Fatalf("want drilled KV rows, got: %s", s)
	}
	if strings.Contains(s, `{"email":"a@x","name":"alice"}`) ||
		strings.Contains(s, `{"name":"alice","email":"a@x"}`) {
		t.Fatalf("object should have been peeled, not inlined: %s", s)
	}
}

// --select=a.b.c produces {a: {b: {c: val}}}; CompositeFormatter should
// drill through the chain to show c as the sole KV row.
func TestComposite_Table_DrillsDottedSelect(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{
		"a": map[string]any{"b": map[string]any{"c": "leaf"}},
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "leaf") {
		t.Fatalf("want leaf value visible, got: %s", s)
	}
	// The outer keys should have been peeled away.
	if strings.Contains(s, `"b":`) {
		t.Fatalf("intermediate wrapper still present: %s", s)
	}
}

// Single-key with scalar value must NOT be drilled — stays as a 1-row
// KV table so the user sees the key name.
func TestComposite_Table_SingleKeyScalarStays(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	data := map[string]any{"status": "ok"}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "status") || !strings.Contains(s, "ok") {
		t.Fatalf("want KV row status/ok, got: %s", s)
	}
}

// Unknown --format values must surface the enum-validation error with the
// supported set spelled out. Enum validation upstream normally catches the
// typo first; this test pins the defensive branch so a refactor cannot
// silently drop it (and the error message stays in sync with the design
// spec's documented mode set).
func TestComposite_UnknownModeReturnsEnumError(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	_, err := c.Format(map[string]any{"a": 1}, &frameworkoutput.FormatOptions{Mode: "xml"})
	if err == nil {
		t.Fatal("want error for unknown --format")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unsupported --format") {
		t.Fatalf("message should call out the failed flag: %q", msg)
	}
	for _, allowed := range []string{"json", "ndjson", "table"} {
		if !strings.Contains(msg, allowed) {
			t.Fatalf("message must list %q as allowed: %q", allowed, msg)
		}
	}
}

// Error-envelope contract: when opts.ErrorEnvelope is set, json passes the
// pre-built {data, meta, error} map through verbatim — no unwrap, no peel.
func TestComposite_ErrorEnvelope_JSON_PassesThrough(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	env := map[string]any{
		"data":  nil,
		"meta":  map[string]any{"tool": "list"},
		"error": map[string]any{"code": "BAD", "message": "boom", "retryable": false},
	}
	out, err := c.Format(env, &frameworkoutput.FormatOptions{Mode: "json", ErrorEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"data"`, `"meta"`, `"error"`, `"code"`, `"BAD"`, `"retryable"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
}

// ndjson must emit the full envelope on a single line — agents consuming
// line-by-line streams expect one record per line, and errors are one record.
func TestComposite_ErrorEnvelope_NDJSON_SingleLine(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	env := map[string]any{
		"data":  nil,
		"meta":  map[string]any{},
		"error": map[string]any{"code": "X", "message": "x", "retryable": true},
	}
	out, err := c.Format(env, &frameworkoutput.FormatOptions{Mode: "ndjson", ErrorEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"code"`) || !strings.Contains(lines[0], `"retryable":true`) {
		t.Fatalf("line missing error fields: %q", lines[0])
	}
}

// table drills into the `error` record and renders KV rows — the spec's
// three-row code/message/retryable view. It does NOT render the outer
// envelope as a KV of {data, meta, error}, which would be useless.
func TestComposite_ErrorEnvelope_Table_KVOfErrorRecord(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	env := map[string]any{
		"data":  nil,
		"meta":  map[string]any{"tool": "list"},
		"error": map[string]any{"code": "BAD_INPUT", "message": "missing flag", "retryable": false},
	}
	out, err := c.Format(env, &frameworkoutput.FormatOptions{Mode: "table", ErrorEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"code", "BAD_INPUT", "message", "missing flag", "retryable", "false"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in table:\n%s", want, s)
		}
	}
	// The outer envelope keys must NOT leak into the header.
	if strings.Contains(s, "data") && strings.Contains(s, "meta") {
		t.Fatalf("outer envelope leaked into error table:\n%s", s)
	}
}

func TestComposite_ErrorEnvelope_TableDoesNotTruncateActionableFields(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	message := "missing required parameters: --work-item-type, --user-key, --project-key"
	suggestion := "meegle workflow list-state-transitions --help"
	env := map[string]any{
		"data": nil,
		"meta": map[string]any{},
		"error": map[string]any{
			"code":       "CLIENT_MISSING_REQUIRED",
			"message":    message,
			"retryable":  false,
			"suggestion": suggestion,
		},
	}
	out, err := c.Format(env, &frameworkoutput.FormatOptions{Mode: "table", ErrorEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{message, suggestion} {
		if !strings.Contains(s, want) {
			t.Fatalf("error table truncated %q:\n%s", want, s)
		}
	}
}

// Error envelope must NEVER be unwrapped even though the outer payload is a
// three-key object — ErrorEnvelope=true takes priority over any unwrap rule.
func TestComposite_ErrorEnvelope_NeverUnwraps(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	env := map[string]any{
		"data":  nil,
		"meta":  map[string]any{},
		"error": map[string]any{"code": "X", "message": "x", "retryable": false},
	}
	out, err := c.Format(env, &frameworkoutput.FormatOptions{Mode: "ndjson", ErrorEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	// If unwrap had fired, the envelope's `error` key could have been peeled
	// into separate rows. One-line output proves it stayed intact.
	if n := strings.Count(strings.TrimSuffix(string(out), "\n"), "\n"); n != 0 {
		t.Fatalf("envelope was split across lines: %q", out)
	}
}

// Unknown --format still produces the enum error even under ErrorEnvelope so
// typos in --format are reported uniformly in the success and error paths.
func TestComposite_ErrorEnvelope_UnknownModeErrors(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	_, err := c.Format(map[string]any{"error": nil}, &frameworkoutput.FormatOptions{Mode: "xml", ErrorEnvelope: true})
	if err == nil {
		t.Fatalf("want error for unknown format")
	}
	if !strings.Contains(err.Error(), "unsupported --format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Selecting an array-valued key out of a multi-key record: the post-select
// shape is {KEY: [item1, item2, ...]} → unwrap peels KEY away → table
// renders the array as rows with headers from item1's fields (not from
// the now-stale "KEY" selection).
func TestComposite_Table_SelectArrayFieldInRecord(t *testing.T) {
	c := NewComposite(formatter.Standard{}, DefaultTableView())
	// Shape after FieldSelectorHook on --select=current_node against a
	// multi-key record.
	data := map[string]any{
		"current_node": []any{
			map[string]any{"id": "a", "name": "node-a"},
			map[string]any{"id": "b", "name": "node-b"},
		},
	}
	out, err := c.Format(data, &frameworkoutput.FormatOptions{Mode: "table", Select: []string{"current_node"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "node-a") || !strings.Contains(s, "node-b") {
		t.Fatalf("want rows for both items, got: %s", s)
	}
	// The outer "current_node" key must NOT appear as a header.
	if strings.Contains(strings.ToUpper(s), "CURRENT NODE") {
		t.Fatalf("stale outer key leaked into header: %s", s)
	}
}
