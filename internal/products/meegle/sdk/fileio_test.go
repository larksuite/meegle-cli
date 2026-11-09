// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/larksuite/meegle-cli/pkg/framework/jsonvalue"
)

func TestFileIOCommandStringHTTP(t *testing.T) {
	// The SDK uses http.DefaultClient. Keep trust changes and any extension
	// registries isolated in a helper process, including repeated/race runs.
	if os.Getenv("MEEGLE_FILE_IO_SDK_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFileIOCommandStringHTTP$", "-test.v")
		cmd.Env = append(os.Environ(), "MEEGLE_FILE_IO_SDK_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("SDK file IO: %v\n%s", err, out)
		}
		return
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpc struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&rpc); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		var result any
		switch rpc.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "file_echo", "description": "Echo file input", "metadata": map[string]any{"resource": "transfer", "method": "echo"}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"content": map[string]any{"type": "string"}, "id": map[string]any{"type": "integer"}, "mode": map[string]any{"type": "string"}, "page_token": map[string]any{"type": "string"}}, "required": []string{"content"}}}}}
			result.(map[string]any)["tools"] = append(result.(map[string]any)["tools"].([]any), map[string]any{"name": "get_workitem_brief", "description": "Get brief", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"work_item_id": map[string]any{"type": "integer"}}, "required": []string{"work_item_id"}}})
		case "tools/call":
			calls.Add(1)
			args := rpc.Params.Arguments
			for _, name := range []string{"input_file", "output_file", "input-file", "output-file"} {
				if _, ok := args[name]; ok {
					t.Errorf("file option sent to server: %s", name)
				}
			}
			body := map[string]any{"data": map[string]any{"items": []any{map[string]any{"content": args["content"], "id": args["id"]}}, "description": "separate"}, "total": 1, "summary": map[string]any{"failed": 1}, "errors": []any{map[string]any{"message": "one item failed"}}}
			if args["mode"] == "paginate" {
				body = map[string]any{"items": []any{args["content"]}, "next_page_token": "second"}
				if args["page_token"] == "second" {
					body["next_page_token"] = ""
				}
			}
			if args["mode"] == "oversize" {
				body = map[string]any{"items": strings.Repeat("z", 32<<20)}
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Error(err)
				return
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}}
			if args["work_item_id"] == json.Number("2") {
				result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "error=user,message=missing item,retriable=false"}}}
			}
		default:
			http.Error(w, "unexpected method", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer server.Close()
	http.DefaultClient = server.Client()
	client, err := NewCommandClient(server.URL, WithToken("file-io-test"))
	if err != nil {
		t.Fatal(err)
	}
	checkDirectory := func(t *testing.T, dir string) {
		t.Helper()
		before := calls.Load()
		path := filepath.Join(dir, "sdk-preflight.json")
		spec := fmt.Sprintf(`{"path":%q,"fields":["data.items"]}`, path)
		_, err := client.Execute(context.Background(), "transfer echo --content test --output-file "+strconv.Quote(spec))
		if err == nil || !strings.Contains(err.Error(), "directory preflight failed") || calls.Load() != before {
			t.Fatalf("error=%v business calls=%d", err, calls.Load()-before)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("destination changed: %v", err)
		}
		files, _ := filepath.Glob(filepath.Join(dir, ".meegle-output-*"))
		if len(files) != 0 {
			t.Fatalf("probe files leaked: %v", files)
		}
	}
	t.Run("unwritable-directory", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("requires POSIX permissions and an unprivileged user")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(dir, 0755) }()
		checkDirectory(t, dir)
	})
	if dir := os.Getenv("MEEGLE_FILE_IO_TEST_NO_LINK_DIR"); dir != "" {
		t.Run("filesystem-without-hard-links", func(t *testing.T) { checkDirectory(t, dir) })
	}
	for _, scenario := range []struct {
		name string
		size int
		form string
	}{{"1MiB", 1 << 20, "single"}, {"10MiB", 10 << 20, "single"}, {"array", 64, "array"}, {"repeated", 64, "repeat"}} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			input, path, second := filepath.Join(dir, "input with spaces.json"), filepath.Join(dir, "output.json"), filepath.Join(dir, "second.json")
			content := strings.Repeat("x", scenario.size)
			request, _ := json.Marshal(map[string]any{"content": content, "id": json.Number("9007199254740993")})
			os.WriteFile(input, request, 0600)
			firstSpec := fmt.Sprintf(`{"path":%q,"fields":["data.items"]}`, path)
			secondSpec := fmt.Sprintf(`{"path":%q,"fields":["data.description"]}`, second)
			outputArgs := "--output-file " + strconv.Quote(firstSpec)
			if scenario.form == "array" {
				outputArgs = "--output-file " + strconv.Quote("["+firstSpec+","+secondSpec+"]")
			}
			if scenario.form == "repeat" {
				outputArgs += " --output-file " + strconv.Quote(secondSpec)
			}
			out, err := client.Execute(context.Background(), "meegle transfer echo --input-file "+strconv.Quote("@"+input)+" "+outputArgs)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) > 4096 || strings.Contains(string(out), content) {
				t.Fatalf("large data leaked to agent: %d bytes", len(out))
			}
			var envelope struct {
				Data map[string]any
				Meta struct {
					OutputFiles []any `json:"output_files"`
				}
			}
			if err := jsonvalue.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			wantFiles := 1
			if scenario.form != "single" {
				wantFiles = 2
			}
			if len(envelope.Meta.OutputFiles) != wantFiles || envelope.Data["summary"].(map[string]any)["failed"] != json.Number("1") {
				t.Fatalf("envelope=%s", out)
			}
			file, _ := os.ReadFile(path)
			var stored struct {
				Data struct {
					Items []struct {
						Content string
						ID      json.Number
					}
				}
			}
			if err := jsonvalue.Unmarshal(file, &stored); err != nil {
				t.Fatal(err)
			}
			if len(stored.Data.Items) != 1 || stored.Data.Items[0].Content != content || stored.Data.Items[0].ID != "9007199254740993" {
				t.Fatal("file content changed")
			}
		})
	}
	t.Run("pagination", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pages.json")
		spec := fmt.Sprintf(`{"path":%q,"fields":["items"]}`, path)
		out, err := client.Execute(context.Background(), "transfer echo --content page --mode paginate --auto-paginate --output-file "+strconv.Quote(spec))
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		file, _ := os.ReadFile(path)
		if string(file) != "{\"items\":[\"page\",\"page\"]}\n" {
			t.Fatalf("pages=%s", file)
		}
	})
	t.Run("oversize-response", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "too-big.json")
		spec := fmt.Sprintf(`{"path":%q,"fields":["items"]}`, path)
		_, err := client.Execute(context.Background(), "transfer echo --content x --mode oversize --output-file "+strconv.Quote(spec))
		if err == nil {
			t.Fatal("oversize response accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("oversize response created a file")
		}
	})
	before := calls.Load()
	_, err = client.Execute(context.Background(), "transfer echo --input-file @/missing/file.json")
	if err == nil || before != calls.Load() {
		t.Fatal("bad input executed business call")
	}
	t.Run("actual-batch-partial-failure", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "batch.json")
		spec := fmt.Sprintf(`{"path":%q,"fields":["results"]}`, path)
		out, err := client.Execute(context.Background(), "workitem +batch-get --work-item-ids 1,2 --output-file "+strconv.Quote(spec))
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		var envelope struct {
			Data struct {
				Summary struct{ Failed int }
				Errors  []any
			}
		}
		if err := json.Unmarshal(out, &envelope); err != nil || envelope.Data.Summary.Failed != 1 || len(envelope.Data.Errors) != 1 {
			t.Fatalf("batch envelope=%s err=%v", out, err)
		}
		file, _ := os.ReadFile(path)
		var stored struct{ Results []any }
		if err := json.Unmarshal(file, &stored); err != nil || len(stored.Results) != 1 {
			t.Fatalf("batch file=%s err=%v", file, err)
		}
	})
	t.Run("local-file-control-rejected", func(t *testing.T) {
		input := filepath.Join(t.TempDir(), "input.json")
		os.WriteFile(input, []byte(`{"ids_file":"/missing/ids.txt"}`), 0600)
		before := calls.Load()
		_, err := client.Execute(context.Background(), "workitem +batch-get --input-file "+strconv.Quote("@"+input))
		if err == nil || !strings.Contains(err.Error(), "control option") || calls.Load() != before {
			t.Fatalf("error=%v", err)
		}
	})
	// A subsequent call must neither reread a previous input nor export again.
	if out, err := client.Execute(context.Background(), "transfer echo --content plain"); err != nil || strings.Contains(string(out), "output_files") {
		t.Fatalf("reuse=%s %v", out, err)
	}
}
