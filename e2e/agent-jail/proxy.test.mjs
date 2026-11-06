// Run with: node --test e2e/agent-jail/
import assert from "node:assert/strict";
import http from "node:http";
import net from "node:net";
import { after, before, test } from "node:test";

let upstream;
let seen;
let proxyPort;
let mod;

before(async () => {
  upstream = http.createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      seen = { url: req.url, headers: req.headers, body };
      res.writeHead(200, {
        "content-type": "application/json",
        "set-cookie": "x=1",
      });
      res.end('{"ok":true}');
    });
  });
  await new Promise((r) => upstream.listen(0, "127.0.0.1", r));
  process.env["ANTHROPIC_API_KEY"] = "real-key";
  process.env["JAIL_ANTHROPIC_UPSTREAM"] =
    `http://127.0.0.1:${upstream.address().port}`;
  process.env["JAIL_ALLOWED_HOSTS"] = "allowed.example";
  mod = await import("./proxy.mjs");
  await new Promise((r) => mod.server.listen(0, "127.0.0.1", r));
  proxyPort = mod.server.address().port;
});

after(() => {
  mod.server.close();
  upstream.close();
});

function post(path, body, headers = {}) {
  return fetch(`http://127.0.0.1:${proxyPort}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
}

test("forwards messages with the real key and drops the agent's", async () => {
  const res = await post(
    "/v1/messages?beta=true",
    { model: "m", messages: [], tools: [{ name: "Read", input_schema: {} }] },
    { "x-api-key": "placeholder", authorization: "Bearer stolen", cookie: "c" },
  );
  assert.equal(res.status, 200);
  assert.equal(res.headers.get("set-cookie"), null);
  assert.equal(seen.url, "/v1/messages?beta=true");
  assert.equal(seen.headers["x-api-key"], "real-key");
  assert.equal(seen.headers["authorization"], undefined);
  assert.equal(seen.headers["cookie"], undefined);
});

test("refuses server tools", async () => {
  seen = undefined;
  for (const tool of [
    { type: "web_search_20250305", name: "web_search" },
    { type: "web_fetch_20250910", name: "web_fetch" },
    { type: "code_execution_20250825", name: "code_execution" },
  ]) {
    const res = await post("/v1/messages", { messages: [], tools: [tool] });
    assert.equal(res.status, 403);
  }
  const mcp = await post("/v1/messages", { messages: [], mcp_servers: [] });
  assert.equal(mcp.status, 403);
  assert.equal(seen, undefined);
});

test("allows custom tools", async () => {
  const res = await post("/v1/messages", {
    messages: [],
    tools: [{ type: "custom", name: "x", input_schema: {} }],
  });
  assert.equal(res.status, 200);
});

test("refuses other paths, methods, bad JSON and absolute URLs", async () => {
  assert.equal((await post("/v1/files", {})).status, 404);
  const get = await fetch(`http://127.0.0.1:${proxyPort}/v1/messages`);
  assert.equal(get.status, 404);
  assert.equal((await post("/v1/messages", "not json")).status, 400);
  const absolute = await new Promise((resolve) => {
    const req = http.request(
      {
        host: "127.0.0.1",
        port: proxyPort,
        path: "http://evil.example/v1/messages",
      },
      resolve,
    );
    req.end();
  });
  assert.equal(absolute.statusCode, 403);
});

function connect(target) {
  return new Promise((resolve) => {
    const s = net.connect(proxyPort, "127.0.0.1", () => {
      s.write(`CONNECT ${target} HTTP/1.1\r\nHost: ${target}\r\n\r\n`);
    });
    s.once("data", (d) => {
      resolve(d.toString().split("\r\n")[0]);
      s.destroy();
    });
  });
}

test("refuses CONNECT to hosts off the list, other ports, private addresses", async () => {
  assert.match(await connect("evil.example:443"), /403/);
  assert.match(await connect("allowed.example:80"), /403/);
  // localhost is not on the list; and if it were, it resolves privately.
  assert.match(await connect("localhost:443"), /403/);
});

test("classifies addresses", () => {
  for (const a of [
    "127.0.0.1",
    "10.1.2.3",
    "169.254.169.254",
    "172.20.0.1",
    "192.168.1.1",
    "100.64.0.1",
    "::1",
    "fe80::1",
    "fd00::1",
    "::ffff:127.0.0.1",
  ]) {
    assert.equal(mod.isPublicAddress(a), false, a);
  }
  for (const a of ["1.1.1.1", "2606:4700::1111"]) {
    assert.equal(mod.isPublicAddress(a), true, a);
  }
});
