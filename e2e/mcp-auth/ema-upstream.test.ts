import assert from "node:assert/strict";
import { createHash, generateKeyPairSync, randomUUID, sign } from "node:crypto";
import { createServer, request } from "node:https";
import { test } from "node:test";
import {
  EMA_GRANT,
  localTLS,
  startEMAUpstream,
  verifyEMAAssertion,
} from "./ema-upstream.ts";

test("EMA validates HTTPS JWKS, exact claims and client authentication before executing tools", async () => {
  const tls = await localTLS();
  const { privateKey, publicKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
  });
  const kid = randomUUID();
  let jwksReads = 0;
  const jwks = createServer(tls, (_req, res) => {
    jwksReads++;
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        keys: [
          {
            ...publicKey.export({ format: "jwk" }),
            kid,
            alg: "RS256",
            use: "sig",
          },
        ],
      }),
    );
  });
  await new Promise<void>((resolve) => jwks.listen(0, "127.0.0.1", resolve));
  const address = jwks.address();
  assert.ok(address && typeof address !== "string");
  const issuer = `https://localhost:${address.port}`;
  const upstream = await startEMAUpstream();
  const trust = {
    issuer,
    jwksURL: `${issuer}/jwks`,
    subject: "fixture:user",
    email: "fixture@example.test",
  };
  upstream.trust(trust);
  const post = (
    path: string,
    value: string,
    headers: Record<string, string> = {},
  ) =>
    new Promise<{ status: number; data: any }>((resolve, reject) => {
      const req = request(
        `${upstream.origin}${path}`,
        { method: "POST", ca: tls.ca, headers },
        (res) => {
          let result = "";
          res.on("data", (chunk) => {
            result += chunk;
          });
          res.on("end", () =>
            resolve({ status: res.statusCode!, data: JSON.parse(result) }),
          );
          res.on("error", reject);
        },
      );
      req.on("error", reject);
      req.end(value);
    });
  try {
    const registration = await post(
      "/register",
      JSON.stringify({ redirect_uris: ["http://127.0.0.1/callback"] }),
    );
    assert.equal(registration.status, 201);
    const clientID = registration.data.client_id;
    const target = {
      audience: upstream.origin,
      resource: upstream.resource,
      clientID,
      scope: upstream.scope,
    };
    const mint = (
      overrides: Record<string, unknown> = {},
      headerOverrides: Record<string, unknown> = {},
    ) => {
      const now = Math.floor(Date.now() / 1000);
      const encode = (value: unknown) =>
        Buffer.from(JSON.stringify(value)).toString("base64url");
      const input = `${encode({ alg: "RS256", typ: "oauth-id-jag+jwt", kid, ...headerOverrides })}.${encode({ iss: issuer, sub: trust.subject, email: trust.email, aud: target.audience, resource: target.resource, client_id: clientID, scope: target.scope, iat: now, exp: now + 120, jti: randomUUID(), ...overrides })}`;
      return `${input}.${sign("sha256", Buffer.from(input), privateKey).toString("base64url")}`;
    };
    const headers = {
      "Content-Type": "application/x-www-form-urlencoded",
      Authorization: `Basic ${Buffer.from(`${clientID}:${registration.data.client_secret}`).toString("base64")}`,
    };
    const exchange = (assertion: string, customHeaders = headers) =>
      post(
        "/token",
        new URLSearchParams({ grant_type: EMA_GRANT, assertion }).toString(),
        customHeaders,
      );
    const assertion = mint();
    const accepted = await exchange(assertion);
    assert.equal(accepted.status, 200);
    assert.ok(jwksReads > 0, "must actually fetch trusted HTTPS JWKS");
    const bearer = {
      Authorization: `Bearer ${accepted.data.access_token}`,
      "Content-Type": "application/json",
    };
    for (const method of ["initialize", "tools/list", "tools/call"]) {
      const result = await post(
        "/mcp",
        JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          method,
          params: {
            protocolVersion: "2025-03-26",
            name: "identity",
            arguments: {},
          },
        }),
        bearer,
      );
      assert.equal(result.status, 200);
      assert.ok(result.data.result);
    }
    assert.equal(upstream.journal.filter((e) => e.kind === "tool").length, 1);
    assert.equal(
      (await exchange(assertion)).status,
      400,
      "replayed assertion rejected",
    );
    for (const override of [
      { iss: `${issuer}/other` },
      { sub: "fixture:other" },
      { email: "other@example.test" },
      { aud: `${upstream.origin}/other` },
      { resource: `${upstream.resource}/` },
      { client_id: "other" },
      { scope: "fixture:write" },
      { scope: "fixture:read fixture:write" },
      { exp: 0 },
      { iat: Math.floor(Date.now() / 1000) + 60 },
      { jti: "" },
    ])
      assert.equal((await exchange(mint(override))).status, 400);
    assert.equal((await exchange(mint({}, { alg: "none" }))).status, 400);
    assert.equal((await exchange(mint({}, { kid: "unknown" }))).status, 400);
    const [h, p, s] = mint().split(".");
    const tampered = `${h}.${Buffer.from(JSON.stringify({ ...JSON.parse(Buffer.from(p!, "base64url").toString()), sub: "fixture:other" })).toString("base64url")}.${s}`;
    assert.equal((await exchange(tampered)).status, 400);
    assert.equal(
      (await exchange(mint(), { ...headers, Authorization: "Basic bm86bm8=" }))
        .status,
      401,
    );
    await assert.rejects(
      verifyEMAAssertion(mint(), trust, target, []),
      /certificate|issuer|verify/i,
    );
    await assert.rejects(
      verifyEMAAssertion(
        mint(),
        { ...trust, jwksURL: "http://localhost/jwks" },
        target,
        tls.ca,
      ),
      /HTTPS/,
    );
    // The personal alternative is a real, one-use PKCE code grant, not an
    // advertised endpoint that always errors or a pre-minted bearer token.
    const verifier = "v".repeat(43);
    const authorization = new URL(`${upstream.origin}/authorize`);
    authorization.search = new URLSearchParams({
      client_id: clientID,
      redirect_uri: "http://127.0.0.1/callback",
      response_type: "code",
      resource: upstream.resource,
      scope: upstream.scope,
      state: "fixture-state",
      code_challenge_method: "S256",
      code_challenge: createHash("sha256").update(verifier).digest("base64url"),
    }).toString();
    const redirect = await new Promise<string>((resolve, reject) => {
      const req = request(authorization, { ca: tls.ca }, (res) => {
        res.resume();
        assert.equal(res.statusCode, 302);
        resolve(res.headers.location!);
      });
      req.on("error", reject);
      req.end();
    });
    const callback = new URL(redirect);
    assert.equal(callback.searchParams.get("state"), "fixture-state");
    const codeForm = new URLSearchParams({
      grant_type: "authorization_code",
      code: callback.searchParams.get("code")!,
      redirect_uri: "http://127.0.0.1/callback",
      code_verifier: verifier,
      resource: upstream.resource,
    });
    const personal = await post("/token", codeForm.toString(), headers);
    assert.equal(personal.status, 200);
    assert.equal(
      (await post("/token", codeForm.toString(), headers)).status,
      400,
    );
    assert.equal(
      upstream.journal.filter((event) => event.kind === "oauth_code").length,
      1,
    );
    upstream.deny();
    assert.equal((await exchange(mint())).status, 400);
    assert.equal(
      (
        await post(
          "/mcp",
          JSON.stringify({
            jsonrpc: "2.0",
            id: 2,
            method: "tools/call",
            params: { name: "identity" },
          }),
          bearer,
        )
      ).status,
      401,
    );
    assert.equal(upstream.journal.filter((e) => e.kind === "tool").length, 1);
    const journal = JSON.stringify(upstream.journal);
    for (const secret of [
      assertion,
      accepted.data.access_token,
      registration.data.client_secret,
      trust.email,
      trust.subject,
    ])
      assert.ok(!journal.includes(secret));
  } finally {
    await upstream.close();
    await new Promise<void>((resolve, reject) => {
      jwks.close((error) => (error ? reject(error) : resolve()));
      jwks.closeAllConnections();
    });
  }
});
