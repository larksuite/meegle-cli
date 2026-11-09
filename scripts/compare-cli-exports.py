#!/usr/bin/env python3
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT

"""Compare two exported CLI trees against a local HTTPS fixture (Python 3.9+)."""

import argparse
from email.parser import BytesParser
from email.policy import default as email_policy
import hashlib
import http.server
import json
import os
import pathlib
import shutil
import ssl
import subprocess
import sys
import threading
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("baseline", type=pathlib.Path, help="baseline exported Go module")
parser.add_argument("candidate", type=pathlib.Path, help="candidate exported Go module")
parser.add_argument(
    "output", type=pathlib.Path, help="new directory for binaries and comparison.json"
)
parser.add_argument(
    "--baseline-ref", default="07e8c81c9e7a9cd50893554e342e901c81664be6"
)
parser.add_argument(
    "--candidate-ref", required=True, help="commit or snapshot label being tested"
)
options = parser.parse_args()
BASE = options.baseline.resolve()
CAND = options.candidate.resolve()
for tree in (BASE, CAND):
    if not (tree / "go.mod").is_file():
        parser.error(f"not an exported Go module: {tree}")
ROOT = options.output.resolve()
ROOT.mkdir(parents=True, exist_ok=False)
CERT = ROOT / "cert.pem"
KEY = ROOT / "key.pem"
if not CERT.exists():
    subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            str(KEY),
            "-out",
            str(CERT),
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=IP:127.0.0.1",
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
source = """package main
import("crypto/tls";"crypto/x509";"net/http";"os";cli "github.com/larksuite/meegle-cli/cmd")
func main(){data,e:=os.ReadFile(os.Getenv("COMPARE_CA"));if e!=nil{panic(e)};roots:=x509.NewCertPool();if !roots.AppendCertsFromPEM(data){panic("invalid CA")};transport:=http.DefaultTransport.(*http.Transport).Clone();transport.TLSClientConfig=&tls.Config{RootCAs:roots,MinVersion:tls.VersionTLS12};http.DefaultClient=&http.Client{Transport:transport};os.Exit(cli.ExecuteWithVersion("compare"))}
"""
entry = ROOT / "compare_main.go"
entry.write_text(source)
for label, tree in [("baseline", BASE), ("candidate", CAND)]:
    subprocess.run(
        ["go", "build", "-o", str(ROOT / (label + "-cli")), str(entry)],
        cwd=tree,
        check=True,
    )
    subprocess.run(
        ["go", "build", "-o", str(ROOT / (label + "-official")), "./cmd/meegle"],
        cwd=tree,
        check=True,
    )


def tool(name, props=None, required=None, metadata=None):
    x = {
        "name": name,
        "description": "Sample tool",
        "inputSchema": {"type": "object", "properties": props or {}},
    }
    if required:
        x["inputSchema"]["required"] = required
    if metadata:
        x["metadata"] = metadata
    return x


TOOLS = [
    tool(
        "get_workitem_brief",
        {"work_item_id": {"type": "integer"}, "project_key": {"type": "string"}},
        ["work_item_id"],
    ),
    tool(
        "search_user_info",
        {"user_keys": {"type": "array", "items": {"type": "string"}}},
    ),
    tool("sample__list", {"page_token": {"type": "string"}}),
    tool("sample__fail"),
    tool("upload_file"),
    tool("get_download_url"),
    tool("sample__required", {"query": {"type": "string"}}, ["query"]),
]
cache_tools = []
for x in TOOLS:
    cache_tools.append(
        {
            "name": x["name"],
            "description": x["description"],
            "parameters": [
                dict(
                    name=k,
                    type=v["type"],
                    required=k in x["inputSchema"].get("required", []),
                    **({"items": v["items"]} if "items" in v else {}),
                )
                for k, v in x["inputSchema"]["properties"].items()
            ],
        }
    )
state = {"mode": "normal", "requests": []}
lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def respond(self, obj, status=200, headers=None):
        data = (
            json.dumps(obj, separators=(",", ":")).encode()
            if not isinstance(obj, bytes)
            else obj
        )
        self.send_response(status)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def record(self, body):
        headers = {
            k.lower(): v
            for k, v in self.headers.items()
            if k.lower() not in ("content-length",)
        }
        # Multipart boundaries are random; compare file content and non-boundary headers.
        if headers.get("content-type", "").startswith("multipart/"):
            message = BytesParser(policy=email_policy).parsebytes(
                (
                    "Content-Type: "
                    + headers["content-type"]
                    + "\r\nMIME-Version: 1.0\r\n\r\n"
                ).encode()
                + body
            )
            parts = []
            for part in message.iter_parts():
                parts.append(
                    {
                        "name": part.get_param("name", header="content-disposition"),
                        "filename": part.get_filename(),
                        "headers": dict(part.items()),
                        "sha256": hashlib.sha256(
                            part.get_payload(decode=True)
                        ).hexdigest(),
                    }
                )
            headers["content-type"] = "multipart/form-data"
            body = {"parts": parts}
        if isinstance(body, bytes):
            body = {"sha256": hashlib.sha256(body).hexdigest()}
        with lock:
            state["requests"].append(
                {
                    "method": self.command,
                    "path": self.path,
                    "headers": headers,
                    "body": body,
                }
            )

    def do_GET(self):
        self.record(None)
        self.respond(
            b"sample file bytes",
            headers={
                "X-Meego-File-Sign": "sig",
                "Content-Disposition": 'attachment; filename="sample.txt"',
            },
        )

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.path == "/file":
            self.record(raw)
            self.respond(
                {
                    "file_token": "sample-token",
                    "file_url": "file://sample",
                    "un_upload_index": [],
                }
            )
            return
        data = json.loads(raw)
        data.pop("id", None)
        self.record(data)
        if data["method"] == "tools/list":
            mode = state["mode"]
            if mode == "error":
                self.respond({}, 503)
                return
            if mode == "unauthorized":
                self.respond({}, 401)
                return
            if mode == "empty":
                tools = []
            elif mode == "malformed":
                tools = TOOLS + [
                    tool("bad", {"value": {"type": ["string", "integer"]}})
                ]
            else:
                tools = TOOLS
            self.respond({"jsonrpc": "2.0", "id": 1, "result": {"tools": tools}})
            return
        name = data.get("params", {}).get("name", "")
        args = data.get("params", {}).get("arguments", {})
        if name == "sample__fail":
            self.respond(
                {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "error": {"code": -32603, "message": "sample__fail rejected"},
                }
            )
            return
        if name == "sample__list":
            result = {
                "items": [2 if args.get("page_token") else 1],
                "has_more": not bool(args.get("page_token")),
                "next_page_token": "" if args.get("page_token") else "next",
            }
        elif name == "search_user_info":
            result = [
                {"user_key": "sample-user", "name_cn": "Sample", "extra": "value"}
            ]
        elif name == "upload_file":
            result = {
                "sign": "sig",
                "is_multipart": False,
                "upload_url": address + "/file",
            }
        elif name == "get_download_url":
            result = {
                "sign": "sig",
                "is_multipart": False,
                "download_url": address + "/file",
            }
        else:
            result = args
        self.respond(
            {
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [
                        {
                            "type": "text",
                            "text": json.dumps(result, separators=(",", ":")),
                        }
                    ]
                },
            }
        )


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain(CERT, KEY)
server.socket = context.wrap_socket(server.socket, server_side=True)
address = "https://127.0.0.1:" + str(server.server_port)
threading.Thread(target=server.serve_forever, daemon=True).start()
home = ROOT / "comparison-home"
output = ROOT / "comparison-download.txt"
upload = ROOT / "comparison-upload.txt"
upload.write_text("sample file bytes")
CASES = [
    ("root-help", ["--help"], {}),
    ("auth-help", ["auth", "login", "--help"], {}),
    ("config-list", ["config", "list"], {}),
    ("inspect", ["inspect"], {"cache": "fresh"}),
    ("dynamic-help", ["workitem", "--help"], {}),
    (
        "ordinary",
        ["workitem", "get", "--work-item-id", "9007199254740993", "--format", "json"],
        {},
    ),
    ("missing", ["sample", "required", "--format", "json"], {}),
    ("business-error", ["sample", "fail", "--format", "json"], {}),
    ("user-me", ["user", "me", "--format", "json"], {}),
    (
        "batch",
        ["workitem", "+batch-get", "--work-item-ids", "1,2", "--format", "json"],
        {},
    ),
    ("batch-missing", ["workitem", "+batch-get", "--format", "json"], {}),
    ("paginate", ["sample", "list", "--auto-paginate", "--format", "json"], {}),
    (
        "upload",
        [
            "attachment",
            "+upload",
            str(upload),
            "--project-key",
            "sample",
            "--work-item-id",
            "1",
            "--resource-type",
            "13",
            "--format",
            "json",
        ],
        {},
    ),
    (
        "download",
        [
            "attachment",
            "+download",
            "file://sample",
            "--project-key",
            "sample",
            "--work-item-id",
            "1",
            "--output",
            str(output),
            "--format",
            "json",
        ],
        {},
    ),
    (
        "attachment-missing",
        [
            "attachment",
            "+upload",
            str(upload),
            "--project-key",
            "sample",
            "--resource-type",
            "13",
            "--format",
            "json",
        ],
        {},
    ),
    (
        "cache-fresh",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"cache": "fresh", "mode": "error"},
    ),
    (
        "cache-refresh",
        ["workitem", "get", "--work-item-id", "1", "--refresh", "--format", "json"],
        {"cache": "fresh"},
    ),
    (
        "cache-stale-fallback",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"cache": "stale", "mode": "error"},
    ),
    (
        "empty-directory",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"mode": "empty"},
    ),
    (
        "bad-entry",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"mode": "malformed"},
    ),
    (
        "directory-error",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"mode": "error"},
    ),
    (
        "rejected-identity",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"mode": "unauthorized"},
    ),
    (
        "config-identity",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"env_token": False},
    ),
    (
        "unrelated-environment",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"unrelated": True},
    ),
    (
        "profile-selection",
        [
            "workitem",
            "get",
            "--work-item-id",
            "1",
            "--profile",
            "second",
            "--format",
            "json",
        ],
        {"env_token": False},
    ),
    (
        "broken-config",
        ["workitem", "get", "--work-item-id", "1", "--format", "json"],
        {"broken": True, "env_token": False},
    ),
]


def run(label, args, opts, official=False):
    shutil.rmtree(home, ignore_errors=True)
    (home / ".meegle/cache").mkdir(parents=True)
    output.unlink(missing_ok=True)
    pathlib.Path(str(output) + ".partial").unlink(missing_ok=True)
    cfg = {
        "current": "default",
        "profiles": {
            "default": {
                "host": address.removeprefix("https://"),
                "user_access_token": (
                    "${COMPARE_MISSING}" if opts.get("broken") else "config-token"
                ),
                "headers": {"X-Sample": "default"},
            },
            "second": {
                "host": address.removeprefix("https://"),
                "user_access_token": "second-token",
                "headers": {"X-Sample": "second"},
            },
        },
    }
    (home / ".meegle/config.json").write_text(json.dumps(cfg))
    if opts.get("cache"):
        (home / ".meegle/cache/tools.json").write_text(
            json.dumps(
                {
                    "timestamp": int(time.time() * 1000)
                    - (2 * 86400000 if opts["cache"] == "stale" else 0),
                    "tools": cache_tools,
                }
            )
        )
    env = {
        k: v
        for k, v in os.environ.items()
        if not k.startswith(("MEEGLE_", "COMPARE_", "UNRELATED_CLI_"))
    }
    env.update(
        HOME=str(home),
        MEEGLE_TELEMETRY="disabled",
        COMPARE_CA=str(CERT),
        MEEGLE_HOST="",
        MEEGLE_USER_ACCESS_TOKEN="env-token" if opts.get("env_token", True) else "",
    )
    if opts.get("unrelated"):
        env.update(
            UNRELATED_CLI_TOKEN="sample-token",
            UNRELATED_CLI_ENDPOINT="http://unused.invalid",
        )
    state.update(mode=opts.get("mode", "normal"), requests=[])
    completed = subprocess.run(
        [str(ROOT / (label + ("-official" if official else "-cli"))), *args],
        env=env,
        capture_output=True,
        timeout=20,
    )
    requests = sorted(state["requests"], key=lambda x: json.dumps(x, sort_keys=True))
    return {
        "exit": completed.returncode,
        "stdout": completed.stdout.decode(),
        "stderr": completed.stderr.decode(),
        "requests": requests,
        "file_sha256": (
            hashlib.sha256(output.read_bytes()).hexdigest() if output.exists() else None
        ),
        "partial": pathlib.Path(str(output) + ".partial").exists(),
    }


report = {
    "baseline": options.baseline_ref,
    "candidate": options.candidate_ref,
    "cases": [],
}
for name, args, opts in CASES:
    baseline = run("baseline", args, opts)
    candidate = run("candidate", args, opts)
    passed = baseline == candidate
    report["cases"].append(
        {"name": name, "pass": passed, "baseline": baseline, "candidate": candidate}
    )
    print(("PASS" if passed else "FAIL"), name, flush=True)
for args in [
    ["--help"],
    ["--version"],
    ["config", "list"],
    ["auth", "login", "--help"],
    ["extension", "policy"],
]:
    name = "official-" + "-".join(args)
    baseline = run("baseline", args, {}, True)
    candidate = run("candidate", args, {}, True)
    passed = baseline == candidate
    report["cases"].append(
        {"name": name, "pass": passed, "baseline": baseline, "candidate": candidate}
    )
    print(("PASS" if passed else "FAIL"), name, flush=True)
(ROOT / "comparison.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
server.shutdown()
if not all(c["pass"] for c in report["cases"]):
    sys.exit(1)
