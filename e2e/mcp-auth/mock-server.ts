import {
  createServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import {
  createHash,
  generateKeyPairSync,
  randomBytes,
  sign,
} from "node:crypto";

export type MockEvent = {
  kind: string;
  provider?: string;
  clientId?: string;
  resource?: string;
  scopes?: string[];
  principal?: string;
  method?: string;
  tool?: string;
  headerNames?: string[];
  outcome?: string;
};
type Provider = "alpha" | "beta";
type Client = { secret: string; redirects: string[] };
type Grant = {
  provider: Provider;
  clientId: string;
  resource: string;
  scopes: string[];
  principal: string;
};
type Code = Grant & { redirect: string; challenge: string; expires: number };
type Token = Grant & { expires: number; revoked: boolean; family: string };
const opaque = () => randomBytes(32).toString("base64url");
const hash = (value: string) =>
  createHash("sha256").update(value).digest("base64url");

/** Local-only upstream fixtures. The journal deliberately excludes bodies, URLs and header values. */
export async function startMockAuthServer(
  options: {
    host?: string;
    port?: number;
    accessTokenLifetimeSeconds?: number;
  } = {},
) {
  const accessTTL = options.accessTokenLifetimeSeconds ?? 3600;
  if (!Number.isInteger(accessTTL) || accessTTL < 1)
    throw new Error("Token lifetime must be a positive number of seconds");
  const events: MockEvent[] = [];
  type TokenFailure = { status: number; error?: string; body?: string };
  const tokenFailures = new Map<Provider, TokenFailure>();
  let advertisedLifetime: number | null = null;
  let closing = false;
  const resourceAuth = new Map<string, boolean>();
  const resourceScopes = new Map<string, string[]>();
  type Barrier = {
    started: () => void;
    wait: Promise<void>;
    release: () => void;
  };
  const nextRefresh = new Map<Provider, Barrier>();
  const barriers = new Set<Barrier>();
  const clients = {
    alpha: new Map<string, Client>(),
    beta: new Map<string, Client>(),
  };
  const codes = new Map<string, Code>();
  const access = new Map<string, Token>();
  const refresh = new Map<string, Token>();
  const { privateKey, publicKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
  });
  const jwk = {
    ...publicKey.export({ format: "jwk" }),
    kid: "fixture-signing-key",
    use: "sig",
    alg: "RS256",
  };
  let origin = "";
  const issuer = (provider: Provider) => `${origin}/${provider}`;
  const profiles = {
    oauthAlpha: "",
    oauthBeta: "",
    oauthScoped: "",
    static: "",
    passThrough: "",
    noAuth: "",
    noAuthTwin: "",
    multiple: "",
    multiMethod: "",
    multiMethodTwin: "",
    multiMethodBeta: "",
    serviceAccount: "",
    dualProvider: "",
  };
  const providers = {
    alpha: {
      issuer: "",
      clientId: "fixture-alpha-client",
      clientSecret: "fixture-alpha-secret",
    },
    beta: {
      issuer: "",
      clientId: "fixture-beta-client",
      clientSecret: "fixture-beta-secret",
    },
  };
  for (const provider of ["alpha", "beta"] as const) {
    clients[provider].set(providers[provider].clientId, {
      secret: providers[provider].clientSecret,
      redirects: [],
    });
  }
  const send = (res: ServerResponse, status: number, body: unknown) => {
    if (closing || res.destroyed) return;
    res.writeHead(status, {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    });
    res.end(JSON.stringify(body));
  };
  const fail = (res: ServerResponse, error: string, status = 400) =>
    send(res, status, { error });
  const body = async (req: IncomingMessage) => {
    let text = "";
    for await (const chunk of req) {
      text += chunk.toString();
      if (text.length > 1024 * 1024) throw new Error("Body too large");
    }
    return text;
  };
  const validResource = (provider: Provider, resource: string) =>
    provider === "alpha"
      ? [
          ...(resourceAuth.get(profiles.noAuth) ? [profiles.noAuth] : []),
          profiles.oauthAlpha,
          profiles.oauthScoped,
          profiles.multiple,
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.serviceAccount,
          profiles.dualProvider,
        ].includes(resource)
      : [
          profiles.oauthBeta,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource);
  const requiredScopes = (resource: string) =>
    resourceScopes.get(resource) ?? [
      resource === profiles.oauthScoped ? "fixture:scoped" : "fixture:read",
    ];
  const revokeFamily = (family: string) => {
    for (const token of [...access.values(), ...refresh.values()])
      if (token.family === family) token.revoked = true;
  };
  const issue = (
    res: ServerResponse,
    grant: Grant,
    family = opaque(),
    nonce?: string,
    includeRefresh = true,
  ) => {
    if (closing || res.destroyed) return;
    const accessToken = opaque();
    const refreshToken = opaque();
    access.set(accessToken, {
      ...grant,
      family,
      expires: Date.now() + accessTTL * 1000,
      revoked: false,
    });
    if (includeRefresh)
      refresh.set(refreshToken, {
        ...grant,
        family,
        expires: Date.now() + 86400_000,
        revoked: false,
      });
    let idToken: string | undefined;
    if (includeRefresh && grant.scopes.includes("openid")) {
      const header = Buffer.from(
        JSON.stringify({ alg: "RS256", kid: jwk.kid, typ: "JWT" }),
      ).toString("base64url");
      const payload = Buffer.from(
        JSON.stringify({
          iss: issuer(grant.provider),
          sub: grant.principal,
          aud: grant.clientId,
          iat: Math.floor(Date.now() / 1000),
          exp: Math.floor(Date.now() / 1000) + 3600,
          nonce,
        }),
      ).toString("base64url");
      const unsigned = `${header}.${payload}`;
      idToken = `${unsigned}.${sign("RSA-SHA256", Buffer.from(unsigned), privateKey).toString("base64url")}`;
    }
    send(res, 200, {
      access_token: accessToken,
      ...(includeRefresh ? { refresh_token: refreshToken } : {}),
      token_type: "Bearer",
      expires_in: advertisedLifetime ?? accessTTL,
      scope: grant.scopes.join(" "),
      ...(idToken ? { id_token: idToken } : {}),
    });
  };
  const nonces = new Map<string, string>();
  const server = createServer((req, res) => {
    void (async () => {
      const url = new URL(req.url ?? "/", origin);
      const path = url.pathname;
      const metadataPrefix = "/.well-known/oauth-protected-resource";
      if (path.startsWith(metadataPrefix)) {
        const resource = `${origin}${path.slice(metadataPrefix.length)}`;
        const provider = [
          profiles.oauthBeta,
          profiles.multiMethodBeta,
        ].includes(resource)
          ? "beta"
          : "alpha";
        if (!validResource(provider, resource))
          return fail(res, "not_found", 404);
        return send(res, 200, {
          resource,
          authorization_servers:
            resource === profiles.dualProvider
              ? [issuer("alpha"), issuer("beta")]
              : [issuer(provider)],
          scopes_supported: [
            ...new Set([...requiredScopes(resource), "openid"]),
          ],
          bearer_methods_supported: ["header"],
        });
      }
      const providerMatch =
        path.match(/^\/(alpha|beta)(?:\/|$)/) ??
        path.match(
          /^\/\.well-known\/(?:oauth-authorization-server|openid-configuration)\/(alpha|beta)$/,
        );
      if (providerMatch) {
        const provider = providerMatch[1] as Provider;
        const base = issuer(provider);
        const endpoint = path.startsWith("/.well-known/")
          ? "discovery"
          : path.slice(provider.length + 2);
        if (
          endpoint === "discovery" ||
          endpoint === ".well-known/oauth-authorization-server" ||
          endpoint === ".well-known/openid-configuration"
        ) {
          return send(res, 200, {
            issuer: base,
            authorization_endpoint: `${base}/authorize`,
            token_endpoint: `${base}/token`,
            registration_endpoint: `${base}/register`,
            revocation_endpoint: `${base}/revoke`,
            introspection_endpoint: `${base}/introspect`,
            jwks_uri: `${base}/jwks`,
            response_types_supported: ["code"],
            grant_types_supported: [
              "authorization_code",
              "refresh_token",
              "client_credentials",
            ],
            code_challenge_methods_supported: ["S256"],
            token_endpoint_auth_methods_supported: [
              "client_secret_basic",
              "client_secret_post",
              "none",
            ],
            scopes_supported: [
              ...new Set([
                "fixture:read",
                "openid",
                ...[...resourceScopes]
                  .filter(([resource]) => validResource(provider, resource))
                  .flatMap(([, scopes]) => scopes),
              ]),
            ],
            subject_types_supported: ["public"],
            id_token_signing_alg_values_supported: ["RS256"],
          });
        }
        if (endpoint === "jwks") return send(res, 200, { keys: [jwk] });
        if (endpoint === "register" && req.method === "POST") {
          const data = JSON.parse(await body(req)) as {
            redirect_uris?: unknown;
            token_endpoint_auth_method?: string;
          };
          if (
            !Array.isArray(data.redirect_uris) ||
            !data.redirect_uris.length ||
            !data.redirect_uris.every(
              (v: unknown) => typeof v === "string" && /^https?:\/\//.test(v),
            )
          )
            return fail(res, "invalid_redirect_uri");
          const clientId = `fixture-${provider}-${opaque()}`;
          const secret =
            data.token_endpoint_auth_method === "none" ? "" : opaque();
          clients[provider].set(clientId, {
            secret,
            redirects: data.redirect_uris,
          });
          events.push({
            kind: "register",
            provider,
            clientId,
            outcome: "success",
          });
          return send(res, 201, {
            client_id: clientId,
            ...(secret ? { client_secret: secret } : {}),
            redirect_uris: data.redirect_uris,
            token_endpoint_auth_method:
              data.token_endpoint_auth_method ?? "client_secret_basic",
            grant_types: ["authorization_code", "refresh_token"],
            response_types: ["code"],
          });
        }
        if (
          endpoint === "authorize" &&
          (req.method === "GET" || req.method === "POST")
        ) {
          const params =
            req.method === "POST"
              ? new URLSearchParams(await body(req))
              : url.searchParams;
          const clientId = params.get("client_id") ?? "";
          const client = clients[provider].get(clientId);
          const redirect = params.get("redirect_uri") ?? "";
          // Fixed fixture clients permit loopback callbacks; dynamically registered clients require exact matches.
          let loopback = false;
          try {
            const u = new URL(redirect);
            loopback =
              ["127.0.0.1", "localhost", "[::1]"].includes(u.hostname) &&
              ["http:", "https:"].includes(u.protocol);
          } catch {
            /* invalid callback */
          }
          if (
            !client ||
            !(client.redirects.length
              ? client.redirects.includes(redirect)
              : loopback)
          )
            return fail(res, "invalid_request");
          const callback = new URL(redirect);
          if (params.has("state"))
            callback.searchParams.set("state", params.get("state")!);
          const resource = params.get("resource") ?? "";
          const scopes = (params.get("scope") ?? "fixture:read")
            .split(/\s+/)
            .filter(Boolean);
          const finish = (error?: string) => {
            if (error) {
              callback.searchParams.set("error", error);
              events.push({
                kind: "authorize",
                provider,
                clientId,
                resource,
                scopes,
                outcome: error,
              });
            }
            res.writeHead(302, {
              Location: callback.toString(),
              "Cache-Control": "no-store",
            });
            res.end();
          };
          const challenge = params.get("code_challenge") ?? "";
          if (params.get("response_type") !== "code")
            return finish("unsupported_response_type");
          if (!validResource(provider, resource))
            return finish("invalid_target");
          if (
            params.get("code_challenge_method") !== "S256" ||
            !/^[A-Za-z0-9_-]{43}$/.test(challenge)
          )
            return finish("invalid_request");
          if (req.method === "GET") {
            const escape = (s: string) =>
              s
                .replaceAll("&", "&amp;")
                .replaceAll('"', "&quot;")
                .replaceAll("<", "&lt;")
                .replaceAll(">", "&gt;");
            const hidden = [...params]
              .map(
                ([key, value]) =>
                  `<input type="hidden" name="${escape(key)}" value="${escape(value)}">`,
              )
              .join("");
            res.writeHead(200, {
              "Content-Type": "text/html; charset=utf-8",
              "Cache-Control": "no-store",
            });
            res.end(
              `<!doctype html><html><title>Mock authorization</title><body><h1>Mock ${provider} authorization</h1><form method="post">${hidden}<button name="decision" value="approve">Approve mock access</button><button name="decision" value="deny">Deny mock access</button></form></body></html>`,
            );
            return;
          }
          if (params.get("decision") !== "approve")
            return finish("access_denied");
          const code = opaque();
          codes.set(code, {
            provider,
            clientId,
            redirect,
            resource,
            scopes,
            challenge,
            expires: Date.now() + 60_000,
            principal: `${provider}-user`,
          });
          if (params.has("nonce")) nonces.set(code, params.get("nonce")!);
          events.push({
            kind: "authorize",
            provider,
            clientId,
            resource,
            scopes,
            principal: `${provider}-user`,
            outcome: "success",
          });
          callback.searchParams.set("code", code);
          return finish();
        }
        if (
          ["token", "revoke", "introspect"].includes(endpoint) &&
          req.method === "POST"
        ) {
          const params = new URLSearchParams(await body(req));
          if (endpoint === "token") {
            const failure = tokenFailures.get(provider);
            if (failure) {
              const requestedResource = params.get("resource") ?? "";
              const allowedScopes = new Set([
                "openid",
                ...Object.values(profiles).flatMap(requiredScopes),
              ]);
              events.push({
                kind: "token_failure",
                provider,
                outcome: String(failure.status),
                resource: validResource(provider, requestedResource)
                  ? requestedResource
                  : undefined,
                scopes: (params.get("scope") ?? "")
                  .split(/\s+/)
                  .filter((scope) => allowedScopes.has(scope)),
              });
              if (failure.body !== undefined) {
                res.writeHead(failure.status, {
                  "Content-Type": "text/plain",
                  "Cache-Control": "no-store",
                });
                res.end(failure.body);
                return;
              }
              return fail(
                res,
                failure.error ?? "temporarily_unavailable",
                failure.status,
              );
            }
          }
          let clientId = params.get("client_id") ?? "";
          let secret = params.get("client_secret") ?? "";
          if (req.headers.authorization?.startsWith("Basic ")) {
            const decoded = Buffer.from(
              req.headers.authorization.slice(6),
              "base64",
            ).toString();
            const colon = decoded.indexOf(":");
            clientId = decodeURIComponent(decoded.slice(0, colon));
            secret = decodeURIComponent(decoded.slice(colon + 1));
          }
          const client = clients[provider].get(clientId);
          if (!client || secret !== client.secret)
            return fail(res, "invalid_client", 401);
          if (endpoint !== "token") {
            const token =
              access.get(params.get("token") ?? "") ??
              refresh.get(params.get("token") ?? "");
            const owned =
              token?.provider === provider && token.clientId === clientId;
            if (endpoint === "revoke") {
              if (owned) revokeFamily(token.family);
              events.push({
                kind: "revoke",
                provider,
                clientId,
                outcome: "success",
              });
              return send(res, 200, {});
            }
            return send(
              res,
              200,
              owned && !token.revoked && token.expires > Date.now()
                ? {
                    active: true,
                    client_id: clientId,
                    sub: token.principal,
                    aud: token.resource,
                    scope: token.scopes.join(" "),
                    exp: Math.floor(token.expires / 1000),
                  }
                : { active: false },
            );
          }
          const grantType = params.get("grant_type");
          if (grantType === "client_credentials") {
            if (!client.secret) return fail(res, "invalid_client", 401);
            const resource = params.get("resource") ?? "";
            const scopes = (params.get("scope") ?? "fixture:read")
              .split(/\s+/)
              .filter(Boolean);
            const outcome = validResource(provider, resource)
              ? "success"
              : "invalid_target";
            events.push({
              kind: "client_credentials",
              provider,
              clientId,
              resource,
              scopes,
              outcome,
            });
            if (outcome !== "success") return fail(res, "invalid_target");
            return issue(
              res,
              {
                provider,
                clientId,
                resource,
                scopes,
                principal: `${provider}-service`,
              },
              undefined,
              undefined,
              false,
            );
          }
          if (grantType === "authorization_code") {
            const codeValue = params.get("code") ?? "";
            const code = codes.get(codeValue);
            codes.delete(codeValue);
            const nonce = nonces.get(codeValue);
            nonces.delete(codeValue);
            const verifier = params.get("code_verifier") ?? "";
            if (
              !code ||
              code.provider !== provider ||
              code.clientId !== clientId ||
              code.expires <= Date.now() ||
              code.redirect !== params.get("redirect_uri") ||
              !/^[A-Za-z0-9._~-]{43,128}$/.test(verifier) ||
              hash(verifier) !== code.challenge ||
              (params.has("resource") &&
                params.get("resource") !== code.resource)
            )
              return fail(res, "invalid_grant");
            events.push({
              kind: "token",
              provider,
              clientId,
              resource: code.resource,
              scopes: code.scopes,
              outcome: "success",
            });
            return issue(res, code, undefined, nonce);
          }
          if (grantType === "refresh_token") {
            const token = refresh.get(params.get("refresh_token") ?? "");
            if (
              !token ||
              token.provider !== provider ||
              token.clientId !== clientId
            )
              return fail(res, "invalid_grant");
            if (token.revoked) {
              revokeFamily(token.family);
              return fail(res, "invalid_grant");
            }
            if (
              token.expires <= Date.now() ||
              (params.has("resource") &&
                params.get("resource") !== token.resource)
            )
              return fail(res, "invalid_grant");
            const scopes = params.has("scope")
              ? params.get("scope")!.split(/\s+/).filter(Boolean)
              : token.scopes;
            if (scopes.some((scope) => !token.scopes.includes(scope)))
              return fail(res, "invalid_scope");
            token.revoked = true;
            events.push({
              kind: "refresh",
              provider,
              clientId,
              resource: token.resource,
              scopes,
              outcome: "success",
            });
            const barrier = nextRefresh.get(provider);
            if (barrier) {
              nextRefresh.delete(provider);
              const disconnected = () => barrier.release();
              res.once("close", disconnected);
              barrier.started();
              await barrier.wait;
              res.off("close", disconnected);
              barriers.delete(barrier);
            }
            return issue(res, { ...token, scopes }, token.family);
          }
          return fail(res, "unsupported_grant_type");
        }
        return fail(res, "not_found", 404);
      }
      const resource = `${origin}${path}`;
      if (!Object.values(profiles).includes(resource))
        return fail(res, "not_found", 404);
      let insufficientScope = false;
      let principal: string | undefined;
      let provider: Provider | undefined;
      let clientId: string | undefined;
      if (
        (resource === profiles.noAuth && !resourceAuth.get(resource)) ||
        resource === profiles.noAuthTwin
      )
        principal = "anonymous";
      if (
        [
          profiles.static,
          profiles.multiple,
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource) &&
        req.headers["x-fixture-key"] === "fixture-static-secret"
      )
        principal = "static-user";
      if (
        [
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource) &&
        req.headers["x-fixture-key"] === "fixture-static-alt-secret"
      )
        principal = "static-alt-user";
      if (
        [
          profiles.passThrough,
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource) &&
        req.headers["x-fixture-user"] === "fixture-user-secret"
      )
        principal = "pass-through-user";
      if (
        !principal &&
        [
          ...(resourceAuth.get(profiles.noAuth) ? [profiles.noAuth] : []),
          profiles.oauthAlpha,
          profiles.oauthBeta,
          profiles.oauthScoped,
          profiles.multiple,
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.multiMethodBeta,
          profiles.dualProvider,
          profiles.serviceAccount,
        ].includes(resource)
      ) {
        const token = access.get(
          req.headers.authorization?.replace(/^Bearer /i, "") ?? "",
        );
        const expected = [
          profiles.oauthBeta,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource)
          ? "beta"
          : "alpha";
        if (
          token &&
          !token.revoked &&
          token.expires > Date.now() &&
          token.resource === resource &&
          (resource === profiles.dualProvider || token.provider === expected)
        ) {
          insufficientScope = !requiredScopes(resource).every((scope) =>
            token.scopes.includes(scope),
          );
          if (!insufficientScope) principal = token.principal;
          provider = token.provider;
          clientId = token.clientId;
        }
      }
      const mixedCredentials =
        [
          profiles.multiMethod,
          profiles.multiMethodTwin,
          profiles.multiMethodBeta,
          profiles.dualProvider,
        ].includes(resource) &&
        ["authorization", "x-fixture-key", "x-fixture-user"].filter(
          (name) => req.headers[name] !== undefined,
        ).length > 1;
      if (mixedCredentials) principal = undefined;
      if (!principal) {
        let rejectedMethod: string | undefined;
        let rejectedTool: string | undefined;
        if (req.method === "POST") {
          try {
            const message = JSON.parse(await body(req)) as {
              method?: unknown;
              params?: { name?: unknown };
            };
            if (typeof message.method === "string")
              rejectedMethod = message.method;
            if (typeof message.params?.name === "string")
              rejectedTool = message.params.name;
          } catch {
            /* A malformed unauthenticated request is still refused. */
          }
        }
        events.push({
          kind: "mcp",
          resource,
          method: rejectedMethod,
          tool: rejectedTool,
          headerNames: Object.keys(req.headers),
          outcome: mixedCredentials
            ? "mixed_credentials"
            : insufficientScope
              ? "insufficient_scope"
              : "unauthorized",
        });
        res.setHeader(
          "WWW-Authenticate",
          `Bearer resource_metadata="${origin}${metadataPrefix}${path}"${
            insufficientScope
              ? `, error="insufficient_scope", scope="${requiredScopes(resource).join(" ")}"`
              : ""
          }`,
        );
        return fail(
          res,
          mixedCredentials
            ? "mixed_credentials"
            : insufficientScope
              ? "insufficient_scope"
              : "unauthorized",
          mixedCredentials ? 400 : insufficientScope ? 403 : 401,
        );
      }
      if (req.method !== "POST") {
        res.setHeader("Allow", "POST");
        return fail(res, "method_not_allowed", 405);
      }
      const message = JSON.parse(await body(req)) as {
        jsonrpc?: string;
        id?: string | number;
        method?: string;
        params?: {
          name?: string;
          arguments?: unknown;
          protocolVersion?: string;
        };
      };
      const method = message.method;
      const tool = message.params?.name;
      events.push({
        kind: "mcp",
        provider,
        clientId,
        resource,
        principal,
        method,
        tool,
        headerNames: Object.keys(req.headers),
        outcome: "authorized",
      });
      if (message.id === undefined) {
        res.writeHead(202);
        res.end();
        return;
      }
      let result: unknown;
      if (method === "initialize")
        result = {
          protocolVersion: message.params?.protocolVersion ?? "2025-03-26",
          capabilities: { tools: {} },
          serverInfo: { name: "mock-auth-upstream", version: "1.0.0" },
        };
      else if (method === "ping") result = {};
      else if (method === "tools/list")
        result = {
          tools: [
            {
              name: "fixture_echo",
              description: "Echo arguments and authenticated fixture identity",
              inputSchema: { type: "object", additionalProperties: true },
            },
            {
              name: "fixture_identity",
              description: "Return authenticated fixture identity",
              inputSchema: { type: "object", properties: {} },
            },
          ],
        };
      else if (
        method === "tools/call" &&
        ["fixture_echo", "fixture_identity"].includes(tool ?? "")
      )
        result = {
          content: [
            {
              type: "text",
              text: JSON.stringify({
                principal,
                provider,
                clientId,
                resource,
                ...(tool === "fixture_echo"
                  ? { arguments: message.params?.arguments ?? {} }
                  : {}),
              }),
            },
          ],
          isError: false,
        };
      else
        return send(res, 200, {
          jsonrpc: "2.0",
          id: message.id,
          error: {
            code: method === "tools/call" ? -32602 : -32601,
            message: "Unknown fixture tool or method",
          },
        });
      return send(res, 200, { jsonrpc: "2.0", id: message.id, result });
    })().catch(() => {
      if (!res.headersSent) fail(res, "invalid_request");
      else res.end();
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(options.port ?? 0, options.host ?? "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Missing fixture server address");
  const host = address.address.includes(":")
    ? `[${address.address}]`
    : address.address;
  origin = `http://${host}:${address.port}`;
  Object.assign(profiles, {
    oauthAlpha: `${origin}/mcp/oauth-alpha`,
    oauthBeta: `${origin}/mcp/oauth-beta`,
    oauthScoped: `${origin}/mcp/oauth-scoped`,
    static: `${origin}/mcp/static`,
    passThrough: `${origin}/mcp/pass-through`,
    noAuth: `${origin}/mcp/no-auth`,
    noAuthTwin: `${origin}/mcp/no-auth-twin`,
    multiple: `${origin}/mcp/multiple`,
    multiMethod: `${origin}/mcp/multi-method`,
    multiMethodTwin: `${origin}/mcp/multi-method-twin`,
    multiMethodBeta: `${origin}/mcp/multi-method-beta`,
    serviceAccount: `${origin}/mcp/service-account`,
    dualProvider: `${origin}/mcp/dual-provider`,
  });
  providers.alpha.issuer = issuer("alpha");
  providers.beta.issuer = issuer("beta");
  return {
    origin,
    providers,
    profiles,
    events: events as readonly MockEvent[],
    setTokenFailure(provider: Provider, failure: TokenFailure | null) {
      if (failure === null) tokenFailures.delete(provider);
      else {
        if (
          !Number.isInteger(failure.status) ||
          failure.status < 400 ||
          failure.status > 599
        )
          throw new Error("Token failure status must be 400-599");
        tokenFailures.set(provider, { ...failure });
      }
    },
    setAdvertisedLifetime(seconds: number | null) {
      if (seconds !== null && (!Number.isInteger(seconds) || seconds < 0))
        throw new Error("Advertised lifetime must be nonnegative seconds");
      advertisedLifetime = seconds;
    },
    pauseNextRefresh(provider: Provider) {
      if (closing || nextRefresh.has(provider))
        throw new Error("Refresh barrier unavailable");
      let started!: () => void;
      let release!: () => void;
      const startedPromise = new Promise<void>((resolve) => {
        started = resolve;
      });
      const wait = new Promise<void>((resolve) => {
        release = resolve;
      });
      const barrier = { started, wait, release };
      nextRefresh.set(provider, barrier);
      barriers.add(barrier);
      return { started: startedPromise, release };
    },
    setResourceAuth(resource: string, required: boolean) {
      if (resource !== profiles.noAuth)
        throw new Error(
          "Only the no-auth resource can transition authentication",
        );
      resourceAuth.set(resource, required);
    },
    setResourceScopes(resource: string, scopes: string[]) {
      if (
        !Object.values(profiles).includes(resource) ||
        scopes.some((scope) => !/^[\x21\x23-\x5B\x5D-\x7E]+$/.test(scope))
      )
        throw new Error("Invalid resource or OAuth scope");
      resourceScopes.set(resource, [...new Set(scopes)]);
    },
    reset() {
      events.length = 0;
      codes.clear();
      nonces.clear();
      access.clear();
      refresh.clear();
    },
    expireTokens() {
      for (const token of access.values()) token.expires = 0;
    },
    revokeTokens() {
      for (const token of [...access.values(), ...refresh.values()])
        token.revoked = true;
    },
    async close() {
      closing = true;
      for (const barrier of barriers) barrier.release();
      barriers.clear();
      nextRefresh.clear();
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    },
  };
}
