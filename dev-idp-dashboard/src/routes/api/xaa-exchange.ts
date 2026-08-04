import { createFileRoute } from "@tanstack/react-router";

/**
 * Runs the whole cross-app access dance as a client would, and reports every
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

/** Params echoed back to the UI with any secret blanked. */
function redact(params: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(params)) {
    out[k] =
      k === "client_secret" || k === "subject_token" || k === "assertion"
        ? `${v.slice(0, 12)}…(${v.length} chars)`
        : v;
  }
  return out;
}

async function postForm(
  name: string,
  url: string,
  params: Record<string, string>,
): Promise<Leg> {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(params).toString(),
  });
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {
    // Non-JSON bodies are surfaced verbatim; an OAuth server answering with
    // HTML is itself the useful signal.
  }
  return {
    name,
    request: { url, params: redact(params) },
    status: res.status,
    body,
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
  const redirectURI = "http://localhost/devidp-playground";
  const authorizeURL = `${devidp}/oauth2-1/authorize?${new URLSearchParams({
    response_type: "code",
    client_id: "gram-local-dev",
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
    body: code
      ? { code: `${code.slice(0, 12)}…` }
      : { error: await authorizeRes.text() },
  });
  if (!code) return fail("authorize");

  // Leg 1b — redeem the code for the id_token that becomes the subject token.
  const tokenLeg = await postForm(
    "token (id_token)",
    `${devidp}/oauth2-1/token`,
    {
      grant_type: "authorization_code",
      code,
      client_id: "gram-local-dev",
      redirect_uri: redirectURI,
    },
  );
  legs.push(tokenLeg);
  const idToken = pick(tokenLeg.body, "id_token");
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

  const mintLeg = await postForm(
    "mint ID-JAG",
    `${devidp}/oauth2-1/token`,
    mintParams,
  );
  legs.push(mintLeg);
  const jag = pick(mintLeg.body, "access_token");
  if (!jag) return fail("mint ID-JAG");

  const decoded = decodeJWT(jag);

  // Leg 3 — redeem it wherever the caller pointed us.
  const tokenEndpoint = req.token_endpoint || `${req.audience}/token`;
  const redeemParams: Record<string, string> = {
    grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
    assertion: jag,
    client_id: req.client_id,
  };
  const redeemLeg = await postForm(
    "redeem ID-JAG",
    tokenEndpoint,
    redeemParams,
  );
  legs.push(redeemLeg);
  if (!pick(redeemLeg.body, "access_token")) {
    return { ok: false, failed_at: "redeem ID-JAG", legs, id_jag: decoded };
  }

  return { ok: true, legs, id_jag: decoded };
}

export const Route = createFileRoute("/api/xaa-exchange")({
  server: {
    handlers: {
      POST: async ({ request }) => {
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

        const req = (await request.json()) as ExchangeRequest;
        if (!req.client_id || !req.audience) {
          return Response.json(
            { error: "client_id and audience are required" },
            { status: 400 },
          );
        }

        try {
          return Response.json(await runExchange(devidp, req));
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
