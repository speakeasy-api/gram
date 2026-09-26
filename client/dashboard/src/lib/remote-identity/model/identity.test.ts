import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { describe, expect, it } from "vitest";
import {
  IdentityErrors,
  authorizationHeaderGuard,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
} from "./headers";
import { deriveIdentityMode } from "./identity";

function header(
  overrides: Partial<RemoteMcpServerHeader> = {},
): RemoteMcpServerHeader {
  return {
    id: "header-1",
    name: "X-API-Key",
    value: "***",
    isRequired: false,
    isSecret: true,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}

describe("deriveIdentityMode", () => {
  it("gives User Identity precedence over a legacy Authorization header", () => {
    expect(
      deriveIdentityMode(1, [header({ name: "Authorization", value: "***" })]),
    ).toBe("user");
  });

  it("derives Agent Identity only from a static Authorization header", () => {
    expect(
      deriveIdentityMode(0, [
        header({ name: " authorization ", value: "***" }),
      ]),
    ).toBe("agent");
    expect(
      deriveIdentityMode(0, [
        header({
          name: "Authorization",
          value: undefined,
          valueFromRequestHeader: "X-Authorization",
        }),
      ]),
    ).toBe("none");
    expect(
      deriveIdentityMode(0, [
        header({
          id: "request-header",
          name: "Authorization",
          value: undefined,
          valueFromRequestHeader: "X-Authorization",
        }),
        header({ id: "static-header", name: "AUTHORIZATION", value: "***" }),
      ]),
    ).toBe("agent");
  });

  it("derives No Identity without a linked client or static credential", () => {
    expect(deriveIdentityMode(0, [header()])).toBe("none");
  });
});

describe("authorizationHeaderGuard", () => {
  it("rejects creating or renaming Authorization under No Identity", () => {
    expect(authorizationHeaderGuard("none", "Authorization", false)).toBe(
      IdentityErrors.NoAuthorization,
    );
    expect(authorizationHeaderGuard("none", " authorization ", false)).toBe(
      IdentityErrors.NoAuthorization,
    );
  });

  it("reserves Authorization for the managed row under Agent and User Identity", () => {
    expect(authorizationHeaderGuard("agent", "Authorization", false)).toBe(
      IdentityErrors.ManagedAuthorization,
    );
    expect(authorizationHeaderGuard("user", "Authorization", false)).toBe(
      IdentityErrors.ManagedAuthorization,
    );
    expect(authorizationHeaderGuard("agent", "Authorization", true)).toBeNull();
    expect(authorizationHeaderGuard("user", "Authorization", true)).toBeNull();
  });

  it("does not restrict other header names", () => {
    expect(authorizationHeaderGuard("none", "X-API-Key", false)).toBeNull();
  });
});

describe("findPassThroughAuthorizationHeader", () => {
  it("separates legacy pass-through Authorization from Agent Identity", () => {
    const passThrough = header({
      name: "Authorization",
      value: undefined,
      valueFromRequestHeader: "X-Legacy-Authorization",
    });

    expect(findPassThroughAuthorizationHeader([passThrough])).toBe(passThrough);
    expect(findStaticAuthorizationHeader([passThrough])).toBeUndefined();
    expect(deriveIdentityMode(0, [passThrough])).toBe("none");
  });
});
