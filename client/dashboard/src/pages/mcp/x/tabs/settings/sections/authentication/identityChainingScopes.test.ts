import { describe, expect, it } from "vitest";

import {
  chainingBound,
  chainingGrantDeclaration,
  chainingResourceMetadata,
  chainingScopeOptions,
  chainingScopesAllowed,
  chainingUnlinkable,
  defaultChainingScopes,
  displayedChainingState,
  isCimdClient,
  isPublicClient,
  JWT_BEARER_GRANT,
  preparationSucceeded,
  sanitizeChainingScopes,
} from "./identityChainingScopes";

describe("defaultChainingScopes", () => {
  it("drops OIDC-reserved scopes from the client's scope", () => {
    expect(
      defaultChainingScopes([
        "openid",
        "profile",
        "email",
        "offline_access",
        "phone",
        "address",
        "todos.read",
      ]),
    ).toEqual(["todos.read"]);
  });

  it("is empty when the client only has sign-in scopes", () => {
    expect(defaultChainingScopes(["openid", "profile", "email"])).toEqual([]);
  });

  it("is empty when the client sets no scope", () => {
    expect(defaultChainingScopes(undefined)).toEqual([]);
  });
});

describe("chainingScopesAllowed", () => {
  const clientScope = ["openid", "todos.read", "todos.write"];

  it("accepts a subset of the client's scope, including none", () => {
    expect(chainingScopesAllowed(["todos.read"], clientScope)).toBe(true);
    expect(chainingScopesAllowed([], clientScope)).toBe(true);
  });

  it("rejects scopes outside the client's scope", () => {
    expect(chainingScopesAllowed(["todos.delete"], clientScope)).toBe(false);
  });

  it("rejects OIDC-reserved scopes even when the client has them", () => {
    expect(chainingScopesAllowed(["openid"], clientScope)).toBe(false);
  });

  it("accepts anything when the client sets no scope", () => {
    expect(chainingScopesAllowed(["anything"], undefined)).toBe(true);
  });
});

describe("chainingScopeOptions", () => {
  it("disables advertised resource scopes outside the client's scope", () => {
    expect(
      chainingScopeOptions(
        ["openid", "todos.read"],
        ["todos.read", "todos.write", "email"],
        [],
      ),
    ).toEqual([
      {
        value: "todos.read",
        label: "todos.read",
        description: "Advertised by this server",
      },
      {
        value: "todos.write",
        label: "todos.write",
        disabled: true,
        description: "Add to the client's scope first",
      },
    ]);
  });

  it("offers advertised scopes when the client sets no scope", () => {
    expect(chainingScopeOptions(undefined, ["todos.read"], ["custom"])).toEqual(
      [
        {
          value: "todos.read",
          label: "todos.read",
          description: "Advertised by this server",
        },
        { value: "custom", label: "custom" },
      ],
    );
  });
});

describe("sanitizeChainingScopes", () => {
  it("drops OIDC scopes and splits pasted lists", () => {
    expect(sanitizeChainingScopes(["openid todos.read", "todos.read"])).toEqual(
      ["todos.read"],
    );
  });
});

describe("chaining binding state", () => {
  const noBinding = { state: "configuration_required" as const };
  const ineligible = { state: "unsupported_profile" as const };
  const boundStuck = {
    state: "manual_setup_required" as const,
    bindingId: "binding-1",
    clientId: "client-1",
  };
  const unlinked = { state: "unlinked" as const, bindingId: "binding-1" };

  it("is unbound without a binding, whatever the state", () => {
    expect(chainingBound(noBinding)).toBe(false);
    expect(chainingBound(ineligible)).toBe(false);
    expect(chainingBound(unlinked)).toBe(false);
    expect(chainingBound(boundStuck)).toBe(true);
  });

  it("reads a first-time server as not enabled but keeps eligibility states", () => {
    expect(displayedChainingState(noBinding)).toBe("unlinked");
    expect(displayedChainingState(ineligible)).toBe("unsupported_profile");
    expect(displayedChainingState(boundStuck)).toBe("manual_setup_required");
  });

  it("offers Disable for any live binding, not only ready ones", () => {
    expect(chainingUnlinkable(boundStuck)).toBe(true);
    expect(
      chainingUnlinkable({ ...boundStuck, state: "transient_failure" }),
    ).toBe(true);
    expect(chainingUnlinkable(unlinked)).toBe(false);
    expect(chainingUnlinkable(noBinding)).toBe(false);
  });
});

describe("preparationSucceeded", () => {
  it("only accepts persisted ready outcomes", () => {
    expect(preparationSucceeded({ state: "ready" })).toBe(true);
    expect(
      preparationSucceeded({ state: "published_acceptance_unverified" }),
    ).toBe(true);
    expect(preparationSucceeded({ state: "configuration_required" })).toBe(
      false,
    );
    expect(preparationSucceeded({ state: "unknown_grants" })).toBe(false);
  });
});

describe("chainingGrantDeclaration", () => {
  it("declares the RFC 7591 default plus JWT-bearer for an unknown client", () => {
    expect(chainingGrantDeclaration(null)).toEqual({
      grants: ["authorization_code", JWT_BEARER_GRANT],
      rewrites: true,
    });
  });

  it("keeps the published interactive grants for an unknown CIMD client", () => {
    expect(chainingGrantDeclaration(null, true)).toEqual({
      grants: ["authorization_code", "refresh_token", JWT_BEARER_GRANT],
      rewrites: true,
    });
  });

  it("adds JWT-bearer to recorded grants", () => {
    expect(chainingGrantDeclaration(["authorization_code"])).toEqual({
      grants: ["authorization_code", JWT_BEARER_GRANT],
      rewrites: true,
    });
  });

  it("does not rewrite a client already registered for JWT-bearer", () => {
    expect(
      chainingGrantDeclaration(["authorization_code", JWT_BEARER_GRANT]),
    ).toEqual({
      grants: ["authorization_code", JWT_BEARER_GRANT],
      rewrites: false,
    });
  });
});

describe("client kind", () => {
  it("detects CIMD and public clients", () => {
    expect(isCimdClient({ clientIdMetadataUri: "https://gram/c.json" })).toBe(
      true,
    );
    expect(isCimdClient({})).toBe(false);
    expect(isPublicClient({ tokenEndpointAuthMethod: "none" })).toBe(true);
    expect(isPublicClient({ tokenEndpointAuthMethod: "private_key_jwt" })).toBe(
      false,
    );
  });
});

describe("chainingResourceMetadata", () => {
  it("passes the probed resource and its authorization servers", () => {
    expect(
      chainingResourceMetadata({
        resource: "https://mcp.example.com/",
        authorizationServers: ["https://as.example.com"],
        scopesSupported: ["read"],
      }),
    ).toEqual({
      resource: "https://mcp.example.com/",
      authorizationServers: ["https://as.example.com"],
    });
  });

  it("is omitted without a probe, a resource or authorization servers", () => {
    expect(chainingResourceMetadata(null)).toBeUndefined();
    expect(
      chainingResourceMetadata({ authorizationServers: ["https://as"] }),
    ).toBeUndefined();
    expect(
      chainingResourceMetadata({
        resource: "https://mcp.example.com/",
        authorizationServers: [],
      }),
    ).toBeUndefined();
  });
});
