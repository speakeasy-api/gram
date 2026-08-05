#!/usr/bin/env python3

import hashlib
import json
import os
import re
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
OUTPUT = ROOT / "server/internal/litellm/fixtures/litellm-v1.94.0"
IMAGE = "ghcr.io/berriai/litellm:v1.94.0@sha256:65d84a2282137b4dc73bbe184650a7c807177c533e4223b3bfbc87963fe3fabe"
DIGEST = IMAGE.rsplit("@", 1)[1]
MASTER_KEY = "fixture-master-key"


def json_bytes(value):
    return (json.dumps(value, separators=(",", ":")) + "\n").encode()


class FixtureServer(ThreadingHTTPServer):
    def __init__(self, address):
        super().__init__(address, FixtureHandler)
        self.callbacks = []
        self.lock = threading.Lock()


class FixtureHandler(BaseHTTPRequestHandler):
    server: FixtureServer

    def log_message(self, _format, *_args):
        return

    def read_json(self):
        length = int(self.headers.get("content-length", "0"))
        return json.loads(self.rfile.read(length) or b"{}")

    def send_json(self, value, status=200):
        body = json_bytes(value)
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        body = self.read_json()
        if self.path.endswith("/beta/litellm_basic_guardrail_api"):
            with self.server.lock:
                self.server.callbacks.append(body)
            self.send_json({"action": "NONE"})
            return

        if self.path.endswith("/chat/completions"):
            if body.get("stream"):
                chunks = [
                    {
                        "id": "chatcmpl-fixture-stream",
                        "object": "chat.completion.chunk",
                        "created": 1700000000,
                        "model": "fixture-openai",
                        "choices": [
                            {
                                "index": 0,
                                "delta": {"role": "assistant", "content": "streamed "},
                                "finish_reason": None,
                            }
                        ],
                    },
                    {
                        "id": "chatcmpl-fixture-stream",
                        "object": "chat.completion.chunk",
                        "created": 1700000000,
                        "model": "fixture-openai",
                        "choices": [
                            {
                                "index": 0,
                                "delta": {"content": "answer"},
                                "finish_reason": "stop",
                            }
                        ],
                    },
                ]
                self.send_response(200)
                self.send_header("content-type", "text/event-stream")
                self.end_headers()
                for chunk in chunks:
                    self.wfile.write(b"data: " + json.dumps(chunk).encode() + b"\n\n")
                self.wfile.write(b"data: [DONE]\n\n")
                return
            self.send_json(
                {
                    "id": "chatcmpl-fixture",
                    "object": "chat.completion",
                    "created": 1700000000,
                    "model": "fixture-openai",
                    "choices": [
                        {
                            "index": 0,
                            "message": {
                                "role": "assistant",
                                "content": "chat answer",
                                "tool_calls": [
                                    {
                                        "id": "call_fixture_output",
                                        "type": "function",
                                        "function": {
                                            "name": "lookup_fixture",
                                            "arguments": '{"query":"fixture"}',
                                        },
                                    }
                                ],
                            },
                            "finish_reason": "tool_calls",
                        }
                    ],
                    "usage": {
                        "prompt_tokens": 12,
                        "completion_tokens": 8,
                        "total_tokens": 20,
                    },
                }
            )
            return

        if self.path.endswith("/responses"):
            self.send_json(
                {
                    "id": "resp_fixture",
                    "object": "response",
                    "created_at": 1700000000,
                    "status": "completed",
                    "error": None,
                    "incomplete_details": None,
                    "instructions": None,
                    "max_output_tokens": None,
                    "model": "fixture-responses",
                    "output": [
                        {
                            "id": "msg_fixture",
                            "type": "message",
                            "status": "completed",
                            "role": "assistant",
                            "content": [
                                {
                                    "type": "output_text",
                                    "annotations": [],
                                    "text": "responses answer",
                                }
                            ],
                        },
                        {
                            "id": "fc_fixture",
                            "type": "function_call",
                            "status": "completed",
                            "call_id": "call_fixture_response",
                            "name": "lookup_fixture",
                            "arguments": '{"query":"fixture"}',
                        },
                    ],
                    "parallel_tool_calls": True,
                    "previous_response_id": None,
                    "reasoning": {"effort": None, "summary": None},
                    "store": True,
                    "temperature": 1.0,
                    "text": {"format": {"type": "text"}},
                    "tool_choice": "auto",
                    "tools": [],
                    "top_p": 1.0,
                    "truncation": "disabled",
                    "usage": {
                        "input_tokens": 12,
                        "output_tokens": 8,
                        "total_tokens": 20,
                    },
                    "user": None,
                    "metadata": {},
                }
            )
            return

        if self.path.endswith("/messages"):
            self.send_json(
                {
                    "id": "msg_fixture",
                    "type": "message",
                    "role": "assistant",
                    "model": "fixture-anthropic",
                    "content": [
                        {"type": "text", "text": "anthropic answer"},
                        {
                            "type": "tool_use",
                            "id": "toolu_fixture_output",
                            "name": "lookup_fixture",
                            "input": {"query": "fixture"},
                        },
                    ],
                    "stop_reason": "tool_use",
                    "stop_sequence": None,
                    "usage": {"input_tokens": 12, "output_tokens": 8},
                }
            )
            return

        if self.path.endswith("/text"):
            self.send_json({"output": "pass-through answer", "metadata": None})
            return

        self.send_json({"error": "unexpected fake provider path"}, 404)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run(*args, env=None, capture=False):
    return subprocess.run(
        args,
        cwd=ROOT,
        env=env,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
    )


def request(base_url, path, body=None, key=MASTER_KEY, headers=None):
    data = None if body is None else json_bytes(body)
    req = urllib.request.Request(
        base_url + path, data=data, method="GET" if body is None else "POST"
    )
    if body is not None:
        req.add_header("content-type", "application/json")
    if key:
        req.add_header("authorization", "Bearer " + key)
    for name, value in (headers or {}).items():
        req.add_header(name, value)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.read(), response.status
    except urllib.error.HTTPError as error:
        detail = error.read().decode(errors="replace")
        raise RuntimeError(f"{path}: HTTP {error.code}: {detail}") from error


def wait_ready(base_url):
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        try:
            _, status = request(base_url, "/health/liveliness", key=None)
            if status == 200:
                return
        except Exception:  # noqa: BLE001 - any failure means not ready yet
            time.sleep(1)
    raise RuntimeError("LiteLLM did not become ready")


def provision_key(base_url, user_id=None, email=None, alias=None):
    if email is not None:
        request(
            base_url,
            "/user/new",
            {
                "user_id": user_id,
                "user_email": email,
                "user_alias": alias,
                "auto_create_key": False,
            },
        )
    key_request = {
        "key_alias": alias,
        "models": ["fixture-openai", "fixture-responses", "fixture-anthropic"],
    }
    if user_id is not None:
        key_request["user_id"] = user_id
    body, _ = request(
        base_url,
        "/key/generate",
        key_request,
    )
    result = json.loads(body)
    key = result.get("key")
    if not isinstance(key, str) or not key:
        raise RuntimeError(f"key provisioning returned no key: {result}")
    return key


def normalize(payload, case):
    payload = json.loads(json.dumps(payload))
    payload["litellm_call_id"] = f"fixture-{case}-call"
    payload["litellm_trace_id"] = f"fixture-{case}-trace"
    request_data = payload.get("request_data") or {}
    if request_data.get("user_api_key_hash") is not None:
        request_data["user_api_key_hash"] = "fixture-key-hash"
    headers = payload.get("request_headers")
    if isinstance(headers, dict):
        payload["request_headers"] = {
            key.lower(): value
            for key, value in headers.items()
            if key.lower() in {"content-type", "x-gram-session-id"}
        }
    return payload


def assert_safe(raw, filename):
    text = raw.decode()
    for email in re.findall(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}", text):
        if not email.lower().endswith("@example.test"):
            raise RuntimeError(f"unsafe email in {filename}: {email}")
    forbidden = [
        r"(?i)authorization",
        r"(?i)cookie",
        r"(?i)bearer\s",
        r"\bsk-[A-Za-z0-9]",
        r"OPENROUTER",
    ]
    for pattern in forbidden:
        if re.search(pattern, text):
            raise RuntimeError(f"unsafe value matching {pattern!r} in {filename}")
    parsed = [json.loads(line) for line in text.splitlines() if line.strip()]
    for payload in parsed:
        data = payload.get("request_data") or {}
        for key in ("user_api_key_org_id",):
            value = data.get(key)
            if value is not None and not str(value).startswith("fixture"):
                raise RuntimeError(f"unsafe {key} in {filename}: {value}")


def record_case(server, base_url, name, path, body, key, headers=None, expected=2):
    with server.lock:
        start = len(server.callbacks)
    request(base_url, path, body, key=key, headers=headers)
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        with server.lock:
            captured = server.callbacks[start:]
        if len(captured) >= expected:
            break
        time.sleep(0.05)
    if len(captured) != expected:
        raise RuntimeError(
            f"{name}: expected {expected} callbacks, got {len(captured)}"
        )
    normalized = [normalize(item, name) for item in captured]
    return b"".join(json_bytes(item) for item in normalized)


def main():
    run("docker", "pull", IMAGE)
    image = run(
        "docker",
        "image",
        "inspect",
        IMAGE,
        "--format",
        "{{json .RepoDigests}}",
        capture=True,
    ).stdout
    if DIGEST not in image:
        raise RuntimeError(f"pinned image digest not present: {DIGEST}")

    provider_port = free_port()
    proxy_port = free_port()
    server = FixtureServer(("0.0.0.0", provider_port))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    project = f"gram-litellm-fixtures-{os.getpid()}"
    env = os.environ.copy()
    env.update(
        {
            "COMPOSE_PROJECT_NAME": project,
            "LITELLM_PORT": str(proxy_port),
            "FIXTURE_PROVIDER_BASE": f"http://host.docker.internal:{provider_port}/v1",
            "FIXTURE_TEXT_TARGET": f"http://host.docker.internal:{provider_port}/text",
            "FIXTURE_GUARDRAIL_BASE": f"http://host.docker.internal:{provider_port}",
        }
    )
    compose = (
        "docker",
        "compose",
        "-p",
        project,
        "-f",
        "compose.yml",
        "-f",
        "local/litellm/contract-fixtures/compose.yml",
        "--profile",
        "litellm",
    )
    try:
        run(*compose, "up", "-d", "--wait", "litellm", env=env)
        base_url = f"http://127.0.0.1:{proxy_port}"
        wait_ready(base_url)

        container_id = run(
            *compose, "ps", "-q", "litellm", env=env, capture=True
        ).stdout.strip()
        actual_image = run(
            "docker", "inspect", container_id, "--format", "{{.Image}}", capture=True
        ).stdout.strip()
        expected_image = run(
            "docker", "image", "inspect", IMAGE, "--format", "{{.Id}}", capture=True
        ).stdout.strip()
        if actual_image != expected_image:
            raise RuntimeError(
                f"running LiteLLM image mismatch: {actual_image} != {expected_image}"
            )

        email_key = provision_key(
            base_url,
            "fixture-email-user",
            "fixture-user@example.test",
            "fixture-email-key",
        )
        email_less_key = provision_key(
            base_url, "fixture-email-less-user", alias="fixture-email-less-key"
        )
        shared_key = provision_key(base_url, alias="fixture-shared-key")

        tool = {
            "type": "function",
            "function": {
                "name": "lookup_fixture",
                "description": "Look up synthetic fixture data.",
                "parameters": {
                    "type": "object",
                    "properties": {"query": {"type": "string"}},
                    "required": ["query"],
                },
            },
        }
        cases = {}
        cases["openai-chat-tools.jsonl"] = record_case(
            server,
            base_url,
            "openai-chat-tools",
            "/v1/chat/completions",
            {
                "model": "fixture-openai",
                "messages": [
                    {"role": "user", "content": "older chat prompt"},
                    {
                        "role": "assistant",
                        "content": None,
                        "tool_calls": [
                            {
                                "id": "call_fixture_history",
                                "type": "function",
                                "function": {
                                    "name": "lookup_fixture",
                                    "arguments": '{"query":"old"}',
                                },
                            }
                        ],
                    },
                    {
                        "role": "tool",
                        "tool_call_id": "call_fixture_history",
                        "content": "historical result",
                    },
                    {
                        "role": "user",
                        "content": [
                            {"type": "text", "text": "latest chat block one"},
                            {"type": "text", "text": "latest chat block two"},
                        ],
                    },
                ],
                "tools": [tool],
            },
            email_key,
            {"x-gram-session-id": "fixture-chat-session"},
        )
        cases["openai-responses-tools.jsonl"] = record_case(
            server,
            base_url,
            "openai-responses-tools",
            "/v1/responses",
            {
                "model": "fixture-responses",
                "input": [
                    {"role": "user", "content": "older responses prompt"},
                    {
                        "type": "function_call",
                        "id": "fc_fixture_history",
                        "call_id": "call_fixture_history",
                        "name": "lookup_fixture",
                        "arguments": '{"query":"old"}',
                    },
                    {
                        "type": "function_call_output",
                        "call_id": "call_fixture_history",
                        "output": "historical result",
                    },
                    {
                        "role": "user",
                        "content": [
                            {"type": "input_text", "text": "latest responses prompt"}
                        ],
                    },
                ],
                "tools": [tool["function"] | {"type": "function"}],
            },
            email_less_key,
            {"x-gram-session-id": "fixture-responses-session"},
        )
        cases["anthropic-messages-tools.jsonl"] = record_case(
            server,
            base_url,
            "anthropic-messages-tools",
            "/v1/messages",
            {
                "model": "fixture-anthropic",
                "max_tokens": 64,
                "messages": [
                    {"role": "user", "content": "older anthropic prompt"},
                    {
                        "role": "assistant",
                        "content": [
                            {
                                "type": "tool_use",
                                "id": "toolu_fixture_history",
                                "name": "lookup_fixture",
                                "input": {"query": "old"},
                            }
                        ],
                    },
                    {
                        "role": "user",
                        "content": [
                            {
                                "type": "tool_result",
                                "tool_use_id": "toolu_fixture_history",
                                "content": "historical result",
                            },
                            {"type": "text", "text": "latest anthropic prompt"},
                        ],
                    },
                ],
                "tools": [
                    {
                        "name": "lookup_fixture",
                        "description": "Look up synthetic fixture data.",
                        "input_schema": tool["function"]["parameters"],
                    }
                ],
            },
            MASTER_KEY,
            {"x-gram-session-id": "fixture-anthropic-session"},
        )
        cases["passthrough-text.jsonl"] = record_case(
            server,
            base_url,
            "passthrough-text",
            "/fixture/text",
            {"input": "pass-through prompt", "ignored": "not selected"},
            email_less_key,
            {"x-gram-session-id": "fixture-passthrough-session"},
            # v1.94.0 strips litellm_logging_obj before configured pass-through
            # post-call dispatch, so the real pinned image emits only pre-call.
            expected=1,
        )
        cases["streaming-chat.jsonl"] = record_case(
            server,
            base_url,
            "streaming-chat",
            "/v1/chat/completions",
            {
                "model": "fixture-openai",
                "messages": [{"role": "user", "content": "streaming prompt"}],
                "stream": True,
            },
            email_key,
            {"x-gram-session-id": "fixture-stream-session"},
        )
        cases["end-user-identity.jsonl"] = record_case(
            server,
            base_url,
            "end-user-identity",
            "/v1/chat/completions",
            {
                "model": "fixture-openai",
                "messages": [{"role": "user", "content": "end-user prompt"}],
                "user": "fixture-end-user-id",
            },
            email_less_key,
        )
        cases["shared-key-identity.jsonl"] = record_case(
            server,
            base_url,
            "shared-key-identity",
            "/v1/chat/completions",
            {
                "model": "fixture-openai",
                "messages": [{"role": "user", "content": "shared-key prompt"}],
            },
            shared_key,
        )

        OUTPUT.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=OUTPUT.parent) as temp_dir:
            temp = Path(temp_dir)
            manifest = {}
            for filename, raw in sorted(cases.items()):
                assert_safe(raw, filename)
                (temp / filename).write_bytes(raw)
                manifest[filename] = hashlib.sha256(raw).hexdigest()
            manifest_raw = json_bytes({"image": IMAGE, "files": manifest})
            assert_safe(manifest_raw, "manifest.json")
            (temp / "manifest.json").write_bytes(manifest_raw)
            OUTPUT.mkdir(parents=True, exist_ok=True)
            for filename in sorted(cases):
                os.replace(temp / filename, OUTPUT / filename)
            for stale in OUTPUT.glob("*.jsonl"):
                if stale.name not in cases:
                    stale.unlink()
            # The manifest is the commit marker for the generated set. Tests
            # reject interrupted or manually edited output by verifying every
            # listed hash and rejecting unlisted JSONL files.
            os.replace(temp / "manifest.json", OUTPUT / "manifest.json")
        print(f"recorded {len(cases)} fixture sequences from {IMAGE}")
    finally:
        try:
            run(*compose, "down", "--volumes", "--remove-orphans", env=env)
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
