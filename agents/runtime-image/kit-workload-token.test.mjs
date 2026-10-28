import { describe, expect, test } from "bun:test";
import { mkdtemp, readFile, readdir, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  fetchAssertion,
  publishAssertion,
  ready,
  refreshAssertion,
  refreshDelay,
  tokenExpiry,
} from "./kit-workload-token.mjs";

const now = 1800000000000;
const audience = "https://example.invalid/oauth/token";
const issuer = "https://example.invalid/agent-identity";
const subject = "spiffe://example.invalid/ns/runtime/sa/agent";
const encode = (value) =>
  Buffer.from(JSON.stringify(value)).toString("base64url");
function token(overrides = {}) {
  return `${encode({ alg: "RS256" })}.${encode({ iss: issuer, sub: subject, aud: [audience], iat: now / 1000, exp: now / 1000 + 3600, ...overrides })}.signature`;
}

describe("managed workload metadata assertions", () => {
  test("requests only the fixed metadata endpoint with exact audience and bounded timeout", async () => {
    const result = await fetchAssertion(
      audience,
      issuer,
      subject,
      async (url, options) => {
        expect(url.origin).toBe("http://169.254.169.254");
        expect(url.pathname).toBe(
          "/computeMetadata/v1/instance/service-accounts/default/identity",
        );
        expect([...url.searchParams]).toEqual([
          ["audience", audience],
          ["format", "full"],
        ]);
        expect(options.method).toBe("GET");
        expect(options.body).toBeUndefined();
        expect(options.headers).toEqual({ "Metadata-Flavor": "Google" });
        expect(options.headers["Metadata-Flavor"]).toBe("Google");
        expect(options.redirect).toBe("error");
        expect(options.signal).toBeInstanceOf(AbortSignal);
        return new Response(token(), {
          headers: { "Metadata-Flavor": "Google" },
        });
      },
      now,
    );
    expect(result.raw).toBe(token());
    expect(result.expiresAt).toBe(now + 3600000);
  });

  test("accepts only the exact string or singleton native audience", () => {
    for (const aud of [audience, [audience]])
      expect(tokenExpiry(token({ aud }), audience, issuer, subject, now)).toBe(
        now + 3600000,
      );
  });

  test("rejects wrong bindings, expiry, and unbounded lifetime", () => {
    for (const overrides of [
      { aud: "different" },
      { sub: subject + "-other" },
      { aud: [] },
      { aud: ["different"] },
      { aud: [audience, "different"] },
      { aud: ["different", audience] },
      { aud: [audience, audience] },
      { aud: [[audience]] },
      { aud: null },
      { cnf: { jkt: "key" } },
      { cnf: null },
      { cnf: false },
      { iat: now / 1000 + 30, exp: now / 1000 + 20 },
      { iat: "invalid" },
      { sub: "service-account-email" },
      { iss: "https://example.invalid" },
      { exp: now / 1000 + 60 },
      { exp: now / 1000 + 3601 },
      { iat: now / 1000 + 31 },
      { exp: "invalid" },
    ])
      expect(() =>
        tokenExpiry(token(overrides), audience, issuer, subject, now),
      ).toThrow();
    expect(() =>
      tokenExpiry("not-a-token", audience, issuer, subject, now),
    ).toThrow();
    expect(() =>
      tokenExpiry("x".repeat(16385), audience, issuer, subject, now),
    ).toThrow();
  });

  test("requires independent configured issuer and exact SPIFFE subject", () => {
    for (const [expectedIssuer, expectedSubject] of [
      [undefined, subject],
      ["", subject],
      [issuer + "/other", subject],
      [issuer, undefined],
      [issuer, ""],
      [issuer, "12345"],
      [issuer, subject + "/other"],
    ])
      expect(() =>
        tokenExpiry(token(), audience, expectedIssuer, expectedSubject, now),
      ).toThrow();
  });

  test("rejects missing metadata response attestation, HTTP errors, oversized responses", async () => {
    for (const response of [
      new Response(token()),
      new Response("unavailable", {
        status: 503,
        headers: { "Metadata-Flavor": "Google" },
      }),
      new Response("x".repeat(16385), {
        headers: { "Metadata-Flavor": "Google" },
      }),
    ])
      await expect(
        fetchAssertion(audience, issuer, subject, async () => response, now),
      ).rejects.toThrow();
  });

  test("replaces the file atomically, keeps restrictive permissions and gates expiry", async () => {
    const dir = await mkdtemp(join(tmpdir(), "kit-workload-"));
    const path = join(dir, "assertion");
    try {
      expect(await ready(path, audience, issuer, subject, now)).toBe(false);
      await publishAssertion(path, token());
      const originalInode = (await stat(path)).ino;
      expect(await ready(path, audience, issuer, subject, now)).toBe(true);
      const rotated = token({ iat: now / 1000 + 10, exp: now / 1000 + 3610 });
      await publishAssertion(path, rotated);
      expect(await readFile(path, "utf8")).toBe(rotated);
      expect((await stat(path)).ino).not.toBe(originalInode);
      expect((await stat(path)).mode & 0o777).toBe(0o600);
      expect(await readdir(dir)).toEqual(["assertion"]);
      expect(await ready(path, audience, issuer, subject, now + 3610000)).toBe(
        false,
      );
      expect(await ready(path, audience, issuer, "wrong", now)).toBe(false);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  test("refresh failure retains a still-valid token but removes expired authority", async () => {
    const dir = await mkdtemp(join(tmpdir(), "kit-workload-"));
    const path = join(dir, "assertion");
    const unavailable = async () => {
      throw new Error("sensitive upstream error must not escape");
    };
    try {
      await publishAssertion(path, token());
      expect(
        await refreshAssertion(
          path,
          audience,
          issuer,
          subject,
          unavailable,
          now,
        ),
      ).toEqual({ refreshed: false, delay: 5000 });
      expect(await readFile(path, "utf8")).toBe(token());
      expect(
        await refreshAssertion(
          path,
          audience,
          issuer,
          subject,
          unavailable,
          now + 3600000,
        ),
      ).toEqual({ refreshed: false, delay: 5000 });
      expect(await ready(path, audience, issuer, subject, now)).toBe(false);
      expect(await readdir(dir)).toEqual([]);
      const fetched = async () =>
        new Response(token(), { headers: { "Metadata-Flavor": "Google" } });
      expect(
        await refreshAssertion(path, audience, issuer, subject, fetched, now),
      ).toEqual({ refreshed: true, delay: 300000 });
      expect(await ready(path, audience, issuer, subject, now)).toBe(true);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  test("refreshes before expiry and backs off for cached or expired assertions", () => {
    expect(refreshDelay(now + 3600000, now)).toBe(300000);
    expect(refreshDelay(now + 400000, now)).toBe(100000);
    expect(refreshDelay(now + 200000, now)).toBe(5000);
    expect(refreshDelay(now - 1, now)).toBe(5000);
  });
});
