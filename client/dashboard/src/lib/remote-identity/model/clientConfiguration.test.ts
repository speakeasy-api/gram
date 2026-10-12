import { describe, expect, it } from "vitest";
import {
  advertisedScopes,
  normalizeScopes,
  pickPreferredAuthMethod,
  preferredScopes,
  serverIdentityAuthMethod,
} from "./clientConfiguration";

describe("preferredScopes", () => {
  it("prefers the protected resource's scopes over the issuer's", () => {
    expect(
      preferredScopes(["read", "write"], ["read", "write", "admin"]),
    ).toEqual(["read", "write"]);
  });

  it("falls back to the issuer's scopes when the resource names none", () => {
    expect(preferredScopes([], ["openid"])).toEqual(["openid"]);
    expect(preferredScopes(undefined, ["openid"])).toEqual(["openid"]);
    expect(preferredScopes(null, null)).toEqual([]);
  });

  it("ignores blank entries and trims the rest", () => {
    expect(preferredScopes([" ", ""], [" openid ", ""])).toEqual(["openid"]);
    expect(preferredScopes([" read "], ["openid"])).toEqual(["read"]);
  });
});

describe("advertisedScopes", () => {
  it("lists the resource's scopes first, then the issuer's others", () => {
    expect(
      advertisedScopes(["read", "write"], ["openid", "read", "admin"]),
    ).toEqual(["read", "write", "openid", "admin"]);
  });

  it("ignores blank and missing lists", () => {
    expect(advertisedScopes(undefined, [" openid ", ""])).toEqual(["openid"]);
    expect(advertisedScopes(null, null)).toEqual([]);
  });

  it("splits a malformed entry that holds several scopes", () => {
    expect(advertisedScopes(["read write"], [" read "])).toEqual([
      "read",
      "write",
    ]);
  });
});

describe("normalizeScopes", () => {
  it("splits entries on whitespace and drops repeats", () => {
    expect(normalizeScopes(["read", " read  write ", "write", ""])).toEqual([
      "read",
      "write",
    ]);
    expect(normalizeScopes(["a\tb\nc", "   "])).toEqual(["a", "b", "c"]);
    expect(normalizeScopes([])).toEqual([]);
  });

  it("keeps the order scopes were first entered in", () => {
    expect(normalizeScopes(["write read", "read"])).toEqual(["write", "read"]);
  });

  it("keeps scopes exactly as cased", () => {
    expect(normalizeScopes(["Read", "read"])).toEqual(["Read", "read"]);
  });
});

describe("serverIdentityAuthMethod", () => {
  it("uses the RFC 8414 default when nothing is advertised", () => {
    expect(serverIdentityAuthMethod([])).toBe("client_secret_basic");
  });

  it("picks the most preferred advertised method", () => {
    expect(serverIdentityAuthMethod(["none", "client_secret_post"])).toBe(
      "client_secret_post",
    );
    expect(
      serverIdentityAuthMethod(["client_secret_post", "client_secret_basic"]),
    ).toBe("client_secret_basic");
    expect(serverIdentityAuthMethod(["none"])).toBe("none");
  });

  it("omits the method when only unsupported ones are advertised", () => {
    expect(serverIdentityAuthMethod(["private_key_jwt"])).toBeUndefined();
    expect(
      serverIdentityAuthMethod(["private_key_jwt", "tls_client_auth"]),
    ).toBeUndefined();
  });
});

describe("pickPreferredAuthMethod", () => {
  it("falls back to client_secret_basic so DCR always names a method", () => {
    expect(pickPreferredAuthMethod(["private_key_jwt"])).toBe(
      "client_secret_basic",
    );
    expect(pickPreferredAuthMethod(["none", "client_secret_post"])).toBe(
      "client_secret_post",
    );
  });
});
