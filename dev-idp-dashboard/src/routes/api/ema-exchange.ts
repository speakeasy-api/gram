import { createFileRoute } from "@tanstack/react-router";

/**
 * Runs the whole enterprise-managed authorization dance as a client would,
 * and reports every
 * leg.
 *
 * This lives server-side rather than in the browser for one concrete reason:
 * leg 1 is an OAuth redirect, and reading the authorization code means seeing
 * the 302 without following it. `fetch` in the browser cannot decline to
 * follow a cross-origin redirect, and the dashboard's generic dev-idp proxy
 * follows redirects too. Here the handler can use redirect: "manual" and pull
 * the code straight off the Location header.
 *
 * The upstream token endpoint is a parameter, not a derived value, so the
 * redeem leg can be pointed at something other than this dev-idp -- which is
 * the point of being able to mint an ID-JAG at all.
 */

/** One HTTP round trip, recorded for display. */
interface Leg {
  name: string;
  request: { url: string; params: Record<string, string> };
  status: number;
  body: unknown;
}

interface ExchangeRequest {
  client_id: string;
  client_secret?: string;
  client_assertion?: string;
  audience: string;
  resource?: string;
  scope?: string;
  /** Where to redeem the ID-JAG. Defaults to `${audience}/token`. */
  token_endpoint?: string;
}

interface ExchangeResult {
  ok: boolean;
  /** The leg that failed, when ok is false. */
  failed_at?: string;
  legs: Leg[];
  id_jag?: { header: unknown; claims: unknown };
}

const REDACTED = "[REDACTED]";

function isSensitiveField(key: string): boolean {
  return (
    key === "client_secret" ||
    key === "subject_token" ||
    key === "assertion" ||
    key === "client_assertion" ||
    key === "code" ||
    key === "token" ||
    key.endsWith("_token")
  );
}

/** Params echoed back to the UI with credentials replaced by a constant. */
function redactParams(params: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(params)) {
    out[k] = isSensitiveField(k) ? REDACTED : v;
  }
  return out;
}

/** Redacts OAuth credentials from response bodies before displaying them. */
function redactBody(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactBody);
  if (!value || typeof value !== "object") return value;

  const out: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value)) {
    out[key] = isSensitiveField(key) ? REDACTED : redactBody(child);
  }
  return out;
}

async function postForm(
  name: string,
  url: string,
  params: Record<string, string>,
): Promise<{ leg: Leg; rawBody: unknown }> {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(params).toString(),
  });
  const text = await res.text();
  let rawBody: unknown = text;
  let displayBody: unknown = text ? "[non-JSON response omitted]" : "";
  try {
    rawBody = JSON.parse(text);
    displayBody = redactBody(rawBody);
  } catch {
    // Keep non-JSON bodies internal because an upstream may reflect submitted
    // credentials in an error page.
  }
  return {
    leg: {
      name,
      request: { url, params: redactParams(params) },
      status: res.status,
      body: displayBody,
    },
    rawBody,
  };
}

/** Decodes a JWT for display. Does not verify — the redeem leg does that. */
function decodeJWT(token: string): { header: unknown; claims: unknown } {
  const [h, p] = token.split(".");
  const parse = (segment: string) =>
    JSON.parse(
      Buffer.from(
        segment.replace(/-/g, "+").replace(/_/g, "/"),
        "base64",
      ).toString("utf8"),
    );
  return { header: parse(h ?? ""), claims: parse(p ?? "") };
}

function pick(body: unknown, key: string): string | null {
  if (body && typeof body === "object" && key in body) {
    const v = (body as Record<string, unknown>)[key];
    return typeof v === "string" ? v : null;
  }
  return null;
}

async function runExchange(
  devidp: string,
  req: ExchangeRequest,
): Promise<ExchangeResult> {
  const legs: Leg[] = [];
  const fail = (failedAt: string): ExchangeResult => ({
    ok: false,
    failed_at: failedAt,
    legs,
  });

  // Leg 1a — authorize. The redirect_uri never receives anything; the handler
  // reads the code off the Location header instead of following it.
  //
  // The login client is read from the environment rather than hardcoded: a
  // checkout that sets GRAM_IDP_CLIENT_ID has a dev-idp that rejects the
  // default, and the playground would fail on its very first leg.
  const loginClientID = process.env["GRAM_IDP_CLIENT_ID"] || "gram-local-dev";
  const redirectURI = "http://localhost/devidp-playground";
  const authorizeURL = `${devidp}/oauth2-1/authorize?${new URLSearchParams({
    response_type: "code",
    client_id: loginClientID,
    redirect_uri: redirectURI,
    scope: "openid email profile",
  }).toString()}`;

  const authorizeRes = await fetch(authorizeURL, { redirect: "manual" });
  const location = authorizeRes.headers.get("location") ?? "";
  const code = location ? new URL(location).searchParams.get("code") : null;
  legs.push({
    name: "authorize (as the current user)",
    request: { url: authorizeURL, params: {} },
    status: authorizeRes.status,
    body: code ? { code: REDACTED } : { error: await authorizeRes.text() },
  });
  if (!code) return fail("authorize");

  // Leg 1b — redeem the code for the id_token that becomes the subject token.
  const tokenRoundTrip = await postForm(
    "token (id_token)",
    `${devidp}/oauth2-1/token`,
    {
      grant_type: "authorization_code",
      code,
      client_id: loginClientID,
      redirect_uri: redirectURI,
    },
  );
  legs.push(tokenRoundTrip.leg);
  const idToken = pick(tokenRoundTrip.rawBody, "id_token");
  if (!idToken) return fail("token");

  // Leg 2 — the token exchange that produces the ID-JAG.
  const mintParams: Record<string, string> = {
    grant_type: "urn:ietf:params:oauth:grant-type:token-exchange",
    requested_token_type: "urn:ietf:params:oauth:token-type:id-jag",
    audience: req.audience,
    subject_token: idToken,
    subject_token_type: "urn:ietf:params:oauth:token-type:id_token",
    client_id: req.client_id,
  };
  if (req.resource) mintParams["resource"] = req.resource;
  if (req.scope) mintParams["scope"] = req.scope;
  if (req.client_secret) mintParams["client_secret"] = req.client_secret;
  if (req.client_assertion) {
    mintParams["client_assertion"] = req.client_assertion;
    mintParams["client_assertion_type"] =
      "urn:ietf:params:oauth:client-assertion-type:jwt-bearer";
  }

  const mintRoundTrip = await postForm(
    "mint ID-JAG",
    `${devidp}/oauth2-1/token`,
    mintParams,
  );
  legs.push(mintRoundTrip.leg);
  const jag = pick(mintRoundTrip.rawBody, "access_token");
  if (!jag) return fail("mint ID-JAG");

  const decoded = decodeJWT(jag);

  // Leg 3 — redeem it wherever the caller pointed us.
  const tokenEndpoint = req.token_endpoint || `${req.audience}/token`;
  const redeemParams: Record<string, string> = {
    grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
    assertion: jag,
    client_id: req.client_id,
  };
  const redeemRoundTrip = await postForm(
    "redeem ID-JAG",
    tokenEndpoint,
    redeemParams,
  );
  legs.push(redeemRoundTrip.leg);
  if (!pick(redeemRoundTrip.rawBody, "access_token")) {
    return { ok: false, failed_at: "redeem ID-JAG", legs, id_jag: decoded };
  }

  return { ok: true, legs, id_jag: decoded };
}

export const Route = createFileRoute("/api/ema-exchange")({
  server: {
    handlers: {
      POST: async ({ request }) => {
        let body: unknown;
        try {
          body = await request.json();
        } catch {
          return Response.json(
            { error: "Request body must be valid JSON" },
            { status: 400 },
          );
        }
        if (!body || typeof body !== "object" || Array.isArray(body)) {
          return Response.json(
            { error: "Request body must be a JSON object" },
            { status: 400 },
          );
        }

        const req = body as Partial<ExchangeRequest>;
        if (
          typeof req.client_id !== "string" ||
          !req.client_id ||
          typeof req.audience !== "string" ||
          !req.audience
        ) {
          return Response.json(
            { error: "client_id and audience are required" },
            { status: 400 },
          );
        }

        const devidp = process.env["GRAM_DEVIDP_EXTERNAL_URL"];
        if (!devidp) {
          return Response.json(
            {
              error:
                "GRAM_DEVIDP_EXTERNAL_URL is not set on the dashboard server",
            },
            { status: 500 },
          );
        }

        try {
          return Response.json(
            await runExchange(devidp, req as ExchangeRequest),
          );
        } catch (e) {
          return Response.json(
            { error: e instanceof Error ? e.message : String(e) },
            { status: 502 },
          );
        }
      },
    },
  },
});
