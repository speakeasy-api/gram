import type { JSONWebKey } from "@gram/client/models/components/jsonwebkey.js";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";

import {
  activePublicJwk,
  addSignInIssuerRequest,
  findSignInClient,
  freeSignInIssuerSlug,
  managedExternalKeyIds,
  managedKeySetIds,
  needsTrustConfirmation,
  normalizeIssuerSlug,
  signInIssuerRows,
  signInIssuerSlugError,
  staleSignInClientIds,
} from "./oktaSignIn";

const AGENT_ID = "agent-client-id";

function makeClient(
  overrides: Partial<RemoteSessionClient>,
): RemoteSessionClient {
  return {
    id: "client",
    clientId: "client-id",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    grantTypes: null,
    credentialOwner: "subject",
    legacyCallbackUrl: false,
    organizationId: "organization-id",
    projectId: "",
    remoteSessionIssuerId: "issuer-id",
    userSessionIssuerIds: [],
    ...overrides,
  };
}

function makeSet(id: string, externalKeyId: string): JSONWebKeySet {
  return {
    id,
    name: id,
    externalKeyId,
    organizationId: "organization-id",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
  };
}

describe("managedKeySetIds", () => {
  it("only excludes the connection-managed client, retaining previous agents and other clients", () => {
    const ids = managedKeySetIds(
      [
        makeClient({
          id: "managed",
          clientId: "management-client-id",
          jsonWebKeySetId: "managed-set",
        }),
        makeClient({
          id: "previous-sign-in",
          clientId: "previous-agent-id",
          jsonWebKeySetId: "previous-sign-in-set",
        }),
        makeClient({
          id: "other",
          clientId: "other-client-id",
          jsonWebKeySetId: "other-set",
        }),
        makeClient({
          id: "sign-in",
          clientId: AGENT_ID,
          jsonWebKeySetId: "sign-in-set",
        }),
        makeClient({ id: "unsigned" }),
      ],
      "management-client-id",
    );

    expect([...ids]).toEqual(["managed-set"]);
    expect([
      ...managedExternalKeyIds(
        [
          makeSet("managed-set", "managed-key"),
          makeSet("previous-sign-in-set", "own-key"),
        ],
        ids,
      ),
    ]).toEqual(["managed-key"]);
  });
});

describe("managedExternalKeyIds", () => {
  it("returns only the external keys backing managed sets", () => {
    const ids = managedExternalKeyIds(
      [makeSet("managed-set", "managed-key"), makeSet("own-set", "own-key")],
      new Set(["managed-set"]),
    );

    expect([...ids]).toEqual(["managed-key"]);
  });

  it("is empty when no set is managed", () => {
    const ids = managedExternalKeyIds(
      [makeSet("own-set", "own-key")],
      new Set(),
    );

    expect(ids.size).toBe(0);
  });
});

function makeIssuer(overrides: Partial<UserSessionIssuer>): UserSessionIssuer {
  return {
    id: "usi",
    slug: "usi",
    authnChallengeMode: "interactive",
    clientIdMetadataAdmissionMode: "disabled",
    sessionDurationHours: 336,
    organizationId: "organization-id",
    projectId: "",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

describe("signInIssuerRows", () => {
  const signIn = makeClient({ id: "sign-in", clientId: AGENT_ID });

  it("marks organization issuers trusting the Okta pair and skips project issuers", () => {
    const rows = signInIssuerRows(
      [
        makeIssuer({
          id: "trusting",
          trustedRemoteSessionIssuerId: "okta",
          trustedRemoteSessionClientId: "sign-in",
        }),
        makeIssuer({
          id: "other",
          trustedRemoteSessionIssuerId: "okta",
          trustedRemoteSessionClientId: "someone-else",
        }),
        makeIssuer({ id: "plain" }),
        makeIssuer({ id: "project", projectId: "project-id" }),
      ],
      "okta",
      signIn,
    );

    expect(
      rows.map(({ issuer, trustsSignIn, trustsOther }) => [
        issuer.id,
        trustsSignIn,
        trustsOther,
      ]),
    ).toEqual([
      ["trusting", true, false],
      ["other", false, true],
      ["plain", false, false],
    ]);
  });

  it("requires the Okta issuer as well as the client", () => {
    const [row] = signInIssuerRows(
      [
        makeIssuer({
          trustedRemoteSessionIssuerId: "elsewhere",
          trustedRemoteSessionClientId: "sign-in",
        }),
      ],
      "okta",
      signIn,
    );

    expect(row?.trustsSignIn).toBe(false);
  });

  it("trusts nothing before the sign-in client exists", () => {
    const rows = signInIssuerRows([makeIssuer({})], "okta", undefined);

    expect(rows[0]?.trustsSignIn).toBe(false);
  });
});

describe("sign-in issuer slugs", () => {
  it("normalizes typed slugs", () => {
    expect(normalizeIssuerSlug("Okta Linear!")).toBe("okta-linear-");
  });

  it("rejects empty and taken organization slugs", () => {
    const issuers = [
      makeIssuer({ slug: "okta-sign-in" }),
      makeIssuer({ slug: "project-only", projectId: "project-id" }),
    ];

    expect(signInIssuerSlugError("", issuers)).not.toBeNull();
    expect(signInIssuerSlugError("okta-sign-in", issuers)).not.toBeNull();
    expect(signInIssuerSlugError("project-only", issuers)).toBeNull();
    expect(signInIssuerSlugError("okta-linear", issuers)).toBeNull();
  });

  it("defaults a new issuer to the first free okta-sign-in slug", () => {
    expect(freeSignInIssuerSlug([])).toBe("okta-sign-in");
    expect(
      freeSignInIssuerSlug([
        makeIssuer({ slug: "okta-sign-in" }),
        makeIssuer({ slug: "okta-sign-in-2" }),
        makeIssuer({ slug: "okta-sign-in-3", projectId: "project-id" }),
      ]),
    ).toBe("okta-sign-in-3");
  });

  it("creates an interactive issuer trusting the pair", () => {
    const form = addSignInIssuerRequest({
      slug: "okta-linear-",
      remoteSessionIssuerId: "okta",
      clientId: "sign-in",
    }).createOrganizationUserSessionIssuerForm;

    expect(form).toMatchObject({
      slug: "okta-linear",
      authnChallengeMode: "interactive",
      trustedRemoteSessionIssuerId: "okta",
      trustedRemoteSessionClientId: "sign-in",
    });
  });
});

describe("findSignInClient", () => {
  const managedId = "managed-client-id";

  it("picks the single org client registered for the agent", () => {
    const signIn = makeClient({ id: "sign-in", clientId: AGENT_ID });
    const match = findSignInClient(
      [
        makeClient({ id: "managed", clientId: managedId }),
        makeClient({ id: "project", clientId: AGENT_ID, projectId: "p" }),
        signIn,
      ],
      AGENT_ID,
      managedId,
    );

    expect(match).toEqual({ client: signIn, duplicates: 0 });
  });

  it("fails closed on duplicates", () => {
    const match = findSignInClient(
      [
        makeClient({ id: "a", clientId: AGENT_ID }),
        makeClient({ id: "b", clientId: AGENT_ID }),
      ],
      AGENT_ID,
      managedId,
    );

    expect(match).toEqual({ client: undefined, duplicates: 2 });
  });

  it("never treats the managed client as the sign-in client", () => {
    const match = findSignInClient(
      [makeClient({ id: "managed", clientId: managedId })],
      managedId,
      managedId,
    );

    expect(match).toEqual({ client: undefined, duplicates: 0 });
  });
});

describe("stale sign-in trust", () => {
  it("flags issuers trusting a previous agent's client on the Okta issuer", () => {
    const clients = [
      makeClient({ id: "current", clientId: AGENT_ID }),
      makeClient({ id: "previous", clientId: "old-agent" }),
      makeClient({ id: "managed", clientId: "managed-client-id" }),
    ];
    const stale = staleSignInClientIds(clients, AGENT_ID, "managed-client-id");
    expect([...stale]).toEqual(["previous"]);

    const rows = signInIssuerRows(
      [
        makeIssuer({
          id: "old",
          trustedRemoteSessionIssuerId: "okta",
          trustedRemoteSessionClientId: "previous",
        }),
        makeIssuer({
          id: "elsewhere",
          trustedRemoteSessionIssuerId: "other",
          trustedRemoteSessionClientId: "previous",
        }),
        makeIssuer({
          id: "current",
          trustedRemoteSessionIssuerId: "okta",
          trustedRemoteSessionClientId: "current",
        }),
      ],
      "okta",
      clients[0],
      stale,
    );

    expect(rows.map((row) => [row.issuer.id, row.trustsStale])).toEqual([
      ["old", true],
      ["elsewhere", false],
      ["current", false],
    ]);
  });
});

function makeKey(overrides: Partial<JSONWebKey>): JSONWebKey {
  return {
    id: "key",
    kid: "kid-1",
    jsonWebKeySetId: "set",
    externalKeyId: "external",
    organizationId: "organization-id",
    keyState: "active",
    publicJwk: {},
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

describe("activePublicJwk", () => {
  it("returns only the active key's public members", () => {
    const jwk = activePublicJwk([
      makeKey({
        id: "retired",
        keyState: "retired",
        publicJwk: { kid: "old" },
      }),
      makeKey({
        publicJwk: {
          kid: "kid-1",
          kty: "RSA",
          alg: "RS256",
          n: "modulus",
          e: "AQAB",
          d: "private",
          p: "private",
        },
      }),
    ]);

    expect(jwk).toEqual({
      kid: "kid-1",
      kty: "RSA",
      alg: "RS256",
      use: "sig",
      n: "modulus",
      e: "AQAB",
    });
  });

  it("is undefined without an active key", () => {
    expect(activePublicJwk([makeKey({ keyState: "pending" })])).toBeUndefined();
  });
});

describe("needsTrustConfirmation", () => {
  const none = {
    trustsOther: false,
    servers: [],
    toolsets: [],
    lookupFailed: false,
  };
  const ref = { id: "r", name: "R", projectId: "p", projectName: "P" };

  it("asks when a toolset uses the issuer", () => {
    expect(needsTrustConfirmation(none)).toBe(false);
    expect(needsTrustConfirmation({ ...none, toolsets: [ref] })).toBe(true);
    expect(needsTrustConfirmation({ ...none, servers: [ref] })).toBe(true);
    expect(needsTrustConfirmation({ ...none, lookupFailed: true })).toBe(true);
  });
});
