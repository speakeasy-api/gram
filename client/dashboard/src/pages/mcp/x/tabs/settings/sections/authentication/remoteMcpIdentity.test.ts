import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { describe, expect, it } from "vitest";
import {
  NO_IDENTITY_AUTHORIZATION_ERROR,
  authorizationHeaderGuard,
  deriveRemoteMcpIdentityMode,
  findPassThroughAuthorizationHeader,
  findStaticAuthorizationHeader,
} from "./remoteMcpIdentity";

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

describe("deriveRemoteMcpIdentityMode", () => {
  it("gives User Identity precedence over a legacy Authorization header", () => {
    expect(
      deriveRemoteMcpIdentityMode(1, [
        header({ name: "Authorization", value: "***" }),
      ]),
    ).toBe("user");
  });

  it("derives Agent Identity only from a static Authorization header", () => {
    expect(
      deriveRemoteMcpIdentityMode(0, [
        header({ name: " authorization ", value: "***" }),
      ]),
    ).toBe("agent");
    expect(
      deriveRemoteMcpIdentityMode(0, [
        header({
          name: "Authorization",
          value: undefined,
          valueFromRequestHeader: "X-Authorization",
        }),
      ]),
    ).toBe("none");
    expect(
      deriveRemoteMcpIdentityMode(0, [
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
    expect(deriveRemoteMcpIdentityMode(0, [header()])).toBe("none");
  });
});

describe("authorizationHeaderGuard", () => {
  it("rejects creating or renaming Authorization under No Identity", () => {
    expect(authorizationHeaderGuard("none", "Authorization", false)).toBe(
      NO_IDENTITY_AUTHORIZATION_ERROR,
    );
    expect(authorizationHeaderGuard("none", " authorization ", false)).toBe(
      NO_IDENTITY_AUTHORIZATION_ERROR,
    );
  });

  it("reserves Authorization for the managed row under Agent and User Identity", () => {
    expect(authorizationHeaderGuard("agent", "Authorization", false)).toBe(
      "Authorization is managed in the Identity section.",
    );
    expect(authorizationHeaderGuard("user", "Authorization", false)).toBe(
      "Authorization is managed in the Identity section.",
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
    expect(deriveRemoteMcpIdentityMode(0, [passThrough])).toBe("none");
  });
});
