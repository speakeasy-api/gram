import assert from "node:assert/strict";
import { test } from "node:test";
import { FixtureMcpClient } from "./client.ts";
import { startMockAuthServer } from "./mock-server.ts";

test("protocol driver interoperates with standalone upstreams without product setup", async () => {
  const upstream = await startMockAuthServer({ accessTokenLifetimeSeconds: 1 });
  const clients: FixtureMcpClient[] = [];
  const make = (endpoint: string, headers?: Record<string, string>) => {
    const client = new FixtureMcpClient({
      endpoint,
      headers,
      allowInsecureLoopback: true,
    });
    clients.push(client);
    return client;
  };
  try {
    for (const [endpoint, headers] of [
      [upstream.profiles.noAuth, undefined],
      [upstream.profiles.static, { "X-Fixture-Key": "fixture-static-secret" }],
      [
        upstream.profiles.passThrough,
        { "X-Fixture-User": "fixture-user-secret" },
      ],
    ] as const) {
      const client = make(endpoint, headers);
      await client.initialize();
      const tools = await client.listTools();
      assert.ok(tools.tools.some((tool) => tool.name === "fixture_echo"));
      const result = await client.callTool("fixture_echo", {
        marker: "protocol-fixture",
      });
      assert.ok(result);
    }
    const oauth = make(upstream.profiles.oauthAlpha);
    const pending = await oauth.beginAuthorization();
    try {
      const authorization = new URL(pending.authorizationUrl);
      const params = new URLSearchParams(authorization.searchParams);
      params.set("decision", "approve");
      // This unit test talks only to its own mock issuer. Product setup in
      // ui.spec.ts uses real browser interaction, never this shortcut.
      const approved = await fetch(authorization, {
        method: "POST",
        body: params,
        redirect: "manual",
      });
      assert.equal(approved.status, 302);
      const callback = approved.headers.get("location");
      assert.ok(callback);
      assert.equal((await fetch(callback)).status, 200);
      await pending.complete();
      await oauth.initialize();
      await oauth.callTool("fixture_echo", { marker: "oauth-fixture" });
      // Match the lifetime advertised by the issuer instead of simulating
      // an unexpected revocation of a token the product still considers fresh.
      await new Promise((resolve) => setTimeout(resolve, 1100));
      await assert.rejects(oauth.listTools());
      await oauth.refresh();
      await oauth.callTool("fixture_echo");
      await oauth.revoke();
      await assert.rejects(oauth.listTools());
      assert.ok(upstream.events.some((event) => event.kind === "token"));
      assert.ok(
        upstream.events.some((event) => event.principal === "alpha-user"),
      );
      const transcript = JSON.stringify(oauth.transcript);
      assert.ok(!transcript.includes("fixture-alpha-secret"));
    } finally {
      await pending.cancel();
    }
  } finally {
    await Promise.all(clients.map((client) => client.close()));
    await upstream.close();
  }
});
