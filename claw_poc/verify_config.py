#!/usr/bin/env python3
"""
DefenseClaw x Gram — Config Verification

Sanity checks that the Gram credentials are valid and the target project
has working MCP servers and chat completions.

Usage:
    uv run python verify_config.py
"""
import json
import sys
from urllib.request import Request, urlopen
from urllib.error import HTTPError

from config import GRAM_SERVER_URL, GRAM_PROJECT_SLUG, GRAM_API_KEY

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

passed = 0
failed = 0


def check(name, ok, detail=""):
    global passed, failed
    if ok:
        passed += 1
        print(f"  PASS: {name}" + (f" — {detail}" if detail else ""))
    else:
        failed += 1
        print(f"  FAIL: {name}" + (f" — {detail}" if detail else ""))


def gram_request(path, headers=None, method="GET", body=None):
    url = f"{GRAM_SERVER_URL}{path}"
    req = Request(url, data=body, method=method)
    req.add_header("Gram-Key", GRAM_API_KEY)
    if headers:
        for k, v in headers.items():
            req.add_header(k, v)
    with urlopen(req, timeout=30) as resp:
        return json.loads(resp.read())


def mcp_call(mcp_url, method, params, sid=None):
    payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = Request(mcp_url, data=payload, method="POST")
    req.add_header("Gram-Key", GRAM_API_KEY)
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json, text/event-stream")
    if sid:
        req.add_header("Mcp-Session-Id", sid)
    with urlopen(req, timeout=30) as resp:
        body = resp.read().decode()
        new_sid = resp.headers.get("Mcp-Session-Id", sid)
        ct = resp.headers.get("Content-Type", "")
        if "text/event-stream" in ct:
            for line in body.split("\n"):
                if line.startswith("data: "):
                    return json.loads(line[6:]), new_sid
        return json.loads(body), new_sid


# ---------------------------------------------------------------------------
# Step 1: Verify API key
# ---------------------------------------------------------------------------
print("=" * 60)
print("Step 1: Verify API key")
print("=" * 60)

verify = gram_request("/rpc/keys.verify")
org = verify.get("organization", {})
scopes = verify.get("scopes", [])
projects = [p.get("slug") for p in verify.get("projects", [])]

check("API key valid", bool(org.get("name")), f"org={org.get('name')}")
check("producer scope", "producer" in scopes, f"scopes={scopes}")
check("project accessible", GRAM_PROJECT_SLUG in projects, GRAM_PROJECT_SLUG)

# ---------------------------------------------------------------------------
# Step 2: Fetch MCP servers
# ---------------------------------------------------------------------------
print()
print("=" * 60)
print(f"Step 2: Fetch MCP servers for '{GRAM_PROJECT_SLUG}'")
print("=" * 60)

data = gram_request("/rpc/toolsets.list", {"Gram-Project": GRAM_PROJECT_SLUG})
toolsets = data.get("toolsets", [])

mcp_endpoints = []
for ts in toolsets:
    name = ts.get("slug", ts.get("name", "unknown"))
    mcp_slug = ts.get("mcp_slug")
    if not ts.get("mcp_enabled") or not mcp_slug:
        continue
    url = f"{GRAM_SERVER_URL}/mcp/{mcp_slug}"
    mcp_endpoints.append({"name": name, "url": url})

check("found MCP servers", len(mcp_endpoints) > 0, f"{len(mcp_endpoints)} server(s)")
for ep in mcp_endpoints:
    print(f"    {ep['name']}: {ep['url']}")

# ---------------------------------------------------------------------------
# Step 3: Test chat completions
# ---------------------------------------------------------------------------
print()
print("=" * 60)
print("Step 3: Test chat completions via Gram")
print("=" * 60)

try:
    resp = gram_request(
        "/chat/completions",
        headers={"Gram-Project": GRAM_PROJECT_SLUG, "Content-Type": "application/json"},
        method="POST",
        body=json.dumps({
            "model": "anthropic/claude-sonnet-4.5",
            "messages": [{"role": "user", "content": "Say hello in exactly 5 words."}],
            "stream": False,
        }).encode(),
    )
    content = resp["choices"][0]["message"]["content"]
    check("chat completion works", True, f'"{content}"')
except Exception as e:
    check("chat completion works", False, str(e))

# ---------------------------------------------------------------------------
# Step 4: Test MCP tool call
# ---------------------------------------------------------------------------
print()
print("=" * 60)
print("Step 4: Test MCP tool call via Gram")
print("=" * 60)

tool_call_ok = False
for ep in mcp_endpoints:
    try:
        data, sid = mcp_call(ep["url"], "initialize", {
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {"name": "gram-poc", "version": "0.1"},
        })
        check(f"MCP init ({ep['name']})", "result" in data)

        data, sid = mcp_call(ep["url"], "tools/list", {}, sid)
        tools = data.get("result", {}).get("tools", [])
        check(f"MCP tools/list ({ep['name']})", len(tools) > 0, f"{len(tools)} tool(s)")

        if not tools:
            continue

        tool = tools[0]
        schema = tool.get("inputSchema", {})
        required = schema.get("required", [])
        props = schema.get("properties", {})
        args = {}
        for r in required:
            prop = props.get(r, {})
            if prop.get("type") == "string":
                args[r] = "test"

        data, sid = mcp_call(ep["url"], "tools/call", {"name": tool["name"], "arguments": args}, sid)
        if "result" in data:
            content = data["result"].get("content", [])
            text = content[0].get("text", "") if content else ""
            is_error = any(w in text.lower()[:100] for w in ["error", "invalid", "expired", "unauthorized", "failed"])
            if not is_error and text:
                check(f"MCP tool call ({tool['name']})", True, text[:100])
                tool_call_ok = True
                break
            else:
                print(f"    {tool['name']}: upstream error, trying next server...")
        elif "error" in data:
            print(f"    {tool['name']}: MCP error, trying next server...")

    except Exception as e:
        print(f"    {ep['name']}: {e}")
        continue

if not tool_call_ok:
    check("MCP tool call", False, "no working tool call found")

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
print()
print("=" * 60)
print(f"Results: {passed} passed, {failed} failed")
print("=" * 60)

sys.exit(1 if failed else 0)
