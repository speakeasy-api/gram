import assert from "node:assert/strict";
import { test } from "node:test";
import { startMockAuthServer } from "./mock-server.ts";

test("anonymous transition resources stay distinct and are not OAuth audiences", async (t) => {
  const server = await startMockAuthServer();
  t.after(() => server.close());
  assert.notEqual(server.profiles.noAuth, server.profiles.noAuthTwin);
  for (const resource of [server.profiles.noAuth, server.profiles.noAuthTwin]) {
    const response = await fetch(resource, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "fixture_identity", arguments: {} },
      }),
    });
    assert.equal(response.status, 200);
    assert.notEqual((await response.json()).isError, true);
    assert.ok(
      server.events.some(
        (event) =>
          event.resource === resource &&
          event.principal === "anonymous" &&
          event.method === "tools/call" &&
          event.outcome === "authorized",
      ),
    );
  }
  const rejected = await fetch(`${server.providers.alpha.issuer}/token`, {
    method: "POST",
    body: new URLSearchParams({
      grant_type: "client_credentials",
      client_id: server.providers.alpha.clientId,
      client_secret: server.providers.alpha.clientSecret,
      resource: server.profiles.noAuthTwin,
      scope: "fixture:read",
    }),
  });
  assert.equal(rejected.status, 400);
  assert.equal((await rejected.json()).error, "invalid_target");
});
