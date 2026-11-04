import {
  createHash,
  createPublicKey,
  randomUUID,
  verify,
  type JsonWebKey,
} from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { createServer, get } from "node:https";
import type { IncomingMessage } from "node:http";
import { resolve } from "node:path";
import { rootCertificates } from "node:tls";

export const EMA_GRANT = "urn:ietf:params:oauth:grant-type:jwt-bearer";
export type EMATrust = {
  issuer: string;
  jwksURL: string;
  subject: string;
  email: string;
};
export type EMAEvent = {
  kind: "register" | "exchange" | "oauth_code" | "tool" | "deny";
  principal?: string;
  issuer?: string;
  resource?: string;
  scope?: string;
  reason?: string;
};
const alias = (value: string) =>
  createHash("sha256").update(value).digest("hex").slice(0, 16);

/** No redirect following: the test pins the trusted HTTPS JWKS location. */
export async function readTrustedJSON(
  url: string,
  ca: string[],
): Promise<Record<string, unknown>> {
  if (new URL(url).protocol !== "https:")
    throw new Error("JWKS requires HTTPS");
  return new Promise((resolve, reject) => {
    const request = get(url, { ca, timeout: 5_000 }, (response) => {
      let body = "";
      response.on("data", (chunk) => {
        body += chunk;
        if (body.length > 128 * 1024)
          response.destroy(new Error("Oversized JWKS"));
      });
      response.on("error", reject);
      response.on("end", () => {
        try {
          if (response.statusCode !== 200) throw new Error("JWKS fetch failed");
          resolve(JSON.parse(body));
        } catch (error) {
          reject(error);
        }
      });
    });
    request.on("timeout", () => request.destroy(new Error("JWKS timeout")));
    request.on("error", reject);
  });
}

export async function localTLS() {
  const root = resolve(import.meta.dirname, "../..");
  const caRoot =
    process.env.CAROOT ||
    execFileSync("mkcert", ["-CAROOT"], { encoding: "utf8" }).trim();
  const [cert, key, ca] = await Promise.all([
    readFile(
      process.env.GRAM_SSL_CERT_FILE ||
        resolve(root, "local/ssl/certs/local.pem"),
    ),
    readFile(
      process.env.GRAM_SSL_KEY_FILE ||
        resolve(root, "local/ssl/keys/local-key.pem"),
    ),
    readFile(resolve(caRoot, "rootCA.pem"), "utf8"),
  ]);
  return { cert, key, ca: [...rootCertificates, ca] };
}

/** Validate signatures before reading identity; never trust assertion-controlled jku/x5u. */
export async function verifyEMAAssertion(
  assertion: string,
  trust: EMATrust,
  target: {
    audience: string;
    resource: string;
    clientID: string;
    scope: string;
  },
  ca: string[],
) {
  const parts = assertion.split(".");
  if (parts.length !== 3) throw new Error("malformed_assertion");
  const [headerPart, bodyPart, signature] = parts as [string, string, string];
  const header = JSON.parse(Buffer.from(headerPart, "base64url").toString());
  if (
    header.typ !== "oauth-id-jag+jwt" ||
    !["RS256", "ES256"].includes(header.alg) ||
    typeof header.kid !== "string"
  )
    throw new Error("invalid_header");
  const jwks = await readTrustedJSON(trust.jwksURL, ca);
  if (!Array.isArray(jwks.keys)) throw new Error("invalid_jwks");
  const keys = jwks.keys.filter(
    (key: JsonWebKey) =>
      key.kid === header.kid &&
      (!key.alg || key.alg === header.alg) &&
      (!key.use || key.use === "sig"),
  );
  if (keys.length !== 1) throw new Error("unknown_key");
  const jwk = keys[0];
  if (
    (header.alg === "RS256" && jwk.kty !== "RSA") ||
    (header.alg === "ES256" && (jwk.kty !== "EC" || jwk.crv !== "P-256"))
  )
    throw new Error("invalid_key_type");
  const valid = verify(
    "sha256",
    Buffer.from(`${headerPart}.${bodyPart}`),
    {
      key: createPublicKey({ key: jwk, format: "jwk" }),
      dsaEncoding: "ieee-p1363",
    },
    Buffer.from(signature, "base64url"),
  );
  if (!valid) throw new Error("invalid_signature");
  const claims = JSON.parse(Buffer.from(bodyPart, "base64url").toString());
  const now = Math.floor(Date.now() / 1000);
  if (
    !Number.isInteger(claims.exp) ||
    !Number.isInteger(claims.iat) ||
    claims.exp <= now ||
    claims.exp <= claims.iat ||
    claims.iat > now + 5 ||
    claims.exp - claims.iat > 300 ||
    (claims.nbf !== undefined &&
      (!Number.isInteger(claims.nbf) || claims.nbf > now + 5))
  )
    throw new Error("invalid_lifetime");
  for (const [key, value] of Object.entries({
    iss: trust.issuer,
    sub: trust.subject,
    email: trust.email,
    aud: target.audience,
    resource: target.resource,
    client_id: target.clientID,
    scope: target.scope,
  })) {
    if (claims[key] !== value) throw new Error(`invalid_${key}`);
  }
  if (typeof claims.jti !== "string" || !claims.jti)
    throw new Error("invalid_jti");
  return {
    jti: claims.jti as string,
    principal: alias(claims.sub),
    issuer: alias(claims.iss),
  };
}

async function body(request: IncomingMessage) {
  let result = "";
  for await (const chunk of request) {
    result += chunk;
    if (result.length > 64 * 1024) throw new Error("oversized_request");
  }
  return result;
}

/** Independent HTTPS resource AS and MCP server; not a management API fixture. */
export async function startEMAUpstream() {
  const tls = await localTLS();
  const journal: EMAEvent[] = [];
  const clients = new Map<string, string>();
  const redirects = new Map<string, string[]>();
  const codes = new Map<
    string,
    { clientID: string; redirect: string; challenge: string; expiresAt: number }
  >();
  const tokens = new Map<
    string,
    { principal: string; issuer: string; expiresAt: number }
  >();
  const seen = new Set<string>();
  let trust: EMATrust | undefined;
  let origin = "";
  let permitted = true;
  const scope = "fixture:read";
  const server = createServer(tls, (request, response) => {
    void (async () => {
      response.setHeader("Content-Type", "application/json");
      response.setHeader("Cache-Control", "no-store");
      const send = (status: number, data: unknown) =>
        response.writeHead(status).end(JSON.stringify(data));
      const path = new URL(request.url || "/", origin).pathname;
      const resource = `${origin}/mcp`;
      if (
        request.method === "GET" &&
        path === "/.well-known/oauth-protected-resource/mcp"
      ) {
        send(200, {
          resource,
          authorization_servers: [origin],
          scopes_supported: [scope],
          bearer_methods_supported: ["header"],
        });
        return;
      }
      if (
        request.method === "GET" &&
        [
          "/.well-known/oauth-authorization-server",
          "/.well-known/openid-configuration",
        ].includes(path)
      ) {
        send(200, {
          issuer: origin,
          authorization_endpoint: `${origin}/authorize`,
          token_endpoint: `${origin}/token`,
          registration_endpoint: `${origin}/register`,
          response_types_supported: ["code"],
          grant_types_supported: ["authorization_code", EMA_GRANT],
          token_endpoint_auth_methods_supported: ["client_secret_basic"],
          code_challenge_methods_supported: ["S256"],
          scopes_supported: [scope],
        });
        return;
      }
      if (request.method === "POST" && path === "/register") {
        const registration = JSON.parse(await body(request));
        const clientID = `ema-${randomUUID()}`;
        const secret = randomUUID();
        if (
          !Array.isArray(registration.redirect_uris) ||
          !registration.redirect_uris.every(
            (uri: unknown) => typeof uri === "string",
          )
        ) {
          send(400, { error: "invalid_redirect_uri" });
          return;
        }
        clients.set(clientID, secret);
        redirects.set(clientID, registration.redirect_uris);
        journal.push({ kind: "register" });
        send(201, {
          ...registration,
          client_id: clientID,
          client_secret: secret,
          token_endpoint_auth_method: "client_secret_basic",
          grant_types: ["authorization_code", EMA_GRANT],
        });
        return;
      }
      if (request.method === "GET" && path === "/authorize") {
        const params = new URL(request.url!, origin).searchParams;
        const clientID = params.get("client_id") || "";
        const redirect = params.get("redirect_uri") || "";
        const challenge = params.get("code_challenge") || "";
        if (
          !trust ||
          !permitted ||
          !redirects.get(clientID)?.includes(redirect) ||
          params.get("response_type") !== "code" ||
          params.get("resource") !== resource ||
          params.get("scope") !== scope ||
          params.get("code_challenge_method") !== "S256" ||
          !/^[A-Za-z0-9_-]{43}$/.test(challenge) ||
          !params.get("state")
        ) {
          journal.push({
            kind: "deny",
            reason: "invalid_authorization_request",
          });
          send(400, { error: "invalid_request" });
          return;
        }
        const code = randomUUID();
        codes.set(code, {
          clientID,
          redirect,
          challenge,
          expiresAt: Date.now() + 60_000,
        });
        const callback = new URL(redirect);
        callback.searchParams.set("code", code);
        callback.searchParams.set("state", params.get("state")!);
        response.writeHead(302, { Location: callback.toString() }).end();
        return;
      }
      if (request.method === "POST" && path === "/token") {
        const form = new URLSearchParams(await body(request));
        const basic = request.headers.authorization?.startsWith("Basic ")
          ? Buffer.from(
              request.headers.authorization.slice(6),
              "base64",
            ).toString()
          : "";
        const separator = basic.indexOf(":");
        const clientID = decodeURIComponent(basic.slice(0, separator));
        const secret = decodeURIComponent(basic.slice(separator + 1));
        if (
          !trust ||
          separator < 0 ||
          !clients.has(clientID) ||
          clients.get(clientID) !== secret
        ) {
          journal.push({ kind: "deny", reason: "invalid_client" });
          send(401, { error: "invalid_client" });
          return;
        }
        if (form.get("grant_type") === "authorization_code") {
          const code = form.get("code") || "";
          const grant = codes.get(code);
          codes.delete(code);
          const verifier = form.get("code_verifier") || "";
          if (
            !permitted ||
            !grant ||
            grant.expiresAt <= Date.now() ||
            grant.clientID !== clientID ||
            grant.redirect !== form.get("redirect_uri") ||
            form.get("resource") !== resource ||
            !/^[A-Za-z0-9._~-]{43,128}$/.test(verifier) ||
            createHash("sha256").update(verifier).digest("base64url") !==
              grant.challenge
          ) {
            journal.push({ kind: "deny", reason: "invalid_code" });
            send(400, { error: "invalid_grant" });
            return;
          }
          const identity = {
            principal: alias(trust.subject),
            issuer: alias(origin),
          };
          const accessToken = randomUUID();
          tokens.set(accessToken, {
            ...identity,
            expiresAt: Date.now() + 60_000,
          });
          journal.push({ kind: "oauth_code", ...identity, resource, scope });
          send(200, {
            access_token: accessToken,
            token_type: "Bearer",
            expires_in: 60,
            scope,
          });
          return;
        }
        if (
          form.get("grant_type") !== EMA_GRANT ||
          form.getAll("assertion").length !== 1 ||
          form.has("subject_token")
        ) {
          journal.push({ kind: "deny", reason: "unsupported_grant_type" });
          send(400, { error: "unsupported_grant_type" });
          return;
        }
        try {
          const identity = await verifyEMAAssertion(
            form.get("assertion")!,
            trust,
            { audience: origin, resource, clientID, scope },
            tls.ca,
          );
          if (seen.has(identity.jti)) throw new Error("assertion_replay");
          seen.add(identity.jti);
          if (!permitted) throw new Error("permission_denied");
          const accessToken = randomUUID();
          tokens.set(accessToken, {
            ...identity,
            expiresAt: Date.now() + 60_000,
          });
          journal.push({
            kind: "exchange",
            principal: identity.principal,
            issuer: identity.issuer,
            resource,
            scope,
          });
          send(200, {
            access_token: accessToken,
            token_type: "Bearer",
            expires_in: 60,
            scope,
          });
        } catch {
          // Never record raw assertions, claims, credentials, or exception bodies.
          journal.push({ kind: "deny", reason: "invalid_grant" });
          send(400, { error: "invalid_grant" });
        }
        return;
      }
      if (path === "/mcp") {
        const authorization = request.headers.authorization || "";
        const identity = authorization.startsWith("Bearer ")
          ? tokens.get(authorization.slice(7))
          : undefined;
        if (!identity || identity.expiresAt <= Date.now() || !permitted) {
          response.setHeader(
            "WWW-Authenticate",
            `Bearer resource_metadata="${origin}/.well-known/oauth-protected-resource/mcp"`,
          );
          send(401, { error: "unauthorized" });
          return;
        }
        if (request.method !== "POST") {
          response.writeHead(405).end();
          return;
        }
        const rpc = JSON.parse(await body(request));
        if (rpc.id === undefined) {
          response.writeHead(202).end();
          return;
        }
        const reply = (result: unknown) =>
          send(200, { jsonrpc: "2.0", id: rpc.id, result });
        if (rpc.method === "initialize")
          reply({
            protocolVersion: rpc.params.protocolVersion,
            capabilities: { tools: {} },
            serverInfo: { name: "ema-fixture", version: "1" },
          });
        else if (rpc.method === "tools/list")
          reply({
            tools: [
              {
                name: "identity",
                description: "Verified EMA identity",
                inputSchema: {
                  type: "object",
                  properties: {},
                  additionalProperties: false,
                },
              },
            ],
          });
        else if (
          rpc.method === "tools/call" &&
          rpc.params.name === "identity"
        ) {
          const event: EMAEvent = {
            kind: "tool",
            principal: identity.principal,
            issuer: identity.issuer,
            resource,
            scope,
          };
          journal.push(event);
          reply({ content: [{ type: "text", text: JSON.stringify(event) }] });
        } else
          send(200, {
            jsonrpc: "2.0",
            id: rpc.id,
            error: { code: -32601, message: "Method not found" },
          });
        return;
      }
      send(404, { error: "not_found" });
    })().catch(() => {
      if (!response.headersSent) response.writeHead(400);
      response.end("{}");
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("EMA fixture did not bind TCP");
  origin = `https://localhost:${address.port}`;
  return {
    origin,
    resource: `${origin}/mcp`,
    scope,
    journal,
    trust(value: EMATrust) {
      trust = { ...value };
    },
    deny() {
      permitted = false;
      tokens.clear();
    },
    close: () =>
      new Promise<void>((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
        server.closeAllConnections();
      }),
  };
}
