#!/usr/bin/env node

//MISE description="Check local fixture TLS, isolated control, DCR/CIMD authorization, refresh and echo"
//MISE dir="{{ config_root }}"
//USAGE flag "--dry-run" help="Describe checks without connecting or creating scenarios"
//USAGE flag "--health-only" help="Readiness only; no scenarios or OAuth requests"

import assert from "node:assert/strict";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { createServer, request } from "node:https";
import type { IncomingHttpHeaders } from "node:http";
import { dryRun, fail, readConfig, validateTLS } from "./lib.ts";

// One MiB bounds fixture evidence and MCP replies; each request has a five-second deadline.
const maxBytes = 1 << 20;
const deadlineMS = 5_000;

function json(body: string) {
  try {
    return JSON.parse(body);
  } catch {
    throw new Error(
      "Fixture returned invalid JSON; response contents withheld.",
    );
  }
}

async function smoke() {
  if (dryRun) {
    console.log(
      "Would verify trusted HTTPS readiness and authenticated control, then create isolated DCR/CIMD scenarios, authorize with PKCE, follow the HTTPS callback, refresh, invoke an echo tool, check events, and delete only those scenarios.",
    );
    return;
  }
  const config = readConfig();
  validateTLS(config);
  const ca = readFileSync(config["FIXTURE_CA_FILE"]);
  async function send(
    url: string,
    method = "GET",
    body?: string,
    headers: Record<string, string> = {},
  ) {
    return await new Promise<{
      status: number;
      headers: IncomingHttpHeaders;
      body: string;
    }>((accept, reject) => {
      const req = request(
        url,
        {
          method,
          ca,
          headers,
          timeout: deadlineMS,
          signal: AbortSignal.timeout(deadlineMS),
        },
        (res) => {
          const chunks: Buffer[] = [];
          let length = 0;
          res.on("data", (chunk: Buffer) => {
            length += chunk.length;
            if (length > maxBytes) req.destroy(new Error("response limit"));
            else chunks.push(chunk);
          });
          res.on("end", () =>
            accept({
              status: res.statusCode ?? 0,
              headers: res.headers,
              body: Buffer.concat(chunks).toString("utf8"),
            }),
          );
          res.once("error", () =>
            reject(new Error("Fixture response interrupted.")),
          );
        },
      );
      req.once("timeout", () => req.destroy(new Error("deadline")));
      req.once("error", () =>
        reject(
          new Error(
            "Fixture HTTPS request failed; check the daemon, local CA and origin allowlist.",
          ),
        ),
      );
      req.end(body);
    });
  }
  const base = config["FIXTURE_BASE_URL"];
  assert.ok(
    (await send(`${base}/readyz`)).status === 200,
    "HTTPS readiness failed.",
  );
  assert.ok(
    (await send(`${base}/__control/presets`)).status === 401,
    "Control accepted a missing secret.",
  );
  let bootID: string | undefined;
  async function control(path: string, method = "GET", body?: object) {
    const response = await send(
      `${base}/__control/${path}`,
      method,
      body && JSON.stringify(body),
      {
        "X-Fixture-Secret": config["FIXTURE_CONTROL_SECRET"],
        "Content-Type": "application/json",
      },
    );
    assert.ok(
      response.status >= 200 && response.status < 300,
      `Control ${method} failed (${response.status}).`,
    );
    const observedBoot = response.headers["x-fixture-boot-id"];
    assert.ok(
      typeof observedBoot === "string" && (!bootID || bootID === observedBoot),
      "Fixture restarted during the smoke.",
    );
    bootID = observedBoot as string;
    return response.body ? json(response.body) : undefined;
  }
  await control("presets");
  if (process.env["usage_health_only"] === "true") {
    console.log("Fixture HTTPS and secret-gated control are ready.");
    return;
  }
  const clientOrigin = config["FIXTURE_SMOKE_ORIGIN"];
  const callback = `${clientOrigin}/callback`;
  const clientID = `${clientOrigin}/client.json`;
  let metadataFetches = 0;
  let callbacks = 0;
  const client = createServer(
    {
      cert: readFileSync(config["FIXTURE_TLS_CERT_FILE"]),
      key: readFileSync(config["FIXTURE_TLS_KEY_FILE"]),
      minVersion: "TLSv1.2",
    },
    (req, res) => {
      if (req.url === "/client.json") {
        metadataFetches++;
        res.setHeader("Content-Type", "application/json");
        res.end(
          JSON.stringify({
            client_id: clientID,
            client_name: "Synthetic fixture smoke",
            redirect_uris: [callback],
            token_endpoint_auth_method: "none",
          }),
        );
      } else if (req.url?.startsWith("/callback?")) {
        callbacks++;
        res.end("Synthetic callback received.");
      } else {
        res.writeHead(404).end();
      }
    },
  );
  client.headersTimeout = deadlineMS;
  client.requestTimeout = deadlineMS;
  await new Promise<void>((accept, reject) => {
    client.once("error", () =>
      reject(new Error("Isolated smoke client port is busy.")),
    );
    client.listen(Number(config["FIXTURE_CLIENT_PORT"]), "127.0.0.1", accept);
  });
  const created: string[] = [];
  try {
    for (const registration of ["dcr", "cimd"]) {
      const id = `zz-local-smoke-${randomBytes(8).toString("hex")}`;
      const view = await control(`scenarios/${id}`, "PUT", {
        preset: `oauth-${registration}`,
        ttl_s: 120,
      });
      created.push(id);
      const issuer = view.urls.issuer as string;
      const resource = view.urls.default as string;
      assert.ok(
        issuer.startsWith(`${base}/s/${id}/`) &&
          resource.startsWith(`${base}/s/${id}/`),
        "Scenario URLs escaped their origin or isolation prefix.",
      );
      const metadata = await send(
        `${base}/.well-known/oauth-authorization-server${new URL(issuer).pathname}`,
      );
      assert.ok(
        metadata.status === 200 && json(metadata.body).issuer === issuer,
        "Canonical AS metadata failed.",
      );
      let client = clientID;
      if (registration === "dcr") {
        const registered = await send(
          `${issuer}/register`,
          "POST",
          JSON.stringify({
            client_name: "Synthetic fixture smoke",
            redirect_uris: [callback],
            token_endpoint_auth_method: "none",
          }),
          { "Content-Type": "application/json" },
        );
        assert.ok(registered.status === 201, "DCR failed.");
        client = json(registered.body).client_id;
      }
      const verifier = randomBytes(32).toString("base64url");
      const authorize = new URL(`${issuer}/authorize`);
      authorize.search = new URLSearchParams({
        response_type: "code",
        client_id: client,
        redirect_uri: callback,
        code_challenge_method: "S256",
        code_challenge: createHash("sha256")
          .update(verifier)
          .digest("base64url"),
        state: id,
        resource,
      }).toString();
      const authorized = await send(authorize.href);
      assert.ok(
        authorized.status === 302 &&
          typeof authorized.headers.location === "string",
        "Authorization failed (including fixture-to-client CIMD TLS verification).",
      );
      const location = new URL(authorized.headers.location!);
      assert.ok(
        location.origin === clientOrigin &&
          location.pathname === "/callback" &&
          location.searchParams.get("state") === id &&
          !!location.searchParams.get("code"),
        "Authorization returned an invalid callback.",
      );
      assert.ok(
        (await send(location.href)).status === 200,
        "Trusted HTTPS callback failed.",
      );
      const token = await send(
        `${issuer}/token`,
        "POST",
        new URLSearchParams({
          grant_type: "authorization_code",
          client_id: client,
          redirect_uri: callback,
          code_verifier: verifier,
          code: location.searchParams.get("code")!,
          resource,
        }).toString(),
        { "Content-Type": "application/x-www-form-urlencoded" },
      );
      assert.ok(token.status === 200, "PKCE code exchange failed.");
      const grant = json(token.body);
      assert.ok(
        typeof grant.access_token === "string" &&
          typeof grant.refresh_token === "string",
        "Expected access and refresh grants.",
      );
      const refreshed = await send(
        `${issuer}/token`,
        "POST",
        new URLSearchParams({
          grant_type: "refresh_token",
          client_id: client,
          refresh_token: grant.refresh_token,
          resource,
        }).toString(),
        { "Content-Type": "application/x-www-form-urlencoded" },
      );
      assert.ok(refreshed.status === 200, "Refresh failed.");
      const bearer = json(refreshed.body).access_token;
      async function rpc(method: string, params: object) {
        const response = await send(
          resource,
          "POST",
          JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
          {
            Authorization: `Bearer ${bearer}`,
            "Content-Type": "application/json",
            Accept: "application/json, text/event-stream",
            "MCP-Protocol-Version": "2025-03-26",
          },
        );
        assert.ok(response.status === 200, "Authenticated MCP request failed.");
        const parsed = json(response.body);
        assert.ok(
          !parsed.error && parsed.result,
          "MCP returned a protocol error.",
        );
        return parsed.result;
      }
      const tools = await rpc("tools/list", {});
      assert.ok(tools.tools.length > 0, "Scenario catalog is empty.");
      const result = await rpc("tools/call", {
        name: tools.tools[0].name,
        arguments: { message: "synthetic smoke", nested: { value: 7 } },
      });
      assert.ok(
        !result.isError &&
          result.structuredContent?.message === "synthetic smoke" &&
          result.structuredContent?.nested?.value === 7,
        "Authenticated echo changed arguments.",
      );
      const events = await control(`scenarios/${id}/events?after=0&limit=200`);
      assert.ok(
        events.events.some(
          (event: any) => event.point === "token:refresh_token",
        ) &&
          events.events.some(
            (event: any) =>
              event.point === "mcp:tools/call" &&
              event.mcp_method === "tools/call",
          ),
        "Missing refresh or MCP-call evidence.",
      );
      if (registration === "cimd")
        assert.ok(
          !events.events.some((event: any) => event.point === "register"),
          "CIMD unexpectedly registered a client.",
        );
    }
    assert.ok(
      metadataFetches > 0 && callbacks === 2,
      "CIMD fetch or HTTPS callbacks were not observed.",
    );
    console.log(
      "Passed trusted HTTPS, control rejection/isolation, DCR + CIMD PKCE callbacks, refresh, authenticated echo and upstream event checks.",
    );
  } finally {
    try {
      for (const id of created) await control(`scenarios/${id}`, "DELETE");
    } finally {
      await new Promise<void>((accept) => client.close(() => accept()));
    }
  }
}

smoke().catch(fail);
