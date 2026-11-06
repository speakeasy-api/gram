import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type IncomingMessage } from "node:http";
import { createHash } from "node:crypto";
import { FixtureMcpClient } from "./client.ts";

async function body(req: IncomingMessage): Promise<string> {
  let value = "";
  for await (const chunk of req) value += chunk;
  return value;
}

async function fixture(implicitBasic = false) {
  let origin = "";
  let redirect = "";
  let challenge = "";
  const revokedTokens: string[] = [];
  let refreshes = 0;
  const calls: Array<{
    path: string;
    authorization?: string;
    session?: string;
    key?: string;
  }> = [];
  const server = createServer(async (req, res) => {
    try {
      const path = new URL(req.url!, origin).pathname;
      calls.push({
        path,
        authorization: req.headers.authorization,
        session: req.headers["mcp-session-id"] as string,
        key: req.headers["x-agent-key"] as string,
      });
      const json = (value: unknown) => {
        res.setHeader("Content-Type", "application/json");
        res.end(JSON.stringify(value));
      };
      if (path === "/mcp" && req.method === "GET") {
        res
          .writeHead(401, {
            "WWW-Authenticate": `Bearer resource_metadata="${origin}/.well-known/oauth-protected-resource/mcp"`,
          })
          .end();
      } else if (path === "/.well-known/oauth-protected-resource/mcp") {
        json({
          resource: `${origin}/mcp`,
          authorization_servers: [`${origin}/issuer`],
          scopes_supported: ["fixture:read"],
        });
      } else if (path === "/.well-known/oauth-authorization-server/issuer") {
        json({
          issuer: `${origin}/issuer`,
          authorization_endpoint: `${origin}/authorize`,
          token_endpoint: `${origin}/token`,
          registration_endpoint: `${origin}/register`,
          revocation_endpoint: `${origin}/revoke`,
          code_challenge_methods_supported: ["S256"],
        });
      } else if (path === "/register") {
        const registration = JSON.parse(await body(req));
        assert.equal(registration.token_endpoint_auth_method, "none");
        redirect = registration.redirect_uris[0];
        assert.equal(new URL(redirect).hostname, "127.0.0.1");
        json({
          client_id: "fixture-client",
          ...(implicitBasic
            ? { client_secret: "private-client-secret" }
            : { token_endpoint_auth_method: "none" }),
        });
      } else if (path === "/authorize") {
        const url = new URL(req.url!, origin);
        assert.equal(url.searchParams.get("scope"), "fixture:read");
        assert.equal(url.searchParams.get("code_challenge_method"), "S256");
        assert.equal(url.searchParams.get("redirect_uri"), redirect);
        assert.equal(url.searchParams.get("resource"), `${origin}/mcp`);
        challenge = url.searchParams.get("code_challenge")!;
        res
          .writeHead(302, {
            Location: `${redirect}?code=private-code&state=${url.searchParams.get("state")}`,
          })
          .end();
      } else if (path === "/token") {
        const params = new URLSearchParams(await body(req));
        if (implicitBasic) {
          assert.equal(
            req.headers.authorization,
            `Basic ${Buffer.from("fixture-client:private-client-secret").toString("base64")}`,
          );
          assert.equal(params.has("client_secret"), false);
        } else assert.equal(params.get("client_id"), "fixture-client");
        assert.equal(params.get("resource"), `${origin}/mcp`);
        if (params.get("grant_type") === "authorization_code") {
          assert.equal(params.get("code"), "private-code");
          assert.equal(params.get("redirect_uri"), redirect);
          assert.equal(
            createHash("sha256")
              .update(params.get("code_verifier")!)
              .digest("base64url"),
            challenge,
          );
        } else {
          assert.equal(params.get("refresh_token"), "private-refresh");
          refreshes++;
        }
        json({
          access_token: refreshes ? "private-refreshed" : "private-access",
          refresh_token: "private-refresh",
          token_type: "Bearer",
        });
      } else if (path === "/revoke") {
        const params = new URLSearchParams(await body(req));
        assert.ok(
          ["private-refreshed", "private-refresh"].includes(
            params.get("token")!,
          ),
        );
        revokedTokens.push(params.get("token")!);
        res.end();
      } else if (path === "/mcp" && req.method === "POST") {
        if (
          revokedTokens.length > 0 ||
          (![
            "Bearer private-access",
            "Bearer private-refreshed",
            "Bearer private-bearer",
          ].includes(req.headers.authorization ?? "") &&
            req.headers["x-agent-key"] !== "private-key")
        ) {
          res.writeHead(401).end();
          return;
        }
        const message = JSON.parse(await body(req));
        if (message.method === "notifications/initialized") {
          res.writeHead(202).end();
          return;
        }
        res.setHeader("mcp-session-id", "fixture-session");
        const result =
          message.method === "initialize"
            ? {
                protocolVersion: "2025-03-26",
                capabilities: {},
                serverInfo: { name: "fixture", version: "1" },
              }
            : message.method === "tools/list"
              ? { tools: [{ name: "echo" }] }
              : {
                  content: [
                    {
                      type: "text",
                      text: JSON.stringify(message.params.arguments),
                    },
                  ],
                };
        if (message.method === "tools/list") {
          res.setHeader("Content-Type", "text/event-stream");
          res.write(
            ': ping\r\n\r\ndata: {"jsonrpc":"2.0","method":"notifications/ping"}\r\n\r\n',
          );
          // Split an event over chunks and keep the stream open: the client must cancel it.
          res.write("event: message\r\ndata: ");
          res.write(
            JSON.stringify({ jsonrpc: "2.0", id: message.id, result }) +
              "\r\n\r\n",
          );
        } else json({ jsonrpc: "2.0", id: message.id, result });
      } else res.writeHead(404).end();
    } catch {
      res.writeHead(500).end("Fixture assertion failed");
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  origin = `http://127.0.0.1:${address.port}`;
  return {
    origin,
    calls,
    revokedTokens,
    close: () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      }),
  };
}

test("OAuth discovery, DCR, strict state, PKCE, SSE, refresh and revocation", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    const metadata = await client.discover();
    assert.equal(metadata.authorizationServer, `${f.origin}/issuer`);
    const auth = await client.beginAuthorization();
    const redirectResponse = await fetch(auth.authorizationUrl, {
      redirect: "manual",
    });
    const callback = new URL(redirectResponse.headers.get("location")!);
    const wrong = new URL(callback);
    wrong.searchParams.set("state", "wrong");
    assert.equal((await fetch(wrong)).status, 400);
    const duplicate = new URL(callback);
    duplicate.searchParams.append("state", callback.searchParams.get("state")!);
    assert.equal((await fetch(duplicate)).status, 400);
    const completion = auth.complete();
    const accepted = await fetch(callback);
    assert.equal(accepted.status, 200);
    assert.match(await accepted.text(), /close this window/);
    await completion;
    await auth.complete(); // Idempotent: no second exchange.
    await assert.rejects(fetch(callback));
    await client.initialize();
    assert.deepEqual(await client.listTools(), { tools: [{ name: "echo" }] });
    assert.deepEqual(await client.callTool("echo", { hello: "world" }), {
      content: [{ type: "text", text: '{"hello":"world"}' }],
    });
    await client.refresh();
    await client.callTool("echo");
    assert.equal(f.calls.at(-1)?.authorization, "Bearer private-refreshed");
    assert.equal(f.calls.at(-1)?.session, "fixture-session");
    await client.revoke();
    assert.deepEqual([...f.revokedTokens].sort(), [
      "private-refresh",
      "private-refreshed",
    ]);
    await assert.rejects(client.initialize(), /401/);
    await assert.rejects(client.callTool("echo"), /401/);
    await assert.rejects(client.refresh(), /No refresh token/);
    assert.doesNotMatch(
      JSON.stringify(client.transcript),
      /private-|code_verifier|state|127\.0\.0\.1/,
    );
    assert.ok(
      f.calls
        .filter((call) => call.path !== "/mcp")
        .every((call) => !call.authorization && !call.key),
    );
  } finally {
    await client.close();
    await f.close();
  }
});

test("fresh initialize omits the previous MCP session", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
    bearerToken: "private-bearer",
  });
  try {
    await client.initialize();
    await client.callTool("echo");
    assert.equal(f.calls.at(-1)?.session, "fixture-session");
    const start = f.calls.length;
    await client.initialize();
    assert.equal(f.calls[start]?.session, undefined);
    assert.equal(f.calls[start + 1]?.session, "fixture-session");
  } finally {
    await client.close();
    await f.close();
  }
});

test("new authorization drops the MCP session but refresh preserves it", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
  });
  const authorize = async () => {
    const auth = await client.beginAuthorization();
    const response = await fetch(auth.authorizationUrl, { redirect: "manual" });
    const completion = auth.complete();
    await fetch(response.headers.get("location")!);
    await completion;
  };
  try {
    await authorize();
    await client.initialize();
    await client.refresh();
    await client.callTool("echo");
    assert.equal(f.calls.at(-1)?.authorization, "Bearer private-refreshed");
    assert.equal(f.calls.at(-1)?.session, "fixture-session");
    await authorize();
    // Inspect the exchange reset independently of initialize's own reset.
    await client.callTool("echo");
    assert.equal(f.calls.at(-1)?.session, undefined);
    assert.equal(f.calls.filter((call) => call.path === "/register").length, 2);
    const start = f.calls.length;
    await client.initialize();
    assert.equal(f.calls[start]?.session, undefined);
    assert.equal(f.calls[start + 1]?.session, "fixture-session");
  } finally {
    await client.close();
    await f.close();
  }
});

test("supplied API headers and bearer token are limited to resource requests", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
    headers: { "X-Agent-Key": "private-key" },
    bearerToken: "private-bearer",
  });
  try {
    await client.discover();
    await client.initialize();
    const post = f.calls.find(
      (call) => call.authorization === "Bearer private-bearer",
    );
    assert.equal(post?.key, "private-key");
    assert.ok(
      f.calls
        .filter((call) => call.path !== "/mcp")
        .every((call) => !call.authorization && !call.key),
    );
  } finally {
    await client.close();
    await f.close();
  }
});

test("cancellation rejects completion and closes the callback listener", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    const auth = await client.beginAuthorization();
    const callback = new URL(auth.authorizationUrl).searchParams.get(
      "redirect_uri",
    )!;
    await auth.cancel();
    await assert.rejects(auth.complete(), /cancelled/);
    await assert.rejects(fetch(callback));
    await client.close();
    await assert.rejects(client.discover(), /closed/);
  } finally {
    await client.close();
    await f.close();
  }
});

test("denied callback fails without exchanging a code and cleans up", async () => {
  const f = await fixture();
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    const auth = await client.beginAuthorization();
    const authorization = new URL(auth.authorizationUrl);
    const callback = new URL(authorization.searchParams.get("redirect_uri")!);
    callback.searchParams.set(
      "state",
      authorization.searchParams.get("state")!,
    );
    callback.searchParams.set("error", "access_denied");
    assert.equal((await fetch(callback)).status, 400);
    await assert.rejects(auth.complete(), /callback failed/);
    assert.equal(f.calls.filter((call) => call.path === "/token").length, 0);
    await assert.rejects(fetch(callback));
  } finally {
    await client.close();
    await f.close();
  }
});

test("discovery uses exact well-known resource path without a challenge", async () => {
  const paths: string[] = [];
  let origin = "";
  const server = createServer((req, res) => {
    paths.push(req.url!);
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/nested/mcp") res.writeHead(405).end();
    else if (req.url === "/.well-known/oauth-protected-resource/nested/mcp") {
      res.end(
        JSON.stringify({
          resource: `${origin}/nested/mcp`,
          authorization_servers: [origin],
        }),
      );
    } else if (req.url === "/.well-known/oauth-authorization-server") {
      res.end(
        JSON.stringify({
          issuer: origin,
          authorization_endpoint: `${origin}/authorize`,
          token_endpoint: `${origin}/token`,
        }),
      );
    } else res.writeHead(404).end();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  origin = `http://127.0.0.1:${address.port}`;
  const client = new FixtureMcpClient({
    endpoint: `${origin}/nested/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    assert.equal((await client.discover()).resource, `${origin}/nested/mcp`);
    assert.deepEqual(paths, [
      "/nested/mcp",
      "/.well-known/oauth-protected-resource/nested/mcp",
      "/.well-known/oauth-authorization-server",
    ]);
    await assert.rejects(
      client.beginAuthorization(),
      /registration is unavailable/,
    );
  } finally {
    await client.close();
    await new Promise<void>((resolve) => {
      server.close(() => resolve());
      server.closeAllConnections();
    });
  }
});

test("destination policy rejects remote, insecure and credential-bearing URLs", () => {
  for (const endpoint of [
    "https://example.com/mcp",
    "http://127.0.0.1/mcp",
    "https://user:password@localhost/mcp",
  ]) {
    assert.throws(() => new FixtureMcpClient({ endpoint }), /destination/);
  }
  assert.doesNotThrow(
    () =>
      new FixtureMcpClient({
        endpoint: "https://local.test/mcp",
        trustedOrigins: ["https://local.test"],
      }),
  );
  assert.throws(
    () =>
      new FixtureMcpClient({
        endpoint: "http://local.test/mcp",
        trustedOrigins: ["http://local.test"],
        allowInsecureLoopback: true,
      }),
    /destination/,
  );
});

test("caller header changes preserve authorization and cancellation closes real HTTP", async () => {
  const received: Array<{ caller?: string; authorization?: string }> = [];
  let started!: () => void;
  const pendingRequest = new Promise<void>((resolve) => {
    started = resolve;
  });
  let disconnected!: () => void;
  const closedRequest = new Promise<void>((resolve) => {
    disconnected = resolve;
  });
  const server = createServer(async (req, res) => {
    const message = JSON.parse(await body(req));
    received.push({
      caller: req.headers["x-fixture-key"] as string | undefined,
      authorization: req.headers.authorization,
    });
    if (message.params.name === "wait") {
      res.on("close", disconnected);
      started();
      return;
    }
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: { isError: false },
      }),
    );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const client = new FixtureMcpClient({
    endpoint: `http://127.0.0.1:${address.port}/mcp`,
    allowInsecureLoopback: true,
    bearerToken: "fixture-caller-token",
  });
  try {
    for (const name of [
      "authorization",
      "cookie",
      "Gram-Key",
      "mcp-session-id",
      "X-Gram-Key",
      "X-Forwarded-For",
      "X-Agent-Key",
      "Content-Type",
    ]) {
      assert.throws(
        () => client.setCallerHeader(name, "forbidden"),
        /non-control/,
      );
    }
    for (const value of ["right", undefined, "wrong", "right"]) {
      client.setCallerHeader("X-Fixture-Key", value);
      await client.callTool("echo");
    }
    assert.deepEqual(
      received.map((call) => call.caller),
      ["right", undefined, "wrong", "right"],
    );
    assert.ok(
      received.every(
        (call) => call.authorization === "Bearer fixture-caller-token",
      ),
    );
    const controller = new AbortController();
    const pending = client.callTool("wait", {}, controller.signal);
    const rejected = assert.rejects(pending, /tools\/call transport failed/);
    await pendingRequest;
    controller.abort();
    await rejected;
    await Promise.race([
      closedRequest,
      new Promise<never>((_, reject) => {
        const timer = setTimeout(
          () => reject(new Error("HTTP request did not close")),
          2_000,
        );
        timer.unref();
      }),
    ]);
    await client.callTool("echo");
    assert.equal(received.at(-1)?.authorization, "Bearer fixture-caller-token");
  } finally {
    await client.close();
    await new Promise<void>((resolve) => {
      server.close(() => resolve());
      server.closeAllConnections();
    });
  }
});

test("registration defaults to basic when a secret is returned without an auth method", async () => {
  const f = await fixture(true);
  const client = new FixtureMcpClient({
    endpoint: `${f.origin}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    const auth = await client.beginAuthorization();
    await fetch(auth.authorizationUrl);
    await auth.complete();
    await client.callTool("echo");
    assert.equal(f.calls.at(-1)?.authorization, "Bearer private-access");
  } finally {
    await client.close();
    await f.close();
  }
});

test("one HTTP-success RPC error produces exactly one enriched transcript event", async () => {
  const server = createServer(async (req, res) => {
    const message = JSON.parse(await body(req));
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        error: {
          code: -32000,
          message: "session authorization is no longer valid",
        },
      }),
    );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const client = new FixtureMcpClient({
    endpoint: `http://127.0.0.1:${address.port}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    await assert.rejects(client.callTool("echo"), /JSON-RPC error/);
    assert.deepEqual(client.transcript, [
      {
        operation: "tools/call",
        status: 200,
        sessionSent: false,
        protocolCode: -32000,
        reason: "session_authorization_invalid",
      },
    ]);
  } finally {
    await client.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test("invalid_target is retained as a safe OAuth diagnostic", async () => {
  const server = createServer((_req, res) => {
    res.writeHead(400, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "invalid_target" }));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const client = new FixtureMcpClient({
    endpoint: `http://127.0.0.1:${address.port}/mcp`,
    allowInsecureLoopback: true,
  });
  try {
    await assert.rejects(client.discover());
    assert.equal(client.transcript[0]?.protocolCode, "invalid_target");
  } finally {
    await client.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
