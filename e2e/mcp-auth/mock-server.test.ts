import assert from "node:assert/strict";
import { createHash, randomBytes, createPublicKey, verify } from "node:crypto";
import { test } from "node:test";
// @ts-ignore Node executes TypeScript source directly; the e2e tsconfig does not enable TS import extensions.
import { startMockAuthServer } from "./mock-server.ts";

test("standalone OAuth and MCP fixtures", async (t) => {
  const server = await startMockAuthServer();
  t.after(() => server.close());
  const { alpha, beta } = server.providers;
  const resource = server.profiles.oauthAlpha;
  const redirect = "http://127.0.0.1:43210/callback";
  const post = (url: string, params: Record<string, string>) =>
    fetch(url, {
      method: "POST",
      body: new URLSearchParams(params),
      redirect: "manual",
    });
  const credentials = {
    client_id: alpha.clientId,
    client_secret: alpha.clientSecret,
  };
  const rpc = (
    url: string,
    headers: Record<string, string> = {},
    method = "tools/call",
    name = "fixture_echo",
  ) =>
    fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
        ...headers,
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method,
        params: { name, arguments: { hello: "world" } },
      }),
    });
  const authorize = async (target = resource, scope = "fixture:read") => {
    const verifier = randomBytes(32).toString("base64url");
    const params = {
      client_id: alpha.clientId,
      redirect_uri: redirect,
      resource: target,
      scope,
      state: "fixture-state",
      nonce: "fixture-nonce",
      response_type: "code",
      code_challenge_method: "S256",
      code_challenge: createHash("sha256").update(verifier).digest("base64url"),
    };
    const page = await fetch(
      `${alpha.issuer}/authorize?${new URLSearchParams(params)}`,
    );
    assert.match(await page.text(), /Approve mock access/);
    const response = await post(`${alpha.issuer}/authorize`, {
      ...params,
      decision: "approve",
    });
    assert.equal(response.status, 302);
    const callback = new URL(response.headers.get("location")!);
    assert.equal(callback.searchParams.get("state"), "fixture-state");
    return { code: callback.searchParams.get("code")!, verifier, params };
  };
  const exchange = (code: string, verifier: string, target = resource) =>
    post(`${alpha.issuer}/token`, {
      ...credentials,
      grant_type: "authorization_code",
      code,
      code_verifier: verifier,
      redirect_uri: redirect,
      resource: target,
    });
  const grant = async (scope?: string, target = resource) => {
    const { code, verifier } = await authorize(target, scope);
    const response = await exchange(code, verifier, target);
    assert.equal(response.status, 200);
    return response.json();
  };
  const bounded = async <T>(promise: Promise<T>): Promise<T> => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        promise,
        new Promise<never>((_, reject) => {
          timer = setTimeout(
            () => reject(new Error("Barrier timed out")),
            3000,
          );
        }),
      ]);
    } finally {
      clearTimeout(timer);
    }
  };
  await t.test(
    "token failures recover and journal only safe metadata",
    async () => {
      for (const provider of ["alpha", "beta"] as const) {
        const config = server.providers[provider];
        const target =
          provider === "alpha" ? resource : server.profiles.oauthBeta;
        const request = {
          client_id: config.clientId,
          client_secret: config.clientSecret,
          grant_type: "client_credentials",
          resource: target,
          scope: "fixture:read",
        };
        server.setTokenFailure(provider, {
          status: 503,
          body: "private-upstream-body",
        });
        const failed = await post(`${config.issuer}/token`, request);
        assert.equal(failed.status, 503);
        assert.equal(await failed.text(), "private-upstream-body");
        server.setTokenFailure(provider, {
          status: 400,
          error: "invalid_grant",
        });
        assert.deepEqual(
          await (await post(`${config.issuer}/token`, request)).json(),
          { error: "invalid_grant" },
        );
        await post(`${config.issuer}/token`, {
          ...request,
          resource: "https://secret.invalid/?token=private-target",
          scope: "private-scope",
        });
        const failures = server.events.filter(
          (event) =>
            event.kind === "token_failure" && event.provider === provider,
        );
        assert.deepEqual(
          failures.map((event) => event.outcome),
          ["503", "400", "400"],
        );
        assert.equal(failures[0]!.resource, target);
        assert.deepEqual(failures[0]!.scopes, ["fixture:read"]);
        server.setTokenFailure(provider, null);
        const recovered = await post(`${config.issuer}/token`, request);
        assert.equal(recovered.status, 200);
        const token = await recovered.json();
        const journal = JSON.stringify(server.events);
        for (const secret of [
          config.clientSecret,
          token.access_token,
          "private-upstream-body",
          "private-target",
          "private-scope",
        ])
          assert.ok(!journal.includes(secret));
      }
    },
  );
  await t.test(
    "zero advertised expiry does not expire actual access tokens",
    async () => {
      server.setAdvertisedLifetime(0);
      try {
        const tokens = await grant();
        assert.equal(tokens.expires_in, 0);
        assert.equal(
          (
            await rpc(resource, {
              Authorization: `Bearer ${tokens.access_token}`,
            })
          ).status,
          200,
        );
        const introspection = await (
          await post(`${alpha.issuer}/introspect`, {
            ...credentials,
            token: tokens.access_token,
          })
        ).json();
        assert.equal(introspection.active, true);
        assert.equal(introspection.aud, resource);
        assert.equal(introspection.sub, "alpha-user");
        assert.equal(introspection.scope, "fixture:read");
      } finally {
        server.setAdvertisedLifetime(null);
      }
      assert.equal((await grant()).expires_in, 3600);
    },
  );
  await t.test(
    "same URL can require OAuth and every configured scope",
    async () => {
      const target = server.profiles.noAuth;
      assert.equal((await rpc(target)).status, 200);
      server.setResourceAuth(target, true);
      server.setResourceScopes(target, ["fixture:read", "fixture:extra"]);
      try {
        assert.equal((await rpc(target)).status, 401);
        const metadata = await (
          await fetch(
            `${server.origin}/.well-known/oauth-protected-resource${new URL(target).pathname}`,
          )
        ).json();
        assert.deepEqual(metadata.scopes_supported, [
          "fixture:read",
          "fixture:extra",
          "openid",
        ]);
        assert.deepEqual(metadata.authorization_servers, [alpha.issuer]);
        const discovery = await (
          await fetch(`${alpha.issuer}/.well-known/oauth-authorization-server`)
        ).json();
        assert.ok(discovery.scopes_supported.includes("fixture:extra"));
        const partial = await grant("fixture:read", target);
        assert.equal(
          (
            await rpc(target, {
              Authorization: `Bearer ${partial.access_token}`,
            })
          ).status,
          403,
        );
        const full = await grant("fixture:read fixture:extra", target);
        assert.equal(
          (await rpc(target, { Authorization: `Bearer ${full.access_token}` }))
            .status,
          200,
        );
        assert.equal(
          (
            await rpc(resource, {
              Authorization: `Bearer ${full.access_token}`,
            })
          ).status,
          401,
        );
        const info = await (
          await post(`${alpha.issuer}/introspect`, {
            ...credentials,
            token: full.access_token,
          })
        ).json();
        assert.equal(info.aud, target);
        assert.equal(info.scope, "fixture:read fixture:extra");
        assert.equal(info.sub, "alpha-user");
        server.setResourceScopes(target, []);
        assert.equal((await rpc(target)).status, 401);
        assert.equal(
          (
            await rpc(target, {
              Authorization: `Bearer ${partial.access_token}`,
            })
          ).status,
          200,
        );
      } finally {
        server.setResourceAuth(target, false);
        server.setResourceScopes(target, ["fixture:read"]);
      }
      assert.equal((await rpc(target)).status, 200);
    },
  );
  await t.test(
    "refresh barrier pauses after consumption and releases or disconnects",
    async () => {
      for (const disconnect of [false, true]) {
        const tokens = await grant();
        const barrier = server.pauseNextRefresh("alpha");
        const controller = new AbortController();
        let settled = false;
        const pending = fetch(`${alpha.issuer}/token`, {
          method: "POST",
          signal: controller.signal,
          body: new URLSearchParams({
            ...credentials,
            grant_type: "refresh_token",
            refresh_token: tokens.refresh_token,
          }),
        }).then(
          (response) => {
            settled = true;
            return response;
          },
          () => {
            settled = true;
            return null;
          },
        );
        try {
          await bounded(barrier.started);
          assert.equal(settled, false);
          const consumed = await (
            await post(`${alpha.issuer}/introspect`, {
              ...credentials,
              token: tokens.refresh_token,
            })
          ).json();
          assert.equal(consumed.active, false);
          if (disconnect) controller.abort();
          else barrier.release();
          const response = await bounded(pending);
          if (disconnect) assert.equal(response, null);
          else {
            assert.equal(response!.status, 200);
            const rotated = await response!.json();
            assert.notEqual(rotated.refresh_token, tokens.refresh_token);
            assert.equal(
              (
                await rpc(resource, {
                  Authorization: `Bearer ${rotated.access_token}`,
                })
              ).status,
              200,
            );
          }
        } finally {
          barrier.release();
          controller.abort();
        }
      }
    },
  );
  await t.test(
    "discovery advertises precise issuers and protected resources",
    async () => {
      const denied = await rpc(resource);
      assert.equal(denied.status, 401);
      const metadataUrl = denied.headers
        .get("www-authenticate")!
        .match(/resource_metadata="([^"]+)"/)![1]!;
      const metadata = await (await fetch(metadataUrl)).json();
      assert.equal(metadata.resource, resource);
      assert.deepEqual(metadata.authorization_servers, [alpha.issuer]);
      for (const provider of [alpha, beta]) {
        const discovery = await (
          await fetch(
            `${server.origin}/.well-known/oauth-authorization-server/${provider === alpha ? "alpha" : "beta"}`,
          )
        ).json();
        assert.equal(discovery.issuer, provider.issuer);
        assert.equal(discovery.token_endpoint, `${provider.issuer}/token`);
      }
    },
  );
  await t.test(
    "rejected authorization targets remain visible without secrets",
    async () => {
      const { params } = await authorize();
      for (const target of [
        undefined,
        server.profiles.oauthBeta,
        "https://example.test/resource?credential=private-query-secret",
      ]) {
        const request = new URLSearchParams({
          ...params,
          scope: "fixture:read openid",
        });
        if (target === undefined) request.delete("resource");
        else request.set("resource", target);
        const offset = server.events.length;
        const response = await fetch(`${alpha.issuer}/authorize?${request}`, {
          redirect: "manual",
        });
        assert.equal(response.status, 302);
        const callback = new URL(response.headers.get("location")!);
        assert.equal(callback.searchParams.get("error"), "invalid_target");
        assert.equal(callback.searchParams.has("code"), false);
        assert.deepEqual(server.events.slice(offset), [
          {
            kind: "authorize",
            provider: "alpha",
            clientId: alpha.clientId,
            resource: target === server.profiles.oauthBeta ? target : "",
            scopes: ["fixture:read", "openid"],
            outcome: "invalid_target",
          },
        ]);
        assert.doesNotMatch(
          JSON.stringify(server.events),
          /private-query-secret/,
        );
      }
    },
  );
  await t.test("PKCE, code single-use, denial and state", async () => {
    const { code, verifier, params } = await authorize();
    assert.equal((await exchange(code, "x".repeat(43))).status, 400);
    assert.equal((await exchange(code, verifier)).status, 400);
    const deny = await post(`${alpha.issuer}/authorize`, {
      ...params,
      decision: "deny",
    });
    const callback = new URL(deny.headers.get("location")!);
    assert.equal(callback.searchParams.get("error"), "access_denied");
    assert.equal(callback.searchParams.get("state"), "fixture-state");
  });
  await t.test(
    "tokens are provider/resource bound; MCP routes tools",
    async () => {
      const tokens = await grant();
      assert.equal(tokens.id_token, undefined);
      const headers = { Authorization: `Bearer ${tokens.access_token}` };
      const offset = server.events.length;
      assert.equal((await rpc(server.profiles.oauthBeta, headers)).status, 401);
      assert.equal((await rpc(server.profiles.multiple, headers)).status, 401);
      assert.deepEqual(
        server.events
          .slice(offset)
          .map(({ resource, method, tool, outcome }) => ({
            resource,
            method,
            tool,
            outcome,
          })),
        [server.profiles.oauthBeta, server.profiles.multiple].map(
          (resource) => ({
            resource,
            method: "tools/call",
            tool: "fixture_echo",
            outcome: "unauthorized",
          }),
        ),
        "Rejected cross-member tool calls must remain visible even if a retry succeeds",
      );
      const initialize = await (
        await rpc(resource, headers, "initialize")
      ).json();
      assert.equal(initialize.result.serverInfo.name, "mock-auth-upstream");
      const tools = await (await rpc(resource, headers, "tools/list")).json();
      assert.equal(tools.result.tools[0].name, "fixture_echo");
      const call = await (await rpc(resource, headers)).json();
      const content = JSON.parse(call.result.content[0].text);
      assert.equal(content.principal, "alpha-user");
      assert.deepEqual(content.arguments, { hello: "world" });
      const unknown = await (
        await rpc(resource, headers, "tools/call", "missing")
      ).json();
      assert.equal(unknown.error.code, -32602);
      assert.equal(
        (
          await post(`${beta.issuer}/token`, {
            client_id: beta.clientId,
            client_secret: beta.clientSecret,
            grant_type: "refresh_token",
            refresh_token: tokens.refresh_token,
          })
        ).status,
        400,
      );
    },
  );
  await t.test(
    "protected resource scopes override issuer defaults",
    async () => {
      const scoped = server.profiles.oauthScoped;
      const discovery = await (
        await fetch(`${alpha.issuer}/.well-known/oauth-authorization-server`)
      ).json();
      assert.deepEqual(discovery.scopes_supported, ["fixture:read", "openid"]);
      const metadata = await (
        await fetch(
          `${server.origin}/.well-known/oauth-protected-resource/mcp/oauth-scoped`,
        )
      ).json();
      assert.equal(metadata.resource, scoped);
      assert.deepEqual(metadata.authorization_servers, [alpha.issuer]);
      assert.deepEqual(metadata.scopes_supported, ["fixture:scoped", "openid"]);

      for (const scope of ["", "fixture:read", "openid"]) {
        const tokens = await grant(scope, scoped);
        const offset = server.events.length;
        const denied = await rpc(scoped, {
          Authorization: `Bearer ${tokens.access_token}`,
        });
        assert.equal(denied.status, 403);
        assert.deepEqual(await denied.json(), { error: "insufficient_scope" });
        assert.match(
          denied.headers.get("www-authenticate")!,
          /error="insufficient_scope"/,
        );
        assert.match(
          denied.headers.get("www-authenticate")!,
          /scope="fixture:scoped"/,
        );
        const journal = server.events.slice(offset);
        assert.deepEqual(
          journal.map(({ method, tool, resource, outcome }) => ({
            method,
            tool,
            resource,
            outcome,
          })),
          [
            {
              method: "tools/call",
              tool: "fixture_echo",
              resource: scoped,
              outcome: "insufficient_scope",
            },
          ],
        );
        assert.ok(!JSON.stringify(journal).includes(tokens.access_token));
        assert.ok(!JSON.stringify(journal).includes(tokens.refresh_token));
      }

      const tokens = await grant("fixture:scoped openid", scoped);
      const headers = { Authorization: `Bearer ${tokens.access_token}` };
      assert.equal((await rpc(scoped, headers)).status, 200);
      assert.equal((await rpc(resource, headers)).status, 401);
      const wrongResource = await grant("fixture:scoped");
      assert.equal(
        (
          await rpc(scoped, {
            Authorization: `Bearer ${wrongResource.access_token}`,
          })
        ).status,
        401,
      );
      const missingRead = await grant("");
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${missingRead.access_token}`,
          })
        ).status,
        403,
      );

      server.expireTokens();
      assert.equal((await rpc(scoped, headers)).status, 401);
      const response = await post(`${alpha.issuer}/token`, {
        ...credentials,
        grant_type: "refresh_token",
        refresh_token: tokens.refresh_token,
        resource: scoped,
      });
      assert.equal(response.status, 200);
      const rotated = await response.json();
      assert.equal(rotated.scope, "fixture:scoped openid");
      assert.deepEqual(server.events.at(-1), {
        kind: "refresh",
        provider: "alpha",
        clientId: alpha.clientId,
        resource: scoped,
        scopes: ["fixture:scoped", "openid"],
        outcome: "success",
      });
      assert.equal(
        (
          await rpc(scoped, {
            Authorization: `Bearer ${rotated.access_token}`,
          })
        ).status,
        200,
      );
    },
  );
  await t.test(
    "DCR public clients use exact callbacks and cannot use another client's tokens",
    async () => {
      const registration = await fetch(`${alpha.issuer}/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirect_uris: [redirect],
          token_endpoint_auth_method: "none",
        }),
      });
      assert.equal(registration.status, 201);
      const client = await registration.json();
      assert.equal(client.client_secret, undefined);
      const { params, verifier } = await authorize();
      const wrongCallback = await post(`${alpha.issuer}/authorize`, {
        ...params,
        client_id: client.client_id,
        redirect_uri: `${redirect}/wrong`,
        decision: "approve",
      });
      assert.equal(wrongCallback.status, 400);
      const approval = await post(`${alpha.issuer}/authorize`, {
        ...params,
        client_id: client.client_id,
        decision: "approve",
      });
      const code = new URL(approval.headers.get("location")!).searchParams.get(
        "code",
      )!;
      const response = await post(`${alpha.issuer}/token`, {
        client_id: client.client_id,
        grant_type: "authorization_code",
        code,
        code_verifier: verifier,
        redirect_uri: redirect,
        resource,
      });
      assert.equal(response.status, 200);
      const tokens = await response.json();
      assert.equal(
        (
          await post(`${alpha.issuer}/token`, {
            ...credentials,
            grant_type: "refresh_token",
            refresh_token: tokens.refresh_token,
          })
        ).status,
        400,
      );
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${tokens.access_token}`,
          })
        ).status,
        200,
      );
      server.revokeTokens();
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${tokens.access_token}`,
          })
        ).status,
        401,
      );
    },
  );
  await t.test("OIDC ID tokens are signed only for openid grants", async () => {
    const tokens = await grant("openid fixture:read");
    const [header, payload, signature] = tokens.id_token.split(".");
    const claims = JSON.parse(Buffer.from(payload, "base64url").toString());
    assert.equal(claims.iss, alpha.issuer);
    assert.equal(claims.aud, alpha.clientId);
    assert.equal(claims.nonce, "fixture-nonce");
    const keys = await (await fetch(`${alpha.issuer}/jwks`)).json();
    assert.ok(
      verify(
        "RSA-SHA256",
        Buffer.from(`${header}.${payload}`),
        createPublicKey({ key: keys.keys[0], format: "jwk" }),
        Buffer.from(signature, "base64url"),
      ),
    );
  });
  await t.test(
    "refresh rotation, reuse detection, expiry, introspection and revocation",
    async () => {
      const tokens = await grant();
      server.expireTokens();
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${tokens.access_token}`,
          })
        ).status,
        401,
      );
      const refreshParams = {
        ...credentials,
        grant_type: "refresh_token",
        refresh_token: tokens.refresh_token,
        resource,
      };
      const response = await post(`${alpha.issuer}/token`, refreshParams);
      assert.equal(response.status, 200);
      const rotated = await response.json();
      assert.notEqual(rotated.refresh_token, tokens.refresh_token);
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${rotated.access_token}`,
          })
        ).status,
        200,
      );
      assert.equal(
        (await post(`${alpha.issuer}/token`, refreshParams)).status,
        400,
      );
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${rotated.access_token}`,
          })
        ).status,
        401,
      );
      const fresh = await grant();
      const introspect = () =>
        post(`${alpha.issuer}/introspect`, {
          ...credentials,
          token: fresh.access_token,
        }).then((r) => r.json());
      assert.equal((await introspect()).active, true);
      assert.equal(
        (
          await post(`${alpha.issuer}/revoke`, {
            ...credentials,
            token: fresh.refresh_token,
          })
        ).status,
        200,
      );
      assert.equal((await introspect()).active, false);
    },
  );
  await t.test(
    "dual-provider resource distinguishes issuers without weakening resource or channel binding",
    async () => {
      const target = server.profiles.dualProvider;
      const metadata = await (
        await fetch(
          `${server.origin}/.well-known/oauth-protected-resource/mcp/dual-provider`,
        )
      ).json();
      assert.deepEqual(metadata.authorization_servers, [
        alpha.issuer,
        beta.issuer,
      ]);
      const alphaTokens = await grant("fixture:read", target);
      const betaResponse = await post(`${beta.issuer}/token`, {
        client_id: beta.clientId,
        client_secret: beta.clientSecret,
        grant_type: "client_credentials",
        resource: target,
        scope: "fixture:read",
      });
      assert.equal(betaResponse.status, 200);
      const betaTokens = await betaResponse.json();
      for (const [token, principal] of [
        [alphaTokens.access_token, "alpha-user"],
        [betaTokens.access_token, "beta-service"],
      ]) {
        const response = await rpc(target, {
          Authorization: `Bearer ${token}`,
        });
        assert.equal(response.status, 200);
        assert.equal(
          JSON.parse((await response.json()).result.content[0].text).principal,
          principal,
        );
      }
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${alphaTokens.access_token}`,
          })
        ).status,
        401,
      );
      assert.equal(
        (
          await rpc(target, {
            Authorization: `Bearer ${alphaTokens.access_token}`,
            "X-Fixture-Key": "fixture-static-secret",
          })
        ).status,
        400,
      );
    },
  );
  await t.test(
    "client credentials stay provider/resource bound and do not issue refresh tokens",
    async () => {
      const target = server.profiles.serviceAccount;
      const response = await post(`${alpha.issuer}/token`, {
        ...credentials,
        grant_type: "client_credentials",
        resource: target,
        scope: "fixture:read",
      });
      assert.equal(response.status, 200);
      const tokens = await response.json();
      assert.equal(tokens.refresh_token, undefined);
      assert.equal(tokens.id_token, undefined);
      const call = await rpc(target, {
        Authorization: `Bearer ${tokens.access_token}`,
      });
      assert.equal(call.status, 200);
      assert.equal(
        JSON.parse((await call.json()).result.content[0].text).principal,
        "alpha-service",
      );
      assert.equal(
        (
          await rpc(server.profiles.multiMethod, {
            Authorization: `Bearer ${tokens.access_token}`,
          })
        ).status,
        401,
      );
      assert.equal(
        (
          await post(`${alpha.issuer}/token`, {
            ...credentials,
            grant_type: "client_credentials",
            resource: server.profiles.oauthBeta,
          })
        ).status,
        400,
      );
      assert.equal(
        (
          await post(`${beta.issuer}/token`, {
            ...credentials,
            grant_type: "client_credentials",
            resource: target,
          })
        ).status,
        401,
      );
    },
  );
  await t.test(
    "multi-method resource distinguishes selected identity and rejects mixed credentials",
    async () => {
      const target = server.profiles.multiMethod;
      const tokens = await grant("fixture:read", target);
      const choices: [Record<string, string>, string][] = [
        [{ Authorization: `Bearer ${tokens.access_token}` }, "alpha-user"],
        [{ "X-Fixture-Key": "fixture-static-secret" }, "static-user"],
        [{ "X-Fixture-Key": "fixture-static-alt-secret" }, "static-alt-user"],
        [{ "X-Fixture-User": "fixture-user-secret" }, "pass-through-user"],
      ];
      for (const [headers, principal] of choices) {
        const response = await rpc(target, headers);
        assert.equal(response.status, 200);
        assert.equal(
          JSON.parse((await response.json()).result.content[0].text).principal,
          principal,
        );
      }
      const offset = server.events.length;
      for (const headers of [
        {
          Authorization: `Bearer ${tokens.access_token}`,
          "X-Fixture-Key": "fixture-static-secret",
        },
        {
          "X-Fixture-Key": "fixture-static-secret",
          "X-Fixture-User": "fixture-user-secret",
        },
      ] as Record<string, string>[])
        assert.equal((await rpc(target, headers)).status, 400);
      assert.deepEqual(
        server.events
          .slice(offset)
          .map(({ method, outcome }) => ({ method, outcome })),
        [
          { method: "tools/call", outcome: "mixed_credentials" },
          { method: "tools/call", outcome: "mixed_credentials" },
        ],
      );
      assert.ok(
        !JSON.stringify(server.events).includes("fixture-static-alt-secret"),
      );
    },
  );
  await t.test(
    "reset cancels blocked refresh without recreating cleared grants",
    async () => {
      const tokens = await grant();
      const barrier = server.pauseNextRefresh("alpha");
      const pending = post(`${alpha.issuer}/token`, {
        ...credentials,
        grant_type: "refresh_token",
        refresh_token: tokens.refresh_token,
      });
      await bounded(barrier.started);
      server.reset();
      assert.equal((await bounded(pending)).status, 400);
      barrier.release();
      assert.equal(server.events.length, 0);
      assert.equal(
        (
          await rpc(resource, {
            Authorization: `Bearer ${tokens.access_token}`,
          })
        ).status,
        401,
      );
    },
  );
  await t.test(
    "static, pass-through, multiple and anonymous identities stay distinct",
    async () => {
      for (const [url, headers, expected] of [
        [
          server.profiles.static,
          { "X-Fixture-Key": "fixture-static-secret" },
          "static-user",
        ],
        [
          server.profiles.multiple,
          { "X-Fixture-Key": "fixture-static-secret" },
          "static-user",
        ],
        [
          server.profiles.passThrough,
          { "X-Fixture-User": "fixture-user-secret" },
          "pass-through-user",
        ],
        [server.profiles.noAuth, {}, "anonymous"],
      ] as [string, Record<string, string>, string][]) {
        const response = await rpc(
          url,
          headers,
          "tools/call",
          "fixture_identity",
        );
        assert.equal(response.status, 200);
        assert.equal(
          JSON.parse((await response.json()).result.content[0].text).principal,
          expected,
        );
      }
      assert.equal(
        (await rpc(server.profiles.static, { "X-Fixture-Key": "wrong" }))
          .status,
        401,
      );
      const journal = JSON.stringify(server.events);
      assert.ok(!journal.includes("fixture-static-secret"));
      assert.ok(!journal.includes("fixture-user-secret"));
      assert.ok(!journal.includes(alpha.clientSecret));
      server.reset();
      assert.equal(server.events.length, 0);
    },
  );
});

test(
  "close cancels blocked refresh responses and releases barriers",
  { timeout: 5000 },
  async () => {
    const server = await startMockAuthServer();
    let closed = false;
    const { alpha } = server.providers;
    const credentials = {
      client_id: alpha.clientId,
      client_secret: alpha.clientSecret,
    };
    const verifier = randomBytes(32).toString("base64url");
    const redirect = "http://127.0.0.1:43210/callback";
    const post = (endpoint: string, params: Record<string, string>) =>
      fetch(`${alpha.issuer}/${endpoint}`, {
        method: "POST",
        redirect: "manual",
        body: new URLSearchParams(params),
      });
    try {
      const authorized = await post("authorize", {
        client_id: alpha.clientId,
        redirect_uri: redirect,
        resource: server.profiles.oauthAlpha,
        scope: "fixture:read",
        response_type: "code",
        code_challenge_method: "S256",
        code_challenge: createHash("sha256")
          .update(verifier)
          .digest("base64url"),
        decision: "approve",
      });
      const code = new URL(
        authorized.headers.get("location")!,
      ).searchParams.get("code")!;
      const tokens = await (
        await post("token", {
          ...credentials,
          grant_type: "authorization_code",
          code,
          code_verifier: verifier,
          redirect_uri: redirect,
        })
      ).json();
      const barrier = server.pauseNextRefresh("alpha");
      // A queued barrier for the other provider must not hold close open either.
      server.pauseNextRefresh("beta");
      const pending = post("token", {
        ...credentials,
        grant_type: "refresh_token",
        refresh_token: tokens.refresh_token,
      }).then(
        () => "response",
        () => "cancelled",
      );
      let timer: ReturnType<typeof setTimeout> | undefined;
      try {
        await Promise.race([
          barrier.started,
          new Promise<never>((_, reject) => {
            timer = setTimeout(
              () => reject(new Error("Refresh never started")),
              2000,
            );
          }),
        ]);
      } finally {
        clearTimeout(timer);
      }
      await server.close();
      closed = true;
      assert.equal(await pending, "cancelled");
      barrier.release();
    } finally {
      if (!closed) await server.close();
    }
  },
);

test("dynamic registrations enforce their declared token authentication and reset removes them", async () => {
  const server = await startMockAuthServer();
  try {
    const issuer = server.providers.alpha.issuer;
    const registrations: {
      client_id: string;
      client_secret?: string;
      token_endpoint_auth_method: string;
    }[] = [];
    for (const method of [
      "none",
      "client_secret_post",
      "client_secret_basic",
    ]) {
      const response = await fetch(`${issuer}/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirect_uris: ["http://127.0.0.1/callback"],
          token_endpoint_auth_method: method,
        }),
      });
      assert.equal(response.status, 201);
      const registration = await response.json();
      registrations.push(registration);
      for (const supplied of [
        "none",
        "client_secret_post",
        "client_secret_basic",
      ]) {
        const params = new URLSearchParams({
          grant_type: "unsupported_fixture_grant",
          client_id: registration.client_id,
        });
        const headers: Record<string, string> = {};
        if (supplied === "client_secret_post")
          params.set("client_secret", registration.client_secret ?? "");
        if (supplied === "client_secret_basic")
          headers.Authorization = `Basic ${Buffer.from(`${registration.client_id}:${registration.client_secret ?? ""}`).toString("base64")}`;
        const result = await fetch(`${issuer}/token`, {
          method: "POST",
          body: params,
          headers,
        });
        assert.equal(
          result.status,
          supplied === method ? 400 : 401,
          `${method} via ${supplied}`,
        );
        assert.equal(
          (await result.json()).error,
          supplied === method ? "unsupported_grant_type" : "invalid_client",
        );
      }
    }
    server.setTokenFailure("alpha", { status: 503 });
    server.setAdvertisedLifetime(1);
    server.setResourceAuth(server.profiles.noAuth, true);
    server.setResourceScopes(server.profiles.oauthAlpha, ["fixture:write"]);
    server.reset();
    assert.equal(server.events.length, 0);
    for (const registration of registrations) {
      const params = new URLSearchParams({
        client_id: registration.client_id,
        grant_type: "unsupported_fixture_grant",
      });
      const headers: Record<string, string> = {};
      if (registration.token_endpoint_auth_method === "client_secret_post")
        params.set("client_secret", registration.client_secret!);
      if (registration.token_endpoint_auth_method === "client_secret_basic")
        headers.Authorization = `Basic ${Buffer.from(`${registration.client_id}:${registration.client_secret}`).toString("base64")}`;
      assert.equal(
        (
          await fetch(`${issuer}/token`, {
            method: "POST",
            body: params,
            headers,
          })
        ).status,
        401,
      );
    }
    assert.equal(
      (
        await fetch(`${issuer}/token`, {
          method: "POST",
          body: new URLSearchParams({
            client_id: server.providers.alpha.clientId,
            client_secret: server.providers.alpha.clientSecret,
            grant_type: "unsupported_fixture_grant",
          }),
        })
      ).status,
      400,
    );
    const metadata = await (
      await fetch(
        server.profiles.oauthAlpha.replace(
          "/mcp/",
          "/.well-known/oauth-protected-resource/mcp/",
        ),
      )
    ).json();
    assert.deepEqual(metadata.scopes_supported, ["fixture:read", "openid"]);
    assert.equal(
      (
        await fetch(server.profiles.noAuth, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }),
        })
      ).status,
      200,
    );
  } finally {
    await server.close();
  }
});
