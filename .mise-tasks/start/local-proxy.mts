#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Serve Gram locally from one gram.local origin"
//MISE dir="{{ config_root }}"

import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import net from "node:net";
import tls from "node:tls";

const proxyHost = process.env["GRAM_LOCAL_PROXY_HOST"] || "gram.local";
const proxyBindAddress =
  process.env["GRAM_LOCAL_PROXY_BIND_ADDRESS"] || "127.0.0.1";
const proxyPort = Number(process.env["GRAM_LOCAL_PROXY_PORT"] || "8443");
const certFile = process.env["GRAM_SSL_CERT_FILE"];
const keyFile = process.env["GRAM_SSL_KEY_FILE"];

if (!certFile || !keyFile) {
  throw new Error("GRAM_SSL_CERT_FILE and GRAM_SSL_KEY_FILE must be set");
}

const serverTarget = new URL(
  process.env["GRAM_SERVER_BACKEND_URL"] ||
    `https://localhost:${process.env["GRAM_SERVER_PORT"] || "8080"}`,
);
const dashboardTarget = new URL(
  process.env["GRAM_SITE_BACKEND_URL"] ||
    `https://localhost:${process.env["GRAM_SITE_PORT"] || "5173"}`,
);

const serverPathPrefixes = [
  "/rpc",
  "/chat",
  "/mcp",
  "/oauth",
  "/oauth-external",
  "/.well-known",
  "/v1",
];

const serverExactPaths = new Set(["/healthz", "/openapi.yaml"]);

function targetFor(rawURL: string | undefined): URL {
  const pathname = new URL(rawURL || "/", "https://local.invalid").pathname;
  if (
    serverExactPaths.has(pathname) ||
    serverPathPrefixes.some(
      (prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`),
    )
  ) {
    return serverTarget;
  }

  return dashboardTarget;
}

function targetPort(target: URL): number {
  if (target.port) return Number(target.port);
  return target.protocol === "https:" ? 443 : 80;
}

function forwardedHeaders(
  req: http.IncomingMessage,
  target: URL,
): http.OutgoingHttpHeaders {
  return {
    ...req.headers,
    host: target.host,
    "x-forwarded-host": req.headers.host || `${proxyHost}:${proxyPort}`,
    "x-forwarded-proto": "https",
    "x-forwarded-for": [
      req.socket.remoteAddress,
      req.headers["x-forwarded-for"],
    ]
      .filter(Boolean)
      .join(", "),
  };
}

function proxyHTTP(req: http.IncomingMessage, res: http.ServerResponse): void {
  const target = targetFor(req.url);
  const client = target.protocol === "https:" ? https : http;
  const upstream = client.request(
    {
      protocol: target.protocol,
      hostname: target.hostname,
      port: targetPort(target),
      method: req.method,
      path: req.url,
      headers: forwardedHeaders(req, target),
      rejectUnauthorized: false,
    },
    (upstreamRes) => {
      res.writeHead(
        upstreamRes.statusCode || 502,
        upstreamRes.statusMessage,
        upstreamRes.headers,
      );
      upstreamRes.pipe(res);
    },
  );

  upstream.on("error", (error) => {
    if (!res.headersSent) {
      res.writeHead(502, { "content-type": "text/plain; charset=utf-8" });
    }
    res.end(`local proxy upstream error: ${error.message}\n`);
  });

  req.pipe(upstream);
}

function proxyUpgrade(
  req: http.IncomingMessage,
  socket: net.Socket,
  head: Buffer,
): void {
  const target = targetFor(req.url);
  const writeRequest = () => {
    const headers = forwardedHeaders(req, target);
    upstream.write(
      `${req.method} ${req.url || "/"} HTTP/${req.httpVersion}\r\n`,
    );
    for (const [name, value] of Object.entries(headers)) {
      if (Array.isArray(value)) {
        for (const item of value) upstream.write(`${name}: ${item}\r\n`);
      } else if (value !== undefined) {
        upstream.write(`${name}: ${value}\r\n`);
      }
    }
    upstream.write("\r\n");
    if (head.length > 0) upstream.write(head);
    socket.pipe(upstream).pipe(socket);
  };

  const upstream =
    target.protocol === "https:"
      ? tls.connect(
          {
            host: target.hostname,
            port: targetPort(target),
            servername: target.hostname,
            rejectUnauthorized: false,
          },
          writeRequest,
        )
      : net.connect(
          { host: target.hostname, port: targetPort(target) },
          writeRequest,
        );

  upstream.on("error", () => {
    socket.destroy();
  });
}

const proxy = https.createServer(
  {
    cert: fs.readFileSync(certFile),
    key: fs.readFileSync(keyFile),
  },
  proxyHTTP,
);

proxy.on("upgrade", proxyUpgrade);

proxy.listen(proxyPort, proxyBindAddress, () => {
  console.log(`Gram local proxy: https://${proxyHost}:${proxyPort}`);
  console.log(`  bind      -> ${proxyBindAddress}:${proxyPort}`);
  console.log(`  dashboard -> ${dashboardTarget.toString()}`);
  console.log(`  api       -> ${serverTarget.toString()}`);
});
