#!/usr/bin/env node
// The only way out of the agent jail (see README.md).
//
// It runs as its own user, so the agent's user cannot read its environment,
// and the agent's user can reach no other host (iptables, set up by
// setup.sh). It does two things:
//
// 1. Anthropic API: plain HTTP from the agent on 127.0.0.1, forwarded over
//    HTTPS to the Anthropic API with the real key added here. The agent only
//    ever holds a placeholder key. Only the Messages endpoints are forwarded,
//    and a request that asks for server-side tools (web search, web fetch,
//    code execution, MCP) is refused: those would let the model reach the
//    internet from Anthropic's side.
// 2. CONNECT tunnels to port 443 of an allowlisted host, for a browser that
//    must reach one site (Magellan). The tunnel is refused if the host
//    resolves to a private, loopback or link-local address.
//
// Decisions are logged to JAIL_PROXY_LOG, never to stdout: CI logs are
// public, and a refused host name is chosen by the agent.

import dns from "node:dns/promises";
import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import net from "node:net";

const env = process.env;
const port = Number(env["JAIL_PROXY_PORT"] ?? "3128");
// jail.sh hands the key over in a file only the proxy's user can read.
const apiKey = env["JAIL_KEY_FILE"]
  ? fs.readFileSync(env["JAIL_KEY_FILE"], "utf8").trim()
  : (env["ANTHROPIC_API_KEY"] ?? "");
const upstream = new URL(
  env["JAIL_ANTHROPIC_UPSTREAM"] ?? "https://api.anthropic.com",
);
const allowedHosts = new Set(
  (env["JAIL_ALLOWED_HOSTS"] ?? "")
    .split(",")
    .map((h) => h.trim().toLowerCase())
    .filter(Boolean),
);
const logFile = env["JAIL_PROXY_LOG"] ?? "/dev/null";
// The key only ever travels over HTTPS. Plain HTTP is allowed for a loopback
// upstream alone, which is what the tests use.
if (
  upstream.protocol !== "https:" &&
  !(
    upstream.protocol === "http:" &&
    ["127.0.0.1", "[::1]", "localhost"].includes(upstream.hostname)
  )
) {
  throw new Error(
    "JAIL_ANTHROPIC_UPSTREAM must be https:// (or http:// on loopback)",
  );
}

const MESSAGE_PATHS = new Set(["/v1/messages", "/v1/messages/count_tokens"]);
const MAX_BODY = 32 * 1024 * 1024;
// Request headers passed on to Anthropic. Everything else, including any key
// or cookie the agent sends, is dropped.
const FORWARD_HEADERS = [
  "accept",
  "anthropic-beta",
  "anthropic-version",
  "content-type",
  "user-agent",
  "x-stainless-retry-count",
  "x-stainless-timeout",
];

const log = fs.createWriteStream(logFile, { flags: "a" });
function record(event, detail) {
  log.write(`${new Date().toISOString()} ${event} ${JSON.stringify(detail)}\n`);
}

// A tool the client declares and runs itself has no `type`, or type "custom".
// Any other type is a server tool that Anthropic runs on the model's behalf.
export function refusalReason(body) {
  if (body === null || typeof body !== "object" || Array.isArray(body)) {
    return "body is not a JSON object";
  }
  for (const key of ["mcp_servers", "container"]) {
    if (key in body) return `field ${key} is not allowed`;
  }
  if ("tools" in body) {
    if (!Array.isArray(body.tools)) return "tools is not an array";
    for (const tool of body.tools) {
      if (tool === null || typeof tool !== "object") return "bad tool";
      if ("type" in tool && tool.type !== "custom") {
        return `server tool ${String(tool.type).slice(0, 60)} is not allowed`;
      }
    }
  }
  return null;
}

// Everything that is not a globally routable unicast address: private,
// loopback, link-local, shared, documentation, benchmarking, multicast and
// reserved ranges, and IPv6 forms that embed an IPv4 address.
// Separate lists: one BlockList also matches IPv4 against ::ffff:0:0/96 rules.
const NON_PUBLIC_V4 = new net.BlockList();
const NON_PUBLIC_V6 = new net.BlockList();
for (const [range, bits] of [
  ["0.0.0.0", 8],
  ["10.0.0.0", 8],
  ["100.64.0.0", 10],
  ["127.0.0.0", 8],
  ["169.254.0.0", 16],
  ["172.16.0.0", 12],
  ["192.0.0.0", 24],
  ["192.0.2.0", 24],
  ["192.88.99.0", 24],
  ["192.168.0.0", 16],
  ["198.18.0.0", 15],
  ["198.51.100.0", 24],
  ["203.0.113.0", 24],
  ["224.0.0.0", 4],
  ["240.0.0.0", 4],
]) {
  NON_PUBLIC_V4.addSubnet(range, bits, "ipv4");
}
for (const [range, bits] of [
  ["::", 96], // unspecified, loopback and IPv4-compatible
  ["::ffff:0:0", 96], // IPv4-mapped: checked as IPv4 below
  ["64:ff9b::", 96], // NAT64
  ["64:ff9b:1::", 48],
  ["100::", 64], // discard
  ["2001::", 23], // IETF protocol assignments, incl. Teredo
  ["2001:db8::", 32], // documentation
  ["2002::", 16], // 6to4
  ["fc00::", 7], // unique local
  ["fe80::", 10], // link-local
  ["fec0::", 10], // site-local
  ["ff00::", 8], // multicast
]) {
  NON_PUBLIC_V6.addSubnet(range, bits, "ipv6");
}
const GLOBAL_UNICAST_V6 = new net.BlockList();
GLOBAL_UNICAST_V6.addSubnet("2000::", 3, "ipv6");

export function isPublicAddress(address) {
  const family = net.isIP(address);
  if (family === 4) return !NON_PUBLIC_V4.check(address, "ipv4");
  if (family === 6) {
    const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(address);
    if (mapped) return isPublicAddress(mapped[1]);
    return (
      GLOBAL_UNICAST_V6.check(address, "ipv6") &&
      !NON_PUBLIC_V6.check(address, "ipv6")
    );
  }
  return false;
}

function deny(res, status, reason) {
  record("deny", { status, reason });
  // An upstream that fails after the response started can only be cut off.
  if (res.headersSent || res.destroyed) {
    res.destroy();
    return;
  }
  res.writeHead(status, { "content-type": "application/json" });
  res.end(
    JSON.stringify({
      type: "error",
      error: { type: "jail_denied", message: reason },
    }),
  );
}

async function forwardMessages(req, res, path, search) {
  const chunks = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > MAX_BODY) return deny(res, 413, "request body too large");
    chunks.push(chunk);
  }
  const raw = Buffer.concat(chunks);
  let body;
  try {
    body = JSON.parse(raw.toString("utf8"));
  } catch {
    return deny(res, 400, "body is not JSON");
  }
  const reason = refusalReason(body);
  if (reason) return deny(res, 403, reason);

  const headers = { "x-api-key": apiKey, "content-length": raw.length };
  for (const name of FORWARD_HEADERS) {
    const value = req.headers[name];
    if (value !== undefined) headers[name] = value;
  }
  const client = upstream.protocol === "http:" ? http : https;
  const out = client.request(
    {
      protocol: upstream.protocol,
      hostname: upstream.hostname,
      port: upstream.port || undefined,
      method: "POST",
      path: `${path}${search}`,
      headers,
    },
    (upstreamRes) => {
      record("messages", { path, status: upstreamRes.statusCode });
      upstreamRes.on("error", () => res.destroy());
      const passed = { ...upstreamRes.headers };
      delete passed["set-cookie"];
      res.writeHead(upstreamRes.statusCode ?? 502, passed);
      upstreamRes.pipe(res);
    },
  );
  out.on("error", () => deny(res, 502, "upstream error"));
  out.end(raw);
}

const server = http.createServer((req, res) => {
  // Absolute-form requests are plain-HTTP proxying, which nothing may use.
  if (!req.url || !req.url.startsWith("/")) {
    return deny(res, 403, "only the Anthropic API is reachable");
  }
  let url;
  try {
    url = new URL(req.url, "http://jail");
  } catch {
    return deny(res, 400, "bad request target");
  }
  if (req.method === "POST" && MESSAGE_PATHS.has(url.pathname)) {
    forwardMessages(req, res, url.pathname, url.search).catch(() =>
      deny(res, 500, "proxy error"),
    );
    return;
  }
  return deny(res, 404, "only the Anthropic Messages API is reachable");
});

server.on("connect", (req, socket, head) => {
  const refuse = (reason) => {
    record("deny-connect", { reason });
    socket.end("HTTP/1.1 403 Forbidden\r\n\r\n");
  };
  const match = /^([a-z0-9.-]+):(\d+)$/i.exec(req.url ?? "");
  if (!match) return refuse("bad target");
  const host = match[1].toLowerCase();
  if (match[2] !== "443") return refuse("only port 443");
  if (!allowedHosts.has(host)) return refuse(`host not allowed: ${host}`);

  dns
    .lookup(host, { all: true })
    .then((addresses) => {
      const usable = addresses.filter((a) => isPublicAddress(a.address));
      // Every address must be public: a host that also resolves to a private
      // address could be pointed at it on the next lookup.
      if (usable.length === 0 || usable.length !== addresses.length) {
        return refuse(`host resolves to a private address: ${host}`);
      }
      const target = net.connect(443, usable[0].address, () => {
        record("connect", { host });
        socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        if (head.length) target.write(head);
        target.pipe(socket);
        socket.pipe(target);
      });
      target.on("error", () => socket.destroy());
      socket.on("error", () => target.destroy());
    })
    .catch(() => refuse(`lookup failed: ${host}`));
});

// The proxy is the jail's only way out: one bad connection must not take it
// down for every other.
server.on("clientError", (_err, socket) => socket.destroy());

const isMain = import.meta.url === `file://${process.argv[1]}`;
if (isMain) {
  process.on("uncaughtException", (err) =>
    record("error", { message: String(err).slice(0, 200) }),
  );
  if (!apiKey) {
    console.error("no Anthropic key: set JAIL_KEY_FILE or ANTHROPIC_API_KEY");
    process.exit(1);
  }
  server.listen(port, "127.0.0.1", () => {
    console.log(`agent-jail proxy listening on 127.0.0.1:${port}`);
  });
}

export { server };
