import { z } from "zod";

// Keep this outside the dashboard proxy prefixes (e.g. /mcp and /oauth).
export const INSPECT_OAUTH_CALLBACK = "/inspect/oauth/callback";
const clientId = "speakeasy-dashboard-inspect";
const pendingKey = "inspect-oauth:pending";
const pendingLifetime = 15 * 60_000;

const grantSchema = z.object({
  accessToken: z.string().min(1),
  expiresAt: z.number(),
});
const pendingSchema = z.object({
  state: z.string(),
  verifier: z.string(),
  tokenUrl: z.string().url(),
  returnTo: z.string(),
  grantKey: z.string(),
  userId: z.string(),
  expiresAt: z.number(),
});
const tokenSchema = z.object({
  access_token: z.string().min(1),
  token_type: z.string().toLowerCase().pipe(z.literal("bearer")),
  expires_in: z.number().positive(),
});

function readJSON(key: string): unknown {
  try {
    return JSON.parse(sessionStorage.getItem(key) ?? "null");
  } catch {
    return null;
  }
}

function base64url(bytes: Uint8Array) {
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function oauthEndpoint(url: string): URL {
  const endpoint = new URL(url, location.origin);
  // HTTP is permitted only for directly addressed local development services.
  const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(
    endpoint.hostname,
  );
  if (
    (endpoint.protocol !== "https:" &&
      !(loopback && endpoint.protocol === "http:")) ||
    endpoint.username ||
    endpoint.password
  ) {
    throw new Error("Inspect requires a secure OAuth endpoint.");
  }
  return endpoint;
}

const callbackUrl = () => new URL(INSPECT_OAUTH_CALLBACK, location.origin).href;

export function readInspectGrant(
  grantKey: string | undefined,
): z.infer<typeof grantSchema> | undefined {
  if (!grantKey) return undefined;
  const grant = grantSchema.safeParse(readJSON(grantKey));
  if (grant.success && grant.data.expiresAt > Date.now()) return grant.data;
  return undefined;
}

export async function prepareInspectAuthorization(input: {
  serverUrl: string;
  tokenUrl: string;
  grantKey: string;
  userId: string;
}): Promise<string> {
  const verifier = base64url(crypto.getRandomValues(new Uint8Array(32)));
  const state = base64url(crypto.getRandomValues(new Uint8Array(32)));
  const hash = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  const authorize = oauthEndpoint(`${input.serverUrl}/authorize`);
  const tokenUrl = oauthEndpoint(input.tokenUrl);
  authorize.search = new URLSearchParams({
    response_type: "code",
    client_id: clientId,
    redirect_uri: callbackUrl(),
    state,
    code_challenge: base64url(new Uint8Array(hash)),
    code_challenge_method: "S256",
    requireUserIdentity: "true",
  }).toString();
  sessionStorage.removeItem(input.grantKey);
  sessionStorage.setItem(
    pendingKey,
    JSON.stringify({
      state,
      verifier,
      tokenUrl: tokenUrl.href,
      returnTo: location.pathname + location.search + location.hash,
      grantKey: input.grantKey,
      userId: input.userId,
      expiresAt: Date.now() + pendingLifetime,
    }),
  );
  return authorize.href;
}

type InspectCallback = {
  returnTo: string;
  exchange: () => Promise<string | undefined>;
};

export function acceptInspectCallback(
  query: URLSearchParams,
): InspectCallback | undefined {
  const pending = pendingSchema.safeParse(readJSON(pendingKey));
  if (
    !pending.success ||
    pending.data.state !== query.get("state") ||
    pending.data.expiresAt <= Date.now()
  ) {
    return undefined;
  }
  sessionStorage.removeItem(pendingKey);
  const { returnTo, verifier, tokenUrl, grantKey, userId } = pending.data;
  const destination = new URL(returnTo, location.origin);
  if (destination.origin !== location.origin) return undefined;
  const code = query.get("code");
  return {
    returnTo: destination.href,
    exchange: async (): Promise<string | undefined> => {
      if (query.has("error") || !code) return "Sign-in wasn't completed.";
      const response = await fetch(oauthEndpoint(tokenUrl).href, {
        method: "POST",
        credentials: "omit",
        redirect: "error",
        body: new URLSearchParams({
          grant_type: "authorization_code",
          client_id: clientId,
          redirect_uri: callbackUrl(),
          code_verifier: verifier,
          code,
        }),
      });
      if (!response.ok) return "Couldn't finish signing in.";
      const token = tokenSchema.parse(await response.json());
      // Bind the grant from our issuer to the viewer who started Inspect,
      // not another account they may have picked during consent.
      const payload = (token.access_token.split(".")[1] ?? "")
        .replace(/-/g, "+")
        .replace(/_/g, "/");
      const claims = z
        .object({ sub: z.string() })
        .parse(JSON.parse(atob(payload)));
      if (claims.sub !== `user:${userId}`)
        return "Sign in with the same account you use in the dashboard.";
      // Browser Inspect must attach a bearer token directly to MCP requests.
      // Keep only the expiring access token, not the refresh token, in this
      // tab rather than localStorage. This lets the full-page callback return
      // to Inspect and survive reloads without sharing credentials across tabs.
      sessionStorage.setItem(
        grantKey,
        JSON.stringify({
          accessToken: token.access_token,
          expiresAt: Date.now() + token.expires_in * 1000,
        }),
      );
      return undefined;
    },
  };
}
