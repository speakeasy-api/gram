import { webcrypto } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  acceptInspectCallback,
  INSPECT_OAUTH_CALLBACK,
  prepareInspectAuthorization,
  readInspectGrant,
} from "./inspect-oauth";

const serverUrl = "https://platform.example/mcp/test-server";
const tokenUrl = `${serverUrl}/token`;
const grantKey = "test-grant";
const jwtFor = (sub: string) =>
  `header.${btoa(JSON.stringify({ sub }))}.signature`;

async function start() {
  return new URL(
    await prepareInspectAuthorization({
      serverUrl,
      tokenUrl,
      grantKey,
      userId: "viewer",
    }),
  );
}

function callback(authorize: URL, values: Record<string, string>) {
  return new URLSearchParams({
    state: authorize.searchParams.get("state")!,
    ...values,
  });
}

beforeEach(() => {
  sessionStorage.clear();
  history.replaceState(
    null,
    "",
    "/org/project/mcp/test-server/inspect?tab=tools#tool",
  );
  vi.stubGlobal("crypto", webcrypto);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Inspect OAuth", () => {
  it("exchanges a matching state and the original S256 verifier once, then scopes the grant to its target", async () => {
    const authorize = await start();
    const redirect = `${location.origin}${INSPECT_OAUTH_CALLBACK}`;
    expect(`${authorize.origin}${authorize.pathname}`).toBe(
      `${serverUrl}/authorize`,
    );
    expect(Object.fromEntries(authorize.searchParams)).toMatchObject({
      client_id: "speakeasy-dashboard-inspect",
      redirect_uri: redirect,
      response_type: "code",
      code_challenge_method: "S256",
      requireUserIdentity: "true",
    });
    const fetch = vi.fn(async (_url: string, init: RequestInit) => {
      const form = init.body as URLSearchParams;
      const verifier = form.get("code_verifier")!;
      const digest = await webcrypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(verifier),
      );
      expect(Buffer.from(digest).toString("base64url")).toBe(
        authorize.searchParams.get("code_challenge"),
      );
      expect(Object.fromEntries(form)).toMatchObject({
        grant_type: "authorization_code",
        redirect_uri: redirect,
        client_id: "speakeasy-dashboard-inspect",
        code: "code-from-consent",
      });
      return Response.json({
        access_token: jwtFor("user:viewer"),
        token_type: "Bearer",
        expires_in: 60,
      });
    });
    vi.stubGlobal("fetch", fetch);

    const params = callback(authorize, { code: "code-from-consent" });
    expect(
      acceptInspectCallback(
        callback(authorize, { code: "wrong-code", state: "wrong-state" }),
      ),
    ).toBeUndefined();
    const result = acceptInspectCallback(params);
    expect(result?.returnTo).toBe(
      `${location.origin}/org/project/mcp/test-server/inspect?tab=tools#tool`,
    );
    expect(await result?.exchange()).toBeUndefined();
    expect(fetch).toHaveBeenCalledExactlyOnceWith(
      tokenUrl,
      expect.objectContaining({
        method: "POST",
        credentials: "omit",
        redirect: "error",
      }),
    );
    expect(acceptInspectCallback(params)).toBeUndefined();
    expect(readInspectGrant(grantKey)?.accessToken).toBe(jwtFor("user:viewer"));
    expect(readInspectGrant("another-target")).toBeUndefined();
    vi.useFakeTimers();
    try {
      vi.setSystemTime(Date.now() + 61_000);
      expect(readInspectGrant(grantKey)).toBeUndefined();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not exchange a denial or accept a token for another viewer", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        access_token: jwtFor("user:other"),
        token_type: "bearer",
        expires_in: 60,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    const denied = acceptInspectCallback(
      callback(await start(), { error: "access_denied" }),
    );
    expect(await denied?.exchange()).toMatch(/wasn't completed/);
    expect(fetch).not.toHaveBeenCalled();
    const result = acceptInspectCallback(
      callback(await start(), { code: "code" }),
    );
    expect(await result?.exchange()).toMatch(/same account/);
    expect(readInspectGrant(grantKey)).toBeUndefined();
  });
});
