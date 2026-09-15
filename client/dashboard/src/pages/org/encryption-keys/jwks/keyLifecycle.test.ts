import { describe, expect, it } from "vitest";
import type { JSONWebKey } from "@gram/client/models/components/jsonwebkey.js";
import {
  availableKeyActions,
  keyActionCopy,
  keyAlgorithm,
} from "./keyLifecycle";

function key(publicJwk: unknown): JSONWebKey {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    organizationId: "org",
    jsonWebKeySetId: "22222222-2222-2222-2222-222222222222",
    externalKeyId: "33333333-3333-3333-3333-333333333333",
    kid: "kid",
    keyState: "active",
    publicJwk,
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
  };
}

describe("availableKeyActions", () => {
  it("mirrors the transitions the server accepts", () => {
    expect(availableKeyActions("pending")).toEqual(["activate", "revoke"]);
    expect(availableKeyActions("active")).toEqual(["retire", "revoke"]);
    expect(availableKeyActions("retired")).toEqual(["activate", "revoke"]);
    expect(availableKeyActions("revoked")).toEqual([]);
  });

  it("never offers retire to a pending key", () => {
    expect(availableKeyActions("pending")).not.toContain("retire");
  });
});

describe("keyAlgorithm", () => {
  it("reads alg from the published JWK document", () => {
    expect(keyAlgorithm(key({ kty: "RSA", alg: "RS256", use: "sig" }))).toBe(
      "RS256",
    );
  });

  it("falls back for a document without a usable alg", () => {
    expect(keyAlgorithm(key({ kty: "RSA" }))).toBe("Unknown");
    expect(keyAlgorithm(key({ alg: 42 }))).toBe("Unknown");
    expect(keyAlgorithm(key(null))).toBe("Unknown");
    expect(keyAlgorithm(key("RS256"))).toBe("Unknown");
  });
});

describe("keyActionCopy", () => {
  it("marks only revoke as destructive", () => {
    expect(keyActionCopy("revoke").destructive).toBe(true);
    expect(keyActionCopy("retire").destructive).toBe(false);
    expect(keyActionCopy("activate").destructive).toBe(false);
  });

  it("warns that revoking breaks verification of outstanding tokens", () => {
    expect(keyActionCopy("revoke").description).toMatch(/stops verifying/);
  });
});
