// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package fileio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadInputContract(t *testing.T) {
	for _, body := range []string{"", "null", "[]", "123", `{`, `{} {}`, string([]byte{0xff})} {
		t.Run(fmt.Sprintf("invalid_%q", body), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			os.WriteFile(path, []byte(body), 0600)
			if _, err := ReadInput(context.Background(), "@"+path); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "input with spaces.json")
	os.WriteFile(path, []byte(`{"id":9007199254740993,"content":"@literal","fields":[null,{}]}`), 0600)
	got, err := ReadInput(context.Background(), "@"+path)
	if err != nil || got["id"] != json.Number("9007199254740993") || got["content"] != "@literal" {
		t.Fatalf("input = %#v, %v", got, err)
	}
	for _, reference := range []string{path, "@", "@-", "@https://example.test/file", "@" + t.TempDir(), "@" + path + "-missing"} {
		if _, err := ReadInput(context.Background(), reference); err == nil {
			t.Fatalf("accepted %q", reference)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadInput(ctx, "@"+path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReadInputDiagnostics(t *testing.T) {
	for _, tc := range []struct{ body, message string }{
		{`{"secret":"do-not-echo", "x": }`, "invalid character"},
		{"\xef\xbb\xbf{}", "UTF-8 without BOM"},
		{"null", "exactly one JSON object"},
		{"{} {}", "multiple top-level values"},
		{"[]", "cannot unmarshal array"},
	} {
		path := filepath.Join(t.TempDir(), "input.json")
		os.WriteFile(path, []byte(tc.body), 0600)
		_, err := ReadInput(context.Background(), "@"+path)
		if err == nil || !strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "do-not-echo") {
			t.Fatalf("error=%v", err)
		}
		if strings.Contains(tc.message, "invalid character") {
			var syntax *json.SyntaxError
			if !errors.As(err, &syntax) || !strings.Contains(err.Error(), fmt.Sprintf("byte %d", syntax.Offset)) {
				t.Fatalf("missing syntax offset/cause: %v", err)
			}
		}
	}
}

func TestReadInputRejectsFIFO(t *testing.T) {
	if path := os.Getenv("MEEGLE_TEST_INPUT_FIFO"); path != "" {
		_, err := ReadInput(context.Background(), "@"+path)
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("FIFO error=%v", err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX named pipe test")
	}
	path := filepath.Join(t.TempDir(), "input.fifo")
	if out, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadInputRejectsFIFO$")
	cmd.Env = append(os.Environ(), "MEEGLE_TEST_INPUT_FIFO="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO must fail before opening: %v %s", err, out)
	}
}

func TestReadInputSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	body := `{"content":"` + strings.Repeat("x", MaxInputBytes-len(`{"content":""}`)) + `"}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInput(context.Background(), "@"+path); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte(" "))
	f.Close()
	if _, err := ReadInput(context.Background(), "@"+path); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestOutputSelectionAndSourceOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	outputs, err := ParseOutputs([]string{fmt.Sprintf(`{"path":%q,"fields":["data.items","data.empty","data.items"]}`, path)})
	if err != nil {
		t.Fatal(err)
	}
	source := map[string]any{"code": json.Number("0"), "data": map[string]any{"items": []any{json.Number("9007199254740993")}, "empty": nil, "total": json.Number("1")}}
	before, _ := json.Marshal(source)
	remaining, saved, err := Export(context.Background(), source, outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || !filepath.IsAbs(saved[0].Path) || len(saved[0].Fields) != 2 {
		t.Fatalf("saved = %#v", saved)
	}
	file, _ := os.ReadFile(path)
	if string(file) != "{\"data\":{\"empty\":null,\"items\":[9007199254740993]}}\n" {
		t.Fatalf("file = %s", file)
	}
	result, _ := json.Marshal(remaining)
	if string(result) != `{"code":0,"data":{"total":1}}` {
		t.Fatalf("remaining = %s", result)
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("source mutated")
	}
	assertNoTemps(t, filepath.Dir(path))
}

func TestOutputConfigValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.json")
	for _, raw := range [][]string{
		{fmt.Sprintf(`{"PATH":%q,"fields":["a"]}`, p)},
		{fmt.Sprintf(`{"path":%q,"path":%q,"fields":["a"]}`, p, p+"2")},
		{`[]`}, {`null`}, {`{}`}, {`{`}, {fmt.Sprintf(`{"path":%q,"fields":[]}`, p)},
		{fmt.Sprintf(`{"path":%q,"fields":[null]}`, p)},
		{fmt.Sprintf(`{"path":%q,"fields":[""],"extra":true}`, p)},
		{fmt.Sprintf(`{"path":%q,"fields":["a","a.b"]}`, p)},
		{fmt.Sprintf(`{"path":%q,"fields":["a.*"]}`, p)},
		{fmt.Sprintf(`{"path":%q,"fields":["a"]} {}`, p)},
		{fmt.Sprintf(`[{"path":%q,"fields":["a"]},{"path":%q,"fields":["b"]}]`, p, p)},
		{fmt.Sprintf(`[{"path":%q,"fields":["a"]},{"path":%q,"fields":["a.b"]}]`, p, p+"2")},
	} {
		if _, err := ParseOutputs(raw); err == nil {
			t.Fatalf("accepted %#v", raw)
		}
	}
	os.WriteFile(p, []byte("existing"), 0600)
	if _, err := ParseOutputs([]string{fmt.Sprintf(`{"path":%q,"fields":["a"]}`, p)}); err == nil {
		t.Fatal("existing target accepted")
	}
}

func TestOutputConfigRejectsMixedForms(t *testing.T) {
	dir := t.TempDir()
	object := fmt.Sprintf(`{"path":%q,"fields":["a"]}`, filepath.Join(dir, "a.json"))
	array := fmt.Sprintf(`[{"path":%q,"fields":["b"]}]`, filepath.Join(dir, "b.json"))
	for _, raw := range [][]string{{object, array}, {array, object}} {
		_, err := ParseOutputs(raw)
		if err == nil || !strings.Contains(err.Error(), "not both") {
			t.Fatalf("mixed forms accepted: %v", err)
		}
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("invalid config created files")
	}
}

func TestExportPreflightsAllSelections(t *testing.T) {
	for _, source := range []any{nil, map[string]any(nil), []any{}, map[string]any{"a": 1}, map[string]any{"a": []any{1}}} {
		dir := t.TempDir()
		_, saved, err := Export(context.Background(), source, []Output{{Path: filepath.Join(dir, "a.json"), Fields: []string{"a"}}, {Path: filepath.Join(dir, "b.json"), Fields: []string{"missing"}}})
		if err == nil || len(saved) != 0 {
			t.Fatalf("source=%#v saved=%v err=%v", source, saved, err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("files written before complete field validation")
		}
	}
}

func TestExportPartialFailureAndConcurrentNoReplace(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	os.WriteFile(b, []byte("existing"), 0600)
	_, saved, err := Export(context.Background(), map[string]any{"a": 1, "b": 2}, []Output{{Path: a, Fields: []string{"a"}}, {Path: b, Fields: []string{"b"}}})
	var failure *OutputError
	if !errors.As(err, &failure) || len(saved) != 1 || len(failure.Files) != 1 || failure.Stage != "publish" {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	unchanged, _ := os.ReadFile(b)
	if string(unchanged) != "existing" {
		t.Fatal("existing file replaced")
	}
	path := filepath.Join(dir, "race.json")
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := Export(context.Background(), map[string]any{"a": 1}, []Output{{Path: path, Fields: []string{"a"}}})
			if err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners=%d", winners.Load())
	}
	assertNoTemps(t, dir)
}

type faultyFile struct {
	*os.File
	failure string
	cancel  context.CancelFunc
}

func (f faultyFile) Write(p []byte) (int, error) {
	if f.failure == "write" {
		return 0, errors.New("disk full")
	}
	if f.cancel != nil && f.failure != "cancel-after-close" {
		f.cancel()
	}
	return f.File.Write(p)
}
func (f faultyFile) Close() error {
	err := f.File.Close()
	if f.failure == "cancel-after-close" {
		f.cancel()
	}
	if f.failure == "close" {
		return errors.New("close failed")
	}
	return err
}

func TestAtomicFileCancelBeforePublish(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := diskOperations()
	ops.create = func(dir, pattern string) (temporaryFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		return faultyFile{File: f, failure: "cancel-after-close", cancel: cancel}, err
	}
	linked := false
	ops.link = func(from, to string) error { linked = true; return os.Link(from, to) }
	committed, stage, err := saveJSON(ctx, filepath.Join(dir, "out.json"), map[string]any{"a": 1}, ops)
	if committed || linked || stage != "publish" || !errors.Is(err, context.Canceled) {
		t.Fatalf("committed=%v linked=%v stage=%s error=%v", committed, linked, stage, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("files after cancellation: %v", entries)
	}
}

func TestOutputDirectoryProbe(t *testing.T) {
	for _, failure := range []string{"", "create", "write", "close", "link", "cleanup"} {
		t.Run("failure="+failure, func(t *testing.T) {
			dir := t.TempDir()
			ops := diskOperations()
			ops.create = func(dir, pattern string) (temporaryFile, error) {
				if failure == "create" {
					return nil, os.ErrPermission
				}
				f, err := os.CreateTemp(dir, pattern)
				return faultyFile{File: f, failure: failure}, err
			}
			if failure == "link" {
				ops.link = func(string, string) error { return errors.New("hard links unsupported") }
			}
			if failure == "cleanup" {
				ops.remove = func(path string) error { _ = os.Remove(path); return errors.New("cleanup failed") }
			}
			if err := probeOutputDir(dir, ops); (err != nil) != (failure != "") {
				t.Fatalf("failure=%s error=%v", failure, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatalf("probe leaked files: %v", entries)
			}
		})
	}
	t.Run("never remove a competing file", func(t *testing.T) {
		dir := t.TempDir()
		ops := diskOperations()
		var occupied string
		ops.link = func(from, to string) error {
			occupied = to
			if err := os.WriteFile(to, []byte("other writer"), 0600); err != nil {
				return err
			}
			return os.Link(from, to)
		}
		if err := probeOutputDir(dir, ops); err == nil {
			t.Fatal("probe overwrote competing file")
		}
		if data, err := os.ReadFile(occupied); err != nil || string(data) != "other writer" {
			t.Fatalf("competing file changed: %s %v", data, err)
		}
	})
}

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("untrusted marshaler") }

func TestAtomicFileFailures(t *testing.T) {
	for _, stage := range []string{"create", "write", "close", "publish", "cancel", "panic"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ops := diskOperations()
			ops.create = func(dir, pattern string) (temporaryFile, error) {
				if stage == "create" {
					return nil, errors.New("create failed")
				}
				f, err := os.CreateTemp(dir, pattern)
				var onWrite context.CancelFunc
				if stage == "cancel" {
					onWrite = cancel
				}
				return faultyFile{File: f, failure: stage, cancel: onWrite}, err
			}
			if stage == "publish" {
				ops.link = func(string, string) error { return errors.New("publish failed") }
			}
			var value any = map[string]any{"content": strings.Repeat("x", 128<<10)}
			if stage == "panic" {
				value = panicJSON{}
			}
			committed, _, err := saveJSON(ctx, filepath.Join(dir, "result.json"), value, ops)
			if committed || err == nil {
				t.Fatalf("committed=%v err=%v", committed, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatalf("leaked files: %v", entries)
			}
		})
	}
}

func TestExportEmptyValuesAndEmptyParents(t *testing.T) {
	dir := t.TempDir()
	source := map[string]any{"data": map[string]any{"s": "", "a": []any{}, "m": map[string]any{}, "null": nil}}
	rest, _, err := Export(context.Background(), source, []Output{{Path: filepath.Join(dir, "result.json"), Fields: []string{"data.s", "data.a", "data.m", "data.null"}}})
	if err != nil || !reflect.DeepEqual(rest, map[string]any{"data": map[string]any{}}) {
		t.Fatalf("rest=%#v err=%v", rest, err)
	}
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, ".meegle-output-*"))
	if len(files) != 0 {
		t.Fatalf("temporary files leaked: %v", files)
	}
}
